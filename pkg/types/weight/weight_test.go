package weight

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/types"
)

func TestWeight(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "model.safetensors"), []byte("weights"), 0o644)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o644)
	entries, _ := layer.Scan(dir)
	src := &types.Source{Dir: dir, OrigDir: dir, Root: "/models/x",
		Entries: entries,
		Opts:    types.Opts{Values: map[string]string{"source": "hf://org/name@rev1"}},
		State:   map[string]any{}}
	wt := weightType{}
	if err := wt.Prepare(context.Background(), src, src.Opts); err != nil {
		t.Fatal(err)
	}
	a := wt.Annotate(src)
	if a[key("source")] != "hf://org/name@rev1" {
		t.Fatalf("source: %v", a)
	}
	if a[key("format")] != "safetensors" {
		t.Fatalf("format: %v", a)
	}
	if !strings.HasPrefix(a[key("files.digest")], "sha256:") {
		t.Fatalf("files.digest: %v", a)
	}
	md := wt.Metadata(src)
	var files, cfg bool
	for _, m := range md {
		if m.Name == "files.json" {
			files = true
			if !strings.Contains(string(m.Data), "model.safetensors") {
				t.Fatalf("files.json: %s", m.Data)
			}
		}
		if m.Name == "config.json" {
			cfg = true
		}
	}
	if !files || !cfg {
		t.Fatalf("metadata: %v", md)
	}
	target, err := wt.DefaultTarget(src, "reg.example.com/u/weights")
	if err != nil {
		t.Fatal(err)
	}
	if target != "reg.example.com/u/weights/org--name:rev1" {
		t.Fatalf("target %q", target)
	}
}
