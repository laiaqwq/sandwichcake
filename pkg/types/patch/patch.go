// Package patch implements the "patch" type: overlay layers that
// intentionally replace files from the base.
package patch

import (
	"context"
	"fmt"
	"strings"

	"github.com/baba/sandwichcake/pkg/layer"
	"github.com/baba/sandwichcake/pkg/types"
)

type patchType struct{}

func init() { types.Register(patchType{}) }

func (patchType) Name() string { return "patch" }
func (patchType) Schema() int  { return 1 }
func (patchType) Options() []types.Option {
	return []types.Option{
		{Name: "base", Usage: "expected base repo@digest this patch applies to (optional)"},
	}
}
func (patchType) Defaults() types.Defaults {
	return types.Defaults{
		Compression: layer.CompressionGzip,
		Split:       true,
		Large:       layer.DefaultLargeThreshold,
		Small:       layer.DefaultSmallThreshold,
	}
}

func key(k string) string { return layer.TypeKey("patch", k) }

func (patchType) Prepare(ctx context.Context, src *types.Source, opts types.Opts) error {
	return nil
}

func (patchType) Layout(src *types.Source) []layer.Rule { return nil }

func (patchType) Annotate(src *types.Source) map[string]string {
	a := map[string]string{
		layer.KeyLayerOverlay: "true",
	}
	if b := src.Opts.Get("base", ""); b != "" {
		a[key("base")] = b
	}
	return a
}

func (patchType) Metadata(src *types.Source) []types.MetadataFile { return nil }

// digestPart extracts the digest portion of a ref (after '@') or returns the
// string unchanged (a bare digest).
func digestPart(ref string) string {
	if i := strings.LastIndex(ref, "@"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func (patchType) Check(ctx context.Context, s *types.Slice, cctx *types.ComposeCtx) []types.Finding {
	base := s.Annotations[key("base")]
	if base == "" || cctx == nil {
		return nil
	}
	want := digestPart(base)
	for _, ref := range []string{cctx.BaseRef, cctx.BaseRefDigest} {
		if ref != "" && digestPart(ref) == want {
			return nil
		}
	}
	return []types.Finding{{
		Level: types.Error,
		Msg:   fmt.Sprintf("patch expects base %s but composing onto %s", base, cctx.BaseRef),
	}}
}
