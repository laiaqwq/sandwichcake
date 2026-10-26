package slice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/laiaqwq/sandwichcake/internal/cache"
	"github.com/laiaqwq/sandwichcake/internal/testutil"
	"github.com/laiaqwq/sandwichcake/pkg/fetch"
	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/registry"
)

func modelFiles() []testutil.HubFile {
	big := make([]byte, 5000)
	for i := range big {
		big[i] = byte(i * 31)
	}
	return []testutil.HubFile{
		{Path: "config.json", Data: []byte(`{"model_type":"x"}`)},
		{Path: "empty.txt", Data: nil},
		{Path: "model-00001-of-00002.safetensors", Data: big, LFS: true},
		{Path: "model-00002-of-00002.safetensors", Data: bytes.Repeat([]byte("z"), 3000), LFS: true},
		{Path: "tokenizer/tokenizer.json", Data: []byte(`{"tok":true}`)},
	}
}

// remoteFor lists hub's model and wraps every file opener with counters
// and an optional failure injector.
func remoteFor(t *testing.T, h *testutil.Hub, opened *int64, failAt func(path string, off int64) error) *Remote {
	t.Helper()
	fc := fetch.New(fetch.Options{
		Endpoints: fetch.Endpoints{HF: h.API.URL},
		Backoff:   func(int) time.Duration { return time.Millisecond },
	})
	l, err := fc.List(context.Background(), fetch.Spec{Provider: fetch.HF, Repo: h.Repo, Rev: "main"})
	if err != nil {
		t.Fatal(err)
	}
	r := &Remote{Name: "m"}
	for _, f := range l.Files {
		f := f
		r.Files = append(r.Files, RemoteFile{
			Path: f.Path, Size: f.Size, SHA256: f.SHA256, GitSHA1: f.GitSHA1,
			Open: func(ctx context.Context, off int64) (io.ReadCloser, error) {
				if opened != nil {
					atomic.AddInt64(opened, 1)
				}
				rc, err := fc.Open(ctx, f, off)
				if err != nil || failAt == nil {
					return rc, err
				}
				return &failingReader{rc: rc, path: f.Path, off: off, failAt: failAt}, nil
			},
		})
	}
	return r
}

type failingReader struct {
	rc     io.ReadCloser
	path   string
	off    int64
	failAt func(string, int64) error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if len(p) > 100 {
		p = p[:100]
	}
	if err := f.failAt(f.path, f.off); err != nil {
		return 0, err
	}
	n, err := f.rc.Read(p)
	f.off += int64(n)
	return n, err
}

func (f *failingReader) Close() error { return f.rc.Close() }

func streamClient(t *testing.T, host string, c *cache.Cache) *registry.Client {
	return registry.New(registry.Options{
		Keychain: authn.NewMultiKeychain(), Insecure: true, Transport: http.DefaultTransport,
		Cache: c, ChunkSize: 1024, Retries: 2,
	})
}

func writeLocal(t *testing.T, files []testutil.HubFile) string {
	dir := filepath.Join(t.TempDir(), "m")
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, f.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func packOpts(target, comp string, r *Remote, dir string) PackOptions {
	return PackOptions{
		Type: "weight", Root: "/models/org/m", Target: target, Compression: comp,
		Large: 4000, Small: 100, Remote: r, Dir: dir,
		TypeOpts: map[string]string{"source": "hf://org/m@main", "source-commit": "c0ffee"},
	}
}

// Streaming a remote model must yield exactly the slice that packing the
// same files from a local directory yields.
func TestRemotePackMatchesLocal(t *testing.T) {
	h := testutil.NewHub(t, "org/m", modelFiles())
	srv, _ := testutil.StatusRegistry(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	for _, comp := range []string{"none", "zstd", "gzip"} {
		t.Run(comp, func(t *testing.T) {
			c, _ := cache.New(t.TempDir())
			client := streamClient(t, host, c)
			keep := filepath.Join(t.TempDir(), "m")
			r := remoteFor(t, h, nil, nil)
			r.KeepDir = keep
			remote, err := Pack(context.Background(), client, packOpts(host+"/w/remote:"+comp, comp, r, "m"))
			if err != nil {
				t.Fatal(err)
			}
			if len(remote.Slice.Layers) < 3 {
				t.Fatalf("expected split layers, got %d", len(remote.Slice.Layers))
			}
			a := remote.Slice.Layers[0].Annotations
			if a[layer.TypeKey("weight", "source.commit")] != "c0ffee" || a[layer.TypeKey("weight", "files.digest")] == "" {
				t.Fatalf("annotations: %v", a)
			}
			for _, dir := range []string{writeLocal(t, modelFiles()), keep} {
				local, err := Pack(context.Background(), client, packOpts(host+"/w/local:"+comp, comp, nil, dir))
				if err != nil {
					t.Fatal(err)
				}
				if local.Digest != remote.Digest {
					t.Fatalf("%s: remote %s != local %s", dir, remote.Digest, local.Digest)
				}
			}
			matches, _ := filepath.Glob(filepath.Join(keep, "**", "*.partial"))
			if len(matches) != 0 {
				t.Fatalf("partials left: %v", matches)
			}
		})
	}
}

func TestRemotePackRejectsCorruptFile(t *testing.T) {
	h := testutil.NewHub(t, "org/m", modelFiles())
	h.CorruptPath = "model-00002-of-00002.safetensors"
	srv, _ := testutil.StatusRegistry(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	c, _ := cache.New(t.TempDir())
	_, err := Pack(context.Background(), streamClient(t, host, c), packOpts(host+"/w/x:1", "none", remoteFor(t, h, nil, nil), "m"))
	if err == nil || !strings.Contains(err.Error(), "upstream says") || !errors.Is(err, registry.ErrAbortUpload) {
		t.Fatalf("want verification error, got %v", err)
	}
}

// An interrupted upload resumes in a new Pack call (new process) from the
// persisted chunk offset and hash state, re-downloading only the rest.
func TestRemotePackResumes(t *testing.T) {
	h := testutil.NewHub(t, "org/m", modelFiles())
	srv, us := testutil.StatusRegistry(t)
	host := strings.TrimPrefix(srv.URL, "http://")
	cacheDir := t.TempDir()
	target := host + "/w/resume:1"
	interrupted := errors.New("simulated crash")

	c, _ := cache.New(cacheDir)
	fail := func(path string, off int64) error {
		if path == "model-00001-of-00002.safetensors" && off >= 3000 {
			return interrupted
		}
		return nil
	}
	_, err := Pack(context.Background(), streamClient(t, host, c), packOpts(target, "none", remoteFor(t, h, nil, fail), "m"))
	if !errors.Is(err, interrupted) {
		t.Fatalf("want interruption, got %v", err)
	}

	// Second run: the first lost PATCH ack must be reconciled too.
	us.FailPatch = 1
	var offsets []int64
	r := remoteFor(t, h, nil, func(path string, off int64) error {
		if path == "model-00001-of-00002.safetensors" {
			offsets = append(offsets, off)
		}
		return nil
	})
	c2, _ := cache.New(cacheDir)
	res, err := Pack(context.Background(), streamClient(t, host, c2), packOpts(target, "none", r, "m"))
	if err != nil {
		t.Fatal(err)
	}
	if len(offsets) == 0 || offsets[0] == 0 {
		t.Fatalf("big file was re-downloaded from 0 (offsets %v)", offsets)
	}
	srv2, _ := testutil.StatusRegistry(t)
	host2 := strings.TrimPrefix(srv2.URL, "http://")
	fresh, err := Pack(context.Background(), streamClient(t, host2, nil), packOpts(host2+"/w/resume:1", "none", remoteFor(t, h, nil, nil), "m"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Digest != fresh.Digest {
		t.Fatalf("resumed %s != fresh %s", res.Digest, fresh.Digest)
	}

	// Third run: every layer is known in the layer cache and present in
	// the registry, so LFS files are not downloaded at all.
	var opened int64
	c3, _ := cache.New(cacheDir)
	again, err := Pack(context.Background(), streamClient(t, host, c3), packOpts(target, "none", remoteFor(t, h, &opened, nil), "m"))
	if err != nil {
		t.Fatal(err)
	}
	// Only the small prefetched (non-LFS) files are fetched.
	if again.Digest != res.Digest || opened != 3 {
		t.Fatalf("cached rerun: digest %s vs %s, %d opens", again.Digest, res.Digest, opened)
	}
}

func TestRemoteDryRunPlansWithoutDownloading(t *testing.T) {
	h := testutil.NewHub(t, "org/m", modelFiles())
	var opened int64
	client, host := testEnv(t)
	res, err := Pack(context.Background(), client, PackOptions{
		Type: "weight", Root: "/m", Target: host + "/w/d:1", DryRun: true, Large: 4000, Small: 100,
		Remote: remoteFor(t, h, &opened, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened != 0 || len(res.Slice.Layers) < 3 || res.Size == 0 {
		t.Fatalf("dry run: %d opens, %+v", opened, res)
	}
}
