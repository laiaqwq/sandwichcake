package types

import (
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func TestCheckRequires(t *testing.T) {
	ctx := ComposeCtx{Platform: v1.Platform{OS: "linux", Architecture: "amd64"}, BaseLibc: "glibc 2.35"}
	if f := CheckRequires("glibc>=2.17", ctx); len(f) != 0 {
		t.Fatalf("unexpected findings: %v", f)
	}
	if f := CheckRequires("glibc>=2.38", ctx); len(f) != 1 || f[0].Level != Error {
		t.Fatalf("want error: %v", f)
	}
	ctx.BaseLibc = "musl"
	if f := CheckRequires("glibc>=2.17", ctx); len(f) != 1 || f[0].Level != Error {
		t.Fatalf("musl: want error: %v", f)
	}
	ctx.BaseLibc = ""
	if f := CheckRequires("glibc>=2.17", ctx); len(f) != 1 || f[0].Level != Warn {
		t.Fatalf("unknown base: want warn: %v", f)
	}
	ctx.BaseLibc = "glibc 2.35"
	if f := CheckRequires("nvidia>=1", ctx); len(f) != 1 || f[0].Level != Warn {
		t.Fatalf("unknown kind: want warn: %v", f)
	}
	if f := CheckRequires("glibc>=2.17, glibc>=2.30", ctx); len(f) != 0 {
		t.Fatalf("multi: %v", f)
	}
}

func TestEnsureCopy(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644)
	os.Symlink("a.txt", filepath.Join(dir, "l"))
	src := &Source{Dir: dir, OrigDir: dir, TmpDir: t.TempDir()}
	if err := src.EnsureCopy(); err != nil {
		t.Fatal(err)
	}
	if src.Dir == dir {
		t.Fatal("expected a copy")
	}
	if _, err := os.Lstat(filepath.Join(src.Dir, "l")); err != nil {
		t.Fatal("symlink not preserved")
	}
	// Mutating the copy must not touch the original.
	os.WriteFile(filepath.Join(src.Dir, "a.txt"), []byte("changed"), 0o644)
	b, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(b) != "hi" {
		t.Fatal("original modified")
	}
	// Idempotent.
	d := src.Dir
	if err := src.EnsureCopy(); err != nil || src.Dir != d {
		t.Fatal("not idempotent")
	}
}
