package elfscan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/baba/sandwichcake/internal/testutil"
)

func TestScanStaticELF(t *testing.T) {
	path := testutil.BuildLinuxELF(t)
	info, ok, err := ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("not detected as ELF")
	}
	if info.Arch != "amd64" {
		t.Fatalf("arch %q", info.Arch)
	}
	if info.Musl {
		t.Fatal("musl on static binary")
	}
	// CGO_ENABLED=0 static binary: no interp, no glibc requirement.
	if info.Interp != "" {
		t.Fatalf("interp %q", info.Interp)
	}
	if info.GlibcMin != [2]int{} {
		t.Fatalf("glibc %v", info.GlibcMin)
	}
}

func TestScanNonELF(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.txt")
	os.WriteFile(p, []byte("not an elf"), 0o644)
	_, ok, err := ScanFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("text file detected as ELF")
	}
}
