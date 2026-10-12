// Package other implements the "other" catch-all type.
package other

import (
	"context"

	"github.com/baba/sandwichcake/pkg/layer"
	"github.com/baba/sandwichcake/pkg/types"
)

type otherType struct{}

func init() { types.Register(otherType{}) }

func (otherType) Name() string            { return "other" }
func (otherType) Schema() int             { return 1 }
func (otherType) Options() []types.Option { return nil }
func (otherType) Defaults() types.Defaults {
	return types.Defaults{
		Compression: layer.CompressionGzip,
		Split:       true,
		Large:       layer.DefaultLargeThreshold,
		Small:       layer.DefaultSmallThreshold,
	}
}
func (otherType) Prepare(ctx context.Context, src *types.Source, opts types.Opts) error {
	return nil
}
func (otherType) Layout(src *types.Source) []layer.Rule           { return nil }
func (otherType) Annotate(src *types.Source) map[string]string    { return nil }
func (otherType) Metadata(src *types.Source) []types.MetadataFile { return nil }
func (otherType) Check(ctx context.Context, s *types.Slice, cctx *types.ComposeCtx) []types.Finding {
	return nil
}
