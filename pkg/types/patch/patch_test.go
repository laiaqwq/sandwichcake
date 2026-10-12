package patch

import (
	"context"
	"testing"

	"github.com/laiaqwq/sandwichcake/pkg/layer"
	"github.com/laiaqwq/sandwichcake/pkg/types"
)

func TestPatch(t *testing.T) {
	src := &types.Source{Opts: types.Opts{Values: map[string]string{"base": "r/img@sha256:abc"}}, State: map[string]any{}}
	a := (patchType{}).Annotate(src)
	if a[layer.KeyLayerOverlay] != "true" {
		t.Fatalf("overlay: %v", a)
	}
	if a[key("base")] != "r/img@sha256:abc" {
		t.Fatalf("base: %v", a)
	}
	s := &types.Slice{Annotations: a}
	ok := (patchType{}).Check(context.Background(), s, &types.ComposeCtx{BaseRef: "r/img@sha256:abc"})
	if len(ok) != 0 {
		t.Fatalf("check: %v", ok)
	}
	bad := (patchType{}).Check(context.Background(), s, &types.ComposeCtx{BaseRef: "r/other@sha256:def"})
	if len(bad) != 1 || bad[0].Level != types.Error {
		t.Fatalf("expected error: %v", bad)
	}
}
