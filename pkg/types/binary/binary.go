// Package binary implements the "binary" type: prebuilt ELF programs/libs.
package binary

import (
	"context"
	"fmt"

	"github.com/baba/sandwichcake/pkg/layer"
	"github.com/baba/sandwichcake/pkg/types"
	"github.com/baba/sandwichcake/pkg/types/internal/elfscan"
)

type binaryType struct{}

func init() { types.Register(binaryType{}) }

func (binaryType) Name() string            { return "binary" }
func (binaryType) Schema() int             { return 1 }
func (binaryType) Options() []types.Option { return nil }
func (binaryType) Defaults() types.Defaults {
	return types.Defaults{
		Compression: layer.CompressionGzip,
		Split:       true,
		Large:       layer.DefaultLargeThreshold,
		Small:       layer.DefaultSmallThreshold,
	}
}

func key(k string) string { return layer.TypeKey("binary", k) }

func (binaryType) Prepare(ctx context.Context, src *types.Source, opts types.Opts) error {
	if src.State == nil {
		src.State = map[string]any{}
	}
	arch, interps, glibc, err := scan(src)
	if err != nil {
		return err
	}
	src.State["arch"] = arch
	src.State["interps"] = interps
	src.State["glibc"] = glibc
	return nil
}

func scan(src *types.Source) (arch string, interps map[string]bool, glibc [2]int, err error) {
	interps = map[string]bool{}
	for _, e := range src.Entries {
		if !e.IsRegular() {
			continue
		}
		info, ok, err := elfscan.ScanFile(e.Abs)
		if err != nil {
			return "", nil, glibc, err
		}
		if !ok {
			continue
		}
		if info.Arch != "" {
			if arch == "" {
				arch = info.Arch
			} else if arch != info.Arch {
				return "", nil, glibc, fmt.Errorf("binary: mixed architectures %s and %s (%s)", arch, info.Arch, e.Rel)
			}
		}
		if info.Interp != "" {
			interps[info.Interp] = true
		}
		if info.GlibcMin[0] > glibc[0] || (info.GlibcMin[0] == glibc[0] && info.GlibcMin[1] > glibc[1]) {
			glibc = info.GlibcMin
		}
	}
	return arch, interps, glibc, nil
}

func (binaryType) Layout(src *types.Source) []layer.Rule { return nil }

func (binaryType) Annotate(src *types.Source) map[string]string {
	a := map[string]string{}
	if arch, _ := src.State["arch"].(string); arch != "" {
		a[key("arch")] = arch
	}
	if interps, _ := src.State["interps"].(map[string]bool); len(interps) > 0 {
		var list []string
		for i := range interps {
			list = append(list, i)
		}
		a[key("elf.interp")] = joinComma(list)
	}
	if glibc, _ := src.State["glibc"].([2]int); glibc != [2]int{} {
		a[layer.KeyLayerRequires] = fmt.Sprintf("glibc>=%d.%d", glibc[0], glibc[1])
	}
	return a
}

func joinComma(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

func (binaryType) Metadata(src *types.Source) []types.MetadataFile { return nil }

func (binaryType) Check(ctx context.Context, s *types.Slice, cctx *types.ComposeCtx) []types.Finding {
	var out []types.Finding
	arch := s.Annotations[key("arch")]
	if arch != "" && cctx != nil && cctx.Platform.Architecture != "" &&
		cctx.Platform.Architecture != "unknown" && arch != cctx.Platform.Architecture {
		out = append(out, types.Finding{
			Level: types.Error,
			Msg:   fmt.Sprintf("binary arch %s does not match platform %s", arch, cctx.Platform.Architecture),
		})
	}
	return out
}
