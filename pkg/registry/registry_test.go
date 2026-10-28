package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// mountRegistry wraps the ggcr fake registry, which does not implement
// cross-repo blob mounts, and serves `mount=` requests itself by copying the
// blob through the inner handler.
type mountRegistry struct{ inner http.Handler }

func (m mountRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost &&
		strings.HasSuffix(r.URL.Path, "/blobs/uploads/") &&
		r.URL.Query().Get("mount") != "" {
		q := r.URL.Query()
		digest, from := q.Get("mount"), q.Get("from")
		repo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v2/"), "/blobs/uploads/")
		// Read the source blob.
		rr := httptest.NewRecorder()
		m.inner.ServeHTTP(rr, httptest.NewRequest(http.MethodGet,
			"/v2/"+from+"/blobs/"+digest, nil))
		if rr.Code != http.StatusOK {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Monolithic upload into the destination repo.
		rr2 := httptest.NewRecorder()
		m.inner.ServeHTTP(rr2, httptest.NewRequest(http.MethodPost,
			"/v2/"+repo+"/blobs/uploads/", nil))
		loc := rr2.Header().Get("Location")
		sep := "?"
		if strings.Contains(loc, "?") {
			sep = "&"
		}
		rr3 := httptest.NewRecorder()
		m.inner.ServeHTTP(rr3, httptest.NewRequest(http.MethodPut,
			loc+sep+"digest="+digest, rr.Body))
		if rr3.Code != http.StatusCreated {
			w.WriteHeader(rr3.Code)
			return
		}
		w.Header().Set("Location", "/v2/"+repo+"/blobs/"+digest)
		w.WriteHeader(http.StatusCreated)
		return
	}
	m.inner.ServeHTTP(w, r)
}

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(mountRegistry{ggcrregistry.New(ggcrregistry.WithReferrersSupport(true))})
	t.Cleanup(s.Close)
	return s
}

func testClient(s *httptest.Server, tr http.RoundTripper, chunk int64) *Client {
	return New(Options{
		Keychain:  authn.NewMultiKeychain(),
		Insecure:  true,
		ChunkSize: chunk,
		Transport: tr,
	})
}

func repoURL(s *httptest.Server, repo string) string {
	return strings.TrimPrefix(s.URL, "http://") + "/" + repo
}

type recorder struct {
	base http.RoundTripper
	mu   sync.Mutex
	reqs []*http.Request
	// failFirst PATCH: inject one transport error
	failPatch int
	patched   int
	// head404, if set, makes matching HEAD requests return 404 locally
	// (emulates per-repo blob isolation that the fake registry lacks).
	head404 func(*http.Request) bool
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req.Clone(req.Context()))
	if r.head404 != nil && req.Method == http.MethodHead && r.head404(req) {
		r.mu.Unlock()
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     http.Header{},
			Request:    req,
		}, nil
	}
	if req.Method == http.MethodPatch {
		r.patched++
		if r.failPatch > 0 {
			r.failPatch--
			r.mu.Unlock()
			return nil, fmt.Errorf("injected PATCH failure")
		}
	}
	r.mu.Unlock()
	return r.base.RoundTrip(req)
}

func (r *recorder) count(match func(*http.Request) bool) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, q := range r.reqs {
		if match(q) {
			n++
		}
	}
	return n
}

func TestChunkedUploadResume(t *testing.T) {
	s := testServer(t)
	rec := &recorder{base: http.DefaultTransport, failPatch: 1}
	c := testClient(s, rec, 8) // 8-byte chunks force many PATCHes

	ctx := context.Background()
	repo, err := name.NewRepository(repoURL(s, "a/b"), name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(strings.Repeat("0123456789abcdef", 10)) // 160 bytes
	sum := sha256.Sum256(data)
	d := v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(sum[:])}
	if err := c.UploadBlob(ctx, repo, d, strings.NewReader(string(data)), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	exists, err := c.BlobExists(ctx, repo, d)
	if err != nil || !exists {
		t.Fatalf("blob not uploaded: %v", err)
	}
	if rec.patched < 2 {
		t.Fatalf("expected multiple PATCHes, got %d", rec.patched)
	}
}

func TestBlobExistsRedirect(t *testing.T) {
	status := http.StatusTemporaryRedirect
	redirects := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodHead || !strings.Contains(r.URL.Path, "/blobs/sha256:") {
			redirects++
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Location", "/must-not-follow")
		w.WriteHeader(status)
	}))
	t.Cleanup(s.Close)
	c := testClient(s, http.DefaultTransport, DefaultChunkSize)
	repo, err := name.NewRepository(repoURL(s, "a/b"), name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	d := v1.Hash{Algorithm: "sha256", Hex: strings.Repeat("a", 64)}
	for _, tc := range []struct {
		status int
		exists bool
		err    bool
	}{
		{http.StatusOK, true, false},
		{http.StatusFound, true, false},
		{http.StatusTemporaryRedirect, true, false},
		{http.StatusNotFound, false, false},
		{http.StatusInternalServerError, false, true},
	} {
		status = tc.status
		got, err := c.BlobExists(context.Background(), repo, d)
		if got != tc.exists || (err != nil) != tc.err {
			t.Errorf("status %d: got (%v, %v), want (%v, error=%v)", tc.status, got, err, tc.exists, tc.err)
		}
	}
	if redirects != 0 {
		t.Fatalf("followed %d blob redirects", redirects)
	}
}

func TestBlobRedirect(t *testing.T) {
	downloads := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		if r.Header.Get("Authorization") != "" {
			t.Error("registry credentials sent to blob storage")
		}
		io.WriteString(w, "metadata")
	}))
	t.Cleanup(target.Close)
	status := http.StatusTemporaryRedirect
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Location", target.URL+"/file")
		w.WriteHeader(status)
	}))
	t.Cleanup(source.Close)
	c := testClient(source, http.DefaultTransport, DefaultChunkSize)
	d := v1.Hash{Algorithm: "sha256", Hex: strings.Repeat("a", 64)}
	for _, code := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
		status = code
		data, err := c.Blob(context.Background(), repoURL(source, "a/b"), d)
		if err != nil || string(data) != "metadata" {
			t.Fatalf("status %d: data %q, error %v", code, data, err)
		}
	}
	if downloads != 2 {
		t.Fatalf("got %d blob storage requests, want 2", downloads)
	}
}

func TestMountAcrossRepos(t *testing.T) {
	s := testServer(t)
	rec := &recorder{base: http.DefaultTransport}
	c := testClient(s, rec, DefaultChunkSize)
	ctx := context.Background()

	// Push an image to repo a/src.
	img := empty.Image
	layerData := []byte("layer contents " + strings.Repeat("z", 1000))
	l := static.NewLayer(layerData, types.OCILayer)
	var err error
	img, err = mutate.AppendLayers(img, l)
	if err != nil {
		t.Fatal(err)
	}
	src := repoURL(s, "a/src") + ":tag"
	if err := c.WriteImage(ctx, src, img); err != nil {
		t.Fatal(err)
	}

	// Pull it; layers become MountableLayers.
	pulled, err := c.Image(ctx, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	layers, err := pulled.Layers()
	if err != nil {
		t.Fatal(err)
	}
	// The fake registry shares blobs across repos; pretend a/dst does not
	// yet have the layer so the mount path is exercised.
	layerDigest, _ := layers[0].Digest()
	rec.head404 = func(r *http.Request) bool {
		return strings.HasPrefix(r.URL.Path, "/v2/a/dst/blobs/"+layerDigest.String())
	}
	dstRepo, _ := name.NewRepository(repoURL(s, "a/dst"), name.Insecure)
	strat, err := c.Classify(ctx, dstRepo, layers[0])
	if err != nil {
		t.Fatal(err)
	}
	if strat != StrategyMount {
		t.Fatalf("strategy %s", strat)
	}

	// Write to a/dst; the layer should mount (no PATCH carries its bytes).
	dst := repoURL(s, "a/dst") + ":tag"
	img2 := empty.Image
	img2, _ = mutate.AppendLayers(img2, layers[0])
	if err := c.WriteImage(ctx, dst, img2); err != nil {
		t.Fatal(err)
	}
	mounts := rec.count(func(r *http.Request) bool {
		return r.Method == http.MethodPost && strings.Contains(r.URL.RawQuery, "mount=")
	})
	if mounts == 0 {
		t.Fatal("no mount attempted")
	}
	// Exactly one big PATCH so far: the original upload into a/src.
	patches := rec.count(func(r *http.Request) bool {
		return r.Method == http.MethodPatch && r.ContentLength >= int64(len(layerData))
	})
	if patches != 1 {
		t.Fatalf("layer was uploaded again, not mounted (patches=%d)", patches)
	}
	// Verify readable in dst.
	img3, err := c.Image(ctx, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	ls, _ := img3.Layers()
	rc, _ := ls[0].Compressed()
	got, _ := io.ReadAll(rc)
	if string(got) != string(layerData) {
		t.Fatal("mounted layer content mismatch")
	}
}

func TestHeadAndTags(t *testing.T) {
	s := testServer(t)
	c := testClient(s, http.DefaultTransport, DefaultChunkSize)
	ctx := context.Background()
	img := empty.Image
	ref := repoURL(s, "x/y") + ":t1"
	if err := c.WriteImage(ctx, ref, img); err != nil {
		t.Fatal(err)
	}
	d, err := c.Head(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if d == nil || d.Digest.Hex == "" {
		t.Fatal("empty descriptor")
	}
	tags, err := c.ListTags(ctx, repoURL(s, "x/y"))
	if err != nil || len(tags) != 1 || tags[0] != "t1" {
		t.Fatalf("tags: %v %v", tags, err)
	}
}
