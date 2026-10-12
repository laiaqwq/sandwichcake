package layer

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, dir, rel, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func tarEntries(t *testing.T, g Group, opts TarOptions) map[string]*tar.Header {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteTar(&buf, g, opts); err != nil {
		t.Fatal(err)
	}
	out := map[string]*tar.Header{}
	tr := tar.NewReader(&buf)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		out[h.Name] = h
	}
	return out
}

func TestScanWhiteout(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".wh.foo", "x", 0o644)
	if _, err := Scan(dir); err == nil {
		t.Fatal("expected whiteout error")
	}
}

func TestScanSorted(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "b.txt", "x", 0o644)
	writeFile(t, dir, "a/c.txt", "x", 0o644)
	writeFile(t, dir, "a.txt", "x", 0o644)
	entries, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Rel >= entries[i].Rel {
			t.Fatalf("not sorted: %v", entries)
		}
	}
}

func TestTarDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "hello", 0o644)
	writeFile(t, dir, "sub/b.sh", "#!/bin/sh\n", 0o755)
	if err := os.Symlink("a.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	entries, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	g := Group{Entries: entries}
	opts := TarOptions{Root: "/opt/x", MTime: time.Unix(0, 0)}

	var b1, b2 bytes.Buffer
	if err := WriteTar(&b1, g, opts); err != nil {
		t.Fatal(err)
	}
	// Change on-disk mtime; output must be identical.
	os.Chtimes(filepath.Join(dir, "a.txt"), time.Now(), time.Now())
	if err := WriteTar(&b2, g, opts); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1.Bytes(), b2.Bytes()) {
		t.Fatal("tar not deterministic")
	}

	hs := tarEntries(t, g, opts)
	for name := range hs {
		if name[0] == '/' {
			t.Fatalf("leading slash in %q", name)
		}
	}
	// Ancestors above root.
	if h := hs["opt/"]; h == nil || h.Mode != 0o755 || h.Uid != 0 {
		t.Fatalf("bad ancestor: %+v", h)
	}
	if h := hs["opt/x/"]; h == nil || h.Typeflag != tar.TypeDir {
		t.Fatal("missing root dir entry")
	}
	if h := hs["opt/x/sub/"]; h == nil || h.Mode != 0o755 {
		t.Fatalf("bad sub dir: %+v", h)
	}
	if h := hs["opt/x/a.txt"]; h == nil || h.Mode != 0o644 || h.Size != 5 {
		t.Fatalf("bad file: %+v", h)
	}
	if h := hs["opt/x/sub/b.sh"]; h == nil || h.Mode != 0o755 {
		t.Fatalf("bad exec file: %+v", h)
	}
	if h := hs["opt/x/link"]; h == nil || h.Typeflag != tar.TypeSymlink || h.Linkname != "a.txt" {
		t.Fatalf("bad symlink: %+v", h)
	}
	for name, h := range hs {
		if !h.ModTime.Equal(time.Unix(0, 0)) || !h.AccessTime.IsZero() || !h.ChangeTime.IsZero() {
			t.Fatalf("bad times on %s: %+v", name, h)
		}
	}
}

func TestTarNoParents(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "hi", 0o644)
	entries, _ := Scan(dir)
	hs := tarEntries(t, Group{Entries: entries}, TarOptions{Root: "/opt/x", NoParents: true})
	if _, ok := hs["opt/"]; ok {
		t.Fatal("NoParents still wrote ancestor")
	}
	if _, ok := hs["opt/x/"]; !ok {
		t.Fatal("root dir missing")
	}
}

func TestSplit(t *testing.T) {
	mk := func(rel string, size int64) Entry {
		return Entry{Rel: rel, Size: size, Mode: 0o644}
	}
	entries := []Entry{
		mk("a/big.bin", 10), // large (>=8)
		mk("b/m1.bin", 6),   // medium [4,8)
		mk("b/m2.bin", 5),   // medium
		mk("c/s1.txt", 1),   // small
		mk("c/s2.txt", 1),
		mk(".python/bin/py", 6),
		mk(".python/lib/x", 1),
		mk("drop/me", 1),
	}
	groups := Split(entries, SplitOptions{
		Large: 8, Small: 4,
		Rules: []Rule{
			{Prefix: "drop", Exclude: true},
			{Prefix: ".python", Separate: true},
		},
	})
	// .python partition first: medium [py] then small [x]. Then remainder:
	// large [big], medium bins [m1],[m2] (6+5 > 8 cap), small bin [s1,s2].
	if len(groups) != 6 {
		t.Fatalf("got %d groups: %+v", len(groups), groups)
	}
	want := [][]string{
		{".python/bin/py"},
		{".python/lib/x"},
		{"a/big.bin"},
		{"b/m1.bin"},
		{"b/m2.bin"},
		{"c/s1.txt", "c/s2.txt"},
	}
	for i, w := range want {
		if len(groups[i].Entries) != len(w) {
			t.Fatalf("group%d: %v", i, groups[i].Entries)
		}
		for j, rel := range w {
			if groups[i].Entries[j].Rel != rel {
				t.Fatalf("group%d: %v", i, groups[i].Entries)
			}
		}
	}
	for _, g := range groups {
		for _, e := range g.Entries {
			if e.Rel == "drop/me" {
				t.Fatal("excluded entry present")
			}
		}
		if g.Part == 0 || g.PartsTotal != 6 {
			t.Fatalf("part info wrong: %+v", g)
		}
	}
}

func TestSplitDisabled(t *testing.T) {
	entries := []Entry{
		{Rel: "a", Size: 100, Mode: 0o644},
		{Rel: ".python/x", Size: 100, Mode: 0o644},
	}
	groups := Split(entries, SplitOptions{
		Disabled: true, Large: 8, Small: 4,
		Rules: []Rule{{Prefix: ".python", Separate: true}},
	})
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %d", len(groups))
	}
}

func TestBuildNoneDigestEqualsDiffID(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "hello", 0o644)
	entries, _ := Scan(dir)
	l, err := Build(Group{Entries: entries}, TarOptions{Root: "/x"}, CompressionNone, 0, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	d, _ := l.Digest()
	id, _ := l.DiffID()
	if d != id {
		t.Fatal("none compression: digest != diff_id")
	}
	// Manual check: diff_id == sha256 of uncompressed stream.
	rc, _ := l.Uncompressed()
	defer rc.Close()
	h := sha256.New()
	io.Copy(h, rc)
	if hex.EncodeToString(h.Sum(nil)) != id.Hex {
		t.Fatal("diff_id mismatch")
	}
}

func TestBuildGzip(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "hello", 0o644)
	entries, _ := Scan(dir)
	l, err := Build(Group{Entries: entries}, TarOptions{Root: "/x"}, CompressionGzip, 0, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	d, _ := l.Digest()
	id, _ := l.DiffID()
	if d == id {
		t.Fatal("gzip: digest == diff_id")
	}
	mt, _ := l.MediaType()
	if mt != "application/vnd.oci.image.layer.v1.tar+gzip" {
		t.Fatalf("mediaType %s", mt)
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{
		"5Gi": 5 << 30, "64Mi": 64 << 20, "1024": 1024,
		"1G": 1000_000_000, "2Ki": 2048,
	} {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Fatalf("ParseSize(%q)=%d,%v want %d", in, got, err, want)
		}
	}
}
