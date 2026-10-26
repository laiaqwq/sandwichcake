package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("SANDWICHCAKE_CACHE_DIR", "")

	// HOME fallback.
	if got, want := Dir(), filepath.Join(os.Getenv("HOME"), ".cache", "sandwichcake"); got != want {
		t.Fatalf("HOME fallback: %q, want %q", got, want)
	}

	// XDG_CACHE_HOME beats HOME.
	xdg := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)
	if got, want := Dir(), filepath.Join(xdg, "sandwichcake"); got != want {
		t.Fatalf("XDG: %q, want %q", got, want)
	}

	// SANDWICHCAKE_CACHE_DIR beats everything.
	custom := t.TempDir()
	t.Setenv("SANDWICHCAKE_CACHE_DIR", custom)
	if got := Dir(); got != custom {
		t.Fatalf("env override: %q, want %q", got, custom)
	}
}

func TestSetGetDelete(t *testing.T) {
	c, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Missing key -> not ok.
	var m map[string]string
	if c.Get(KindManifest, "sha256:absent", &m) {
		t.Fatal("Get on missing key returned true")
	}

	// Round trip.
	want := map[string]string{"digest": "sha256:abc"}
	if err := c.Set(KindManifest, "sha256:deadbeef", want); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if !c.Get(KindManifest, "sha256:deadbeef", &got) || got["digest"] != want["digest"] {
		t.Fatalf("roundtrip: %v", got)
	}

	// Digest/path-ish keys are sanitized into flat filenames.
	if err := c.Set(KindUpload, "repo/sub@sha256:xyz", UploadSession{Location: "u"}); err != nil {
		t.Fatal(err)
	}

	c.Delete(KindManifest, "sha256:deadbeef")
	if c.Get(KindManifest, "sha256:deadbeef", &got) {
		t.Fatal("Get after Delete returned true")
	}
}

func TestCorruptAndSanitize(t *testing.T) {
	dir := t.TempDir()
	c, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt JSON -> not ok.
	if err := c.Set(KindDiffID, "k", "v"); err != nil {
		t.Fatal(err)
	}
	// Find the file cache wrote and corrupt it.
	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		files = append(files, e.Name())
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 cache file, got %v", files)
	}
	if strings.ContainsAny(files[0], "/:@\\") {
		t.Fatalf("filename not sanitized: %q", files[0])
	}
	if err := os.WriteFile(filepath.Join(dir, files[0]), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	var s string
	if c.Get(KindDiffID, "k", &s) {
		t.Fatal("Get on corrupt JSON returned true")
	}
}
