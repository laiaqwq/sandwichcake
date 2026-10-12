// Package testutil provides shared test helpers.
package testutil

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"

	"github.com/laiaqwq/sandwichcake/pkg/registry"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/static"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"
)

// TarBytes builds a simple tar of regular files (and .wh. entries if asked).
func TarBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// MountRegistry wraps the ggcr fake registry, which does not implement
// cross-repo blob mounts, and serves `mount=` requests itself by copying the
// blob through the inner handler.
type MountRegistry struct{ Inner http.Handler }

func (m MountRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost &&
		strings.HasSuffix(r.URL.Path, "/blobs/uploads/") &&
		r.URL.Query().Get("mount") != "" {
		q := r.URL.Query()
		digest, from := q.Get("mount"), q.Get("from")
		repo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v2/"), "/blobs/uploads/")
		rr := httptest.NewRecorder()
		m.Inner.ServeHTTP(rr, httptest.NewRequest(http.MethodGet,
			"/v2/"+from+"/blobs/"+digest, nil))
		if rr.Code != http.StatusOK {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		rr2 := httptest.NewRecorder()
		m.Inner.ServeHTTP(rr2, httptest.NewRequest(http.MethodPost,
			"/v2/"+repo+"/blobs/uploads/", nil))
		loc := rr2.Header().Get("Location")
		sep := "?"
		if strings.Contains(loc, "?") {
			sep = "&"
		}
		rr3 := httptest.NewRecorder()
		m.Inner.ServeHTTP(rr3, httptest.NewRequest(http.MethodPut,
			loc+sep+"digest="+digest, rr.Body))
		if rr3.Code != http.StatusCreated {
			w.WriteHeader(rr3.Code)
			return
		}
		w.Header().Set("Location", "/v2/"+repo+"/blobs/"+digest)
		w.WriteHeader(http.StatusCreated)
		return
	}
	m.Inner.ServeHTTP(w, r)
}

// Registry spins up a fake registry (with mount + referrers emulation).
// Returns the server and a client pointed at it (insecure).
func Registry(t *testing.T) (*httptest.Server, *registry.Client) {
	t.Helper()
	s := httptest.NewServer(MountRegistry{ggcrregistry.New(ggcrregistry.WithReferrersSupport(true))})
	t.Cleanup(s.Close)
	return s, registry.New(registry.Options{
		Keychain:  authn.NewMultiKeychain(),
		Insecure:  true,
		Transport: http.DefaultTransport,
	})
}

// RepoURL returns "host/repo" for the test server.
func RepoURL(s *httptest.Server, repo string) string {
	return strings.TrimPrefix(s.URL, "http://") + "/" + repo
}

// GzipLayer returns a gzip-compressed OCI layer containing the tar.
func GzipLayer(t *testing.T, files map[string]string) v1.Layer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(TarBytes(t, files)); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return static.NewLayer(buf.Bytes(), gtypes.OCILayer)
}

// DockerLayer returns a layer with docker schema2 media type.
func DockerLayer(t *testing.T, files map[string]string) v1.Layer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(TarBytes(t, files)); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return static.NewLayer(buf.Bytes(), gtypes.DockerLayer)
}

// BuildLinuxELF compiles a trivial static Linux binary and returns its path.
func BuildLinuxELF(t *testing.T) string { return BuildLinuxELFArch(t, "amd64") }

// BuildLinuxELFArch compiles a static Linux binary for the given GOARCH.
func BuildLinuxELFArch(t *testing.T, arch string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "prog")
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0", "GO111MODULE=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	return out
}
