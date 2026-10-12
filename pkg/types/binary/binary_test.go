package binary

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/baba/sandwichcake/internal/testutil"
	"github.com/baba/sandwichcake/pkg/layer"
	"github.com/baba/sandwichcake/pkg/types"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func TestBinaryAnnotateAndCheck(t *testing.T) {
	dir := t.TempDir()
	data, _ := os.ReadFile(testutil.BuildLinuxELF(t))
	os.WriteFile(filepath.Join(dir, "prog"), data, 0o755)
	entries, _ := layer.Scan(dir)
	src := &types.Source{Dir: dir, OrigDir: dir, Root: "/usr/local/bin",
		Entries: entries, Opts: types.Opts{Values: map[string]string{}}, State: map[string]any{}}
	bt := binaryType{}
	if err := bt.Prepare(context.Background(), src, src.Opts); err != nil {
		t.Fatal(err)
	}
	a := bt.Annotate(src)
	if a[key("arch")] != "amd64" {
		t.Fatalf("arch: %v", a)
	}
	s := &types.Slice{Annotations: a}
	ok := bt.Check(context.Background(), s, &types.ComposeCtx{Platform: v1.Platform{OS: "linux", Architecture: "amd64"}})
	if len(ok) != 0 {
		t.Fatalf("check: %v", ok)
	}
	bad := bt.Check(context.Background(), s, &types.ComposeCtx{Platform: v1.Platform{OS: "linux", Architecture: "arm64"}})
	if len(bad) != 1 || bad[0].Level != types.Error {
		t.Fatalf("expected arch error: %v", bad)
	}
}
