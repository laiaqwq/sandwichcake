package venv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/baba/sandwichcake/internal/testutil"
	"github.com/baba/sandwichcake/pkg/layer"
	"github.com/baba/sandwichcake/pkg/types"
)

// makeVenv builds a fake embedded venv under dir. home is the pyvenv.cfg
// home value; returns dir.
func makeVenv(t *testing.T, dir, home string) {
	t.Helper()
	mk := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("pyvenv.cfg", "home = "+home+"\nversion_info = 3.12.4\n")
	elf := testutil.BuildLinuxELF(t)
	data, _ := os.ReadFile(elf)
	py := filepath.Join(dir, ".python/bin")
	os.MkdirAll(py, 0o755)
	if err := os.WriteFile(filepath.Join(py, "python3.12"), data, 0o755); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	if err := os.Symlink("../.python/bin/python3.12", filepath.Join(dir, "bin", "python")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("python", filepath.Join(dir, "bin", "python3")); err != nil {
		t.Fatal(err)
	}
	mk("bin/pip-tool", "#!"+home+"/python3.12\nprint('x')\n")
	mk("lib/python3.12/site-packages/foo.pth", home+"/extra\n")
	mk("lib/python3.12/site-packages/foo-1.0.dist-info/METADATA", "Name: foo\nVersion: 1.0\n\n")
}

func newSource(t *testing.T, dir string, opts types.Opts) *types.Source {
	t.Helper()
	entries, err := layer.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &types.Source{
		Dir: dir, OrigDir: dir, Root: "/opt/venv",
		Entries: entries, Opts: opts, TmpDir: t.TempDir(), State: map[string]any{},
	}
}

func TestVenvPrepareAnnotate(t *testing.T) {
	dir := t.TempDir()
	makeVenv(t, dir, "/opt/venv/.python/bin")
	src := newSource(t, dir, types.Opts{Values: map[string]string{}})
	vt := venvType{}
	if err := vt.Prepare(context.Background(), src, src.Opts); err != nil {
		t.Fatal(err)
	}
	a := vt.Annotate(src)
	if a[key("python")] != "3.12.4" {
		t.Fatalf("python version: %v", a)
	}
	if a[key("python.mode")] != "embedded" {
		t.Fatalf("mode: %v", a)
	}
	if a[key("python.home")] != "/opt/venv/.python/bin" {
		t.Fatalf("home: %v", a)
	}
	if a[key("arch")] != "amd64" {
		t.Fatalf("arch: %v", a)
	}
	if a[key("libc")] != "glibc" {
		t.Fatalf("libc: %v", a)
	}
	md := vt.Metadata(src)
	var freeze bool
	for _, m := range md {
		if m.Name == "requirements.freeze.txt" {
			freeze = true
			if !strings.Contains(string(m.Data), "foo==1.0") {
				t.Fatalf("freeze: %s", m.Data)
			}
		}
	}
	if !freeze {
		t.Fatal("no freeze metadata")
	}
	// Layout: .python separate.
	rules := vt.Layout(src)
	if len(rules) != 1 || rules[0].Prefix != ".python" || !rules[0].Separate {
		t.Fatalf("layout: %v", rules)
	}
}

func TestVenvRewrite(t *testing.T) {
	dir := t.TempDir()
	makeVenv(t, dir, "/tmp/build/opt/venv/.python/bin")
	src := newSource(t, dir, types.Opts{
		Values:        map[string]string{},
		RewritePrefix: []types.Rewrite{{From: "/tmp/build/opt/venv", To: "/opt/venv"}},
	})
	vt := venvType{}
	if err := vt.Prepare(context.Background(), src, src.Opts); err != nil {
		t.Fatal(err)
	}
	// The copy was rewritten; the original untouched.
	cfg, _ := parsePyvenvCfg(filepath.Join(src.Dir, "pyvenv.cfg"))
	if cfg["home"] != "/opt/venv/.python/bin" {
		t.Fatalf("home after rewrite: %q", cfg["home"])
	}
	orig, _ := os.ReadFile(filepath.Join(dir, "pyvenv.cfg"))
	if !strings.Contains(string(orig), "/tmp/build") {
		t.Fatal("original was modified")
	}
	script, _ := os.ReadFile(filepath.Join(src.Dir, "bin", "pip-tool"))
	if !strings.HasPrefix(string(script), "#!/opt/venv/.python/bin/python3.12") {
		t.Fatalf("shebang: %q", script)
	}
	pth, _ := os.ReadFile(filepath.Join(src.Dir, "lib/python3.12/site-packages/foo.pth"))
	if !strings.Contains(string(pth), "/opt/venv/.python/bin/extra") {
		t.Fatalf("pth: %q", pth)
	}
}

func TestVenvMissingPython(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pyvenv.cfg"), []byte("home = /x\n"), 0o644)
	src := newSource(t, dir, types.Opts{Values: map[string]string{}})
	if err := (venvType{}).Prepare(context.Background(), src, src.Opts); err == nil {
		t.Fatal("expected failure")
	}
}
