---
title: Writing a Type
weight: 5
---

A pack type is a Go package that implements `types.Type` and registers
itself in `init()`. Registered types automatically get a
`pack <name>` subcommand, flags generated from their options, and a
line in `sandwichcake types`. Types are compiled in — there is no
runtime plugin loading.

The built-ins under `pkg/types/` are the reference implementations;
`pkg/types/other` is the minimal one.

## The interface

```go
// pkg/types/types.go
type Type interface {
    Name() string                                      // "venv"; also the annotation namespace …<name>.*
    Schema() int                                       // version of the type's annotations → layer.schema
    Options() []Option                                 // type-specific flags for `pack <name>`
    Defaults() Defaults                                // compression, level, split, thresholds
    Prepare(ctx context.Context, src *Source, opts Opts) error
    Layout(src *Source) []layer.Rule                   // extra layer-splitting rules
    Annotate(src *Source) map[string]string            // type annotations + layer.requires
    Metadata(src *Source) []MetadataFile               // files for the metadata artifact
    Check(ctx context.Context, s *Slice, cctx *ComposeCtx) []Finding
}

// Optional: derive a target ref for --to-prefix.
type Namer interface {
    DefaultTarget(src *Source, prefix string) (string, error)
}
```

| Method | Called | Notes |
|--------|--------|-------|
| `Options` | CLI setup | Each `Option{Name, Usage, Default, Bool}` becomes a flag; values arrive in `Opts.Values` |
| `Defaults` | pack | Overridden by project `pack.*` and flags |
| `Prepare` | pack, before splitting | Validate the tree, rewrite content. Call `src.EnsureCopy()` before modifying files so the user's directory is untouched, and `src.Rescan()` after changes. Share data with later hooks via `src.State` |
| `Layout` | pack | `layer.Rule{Prefix, Separate, Exclude}`: `Separate` forces a subtree into its own layer(s), `Exclude` drops it. Rules can only make splitting finer |
| `Annotate` | pack | Return keys built with `layer.TypeKey(name, key)`, plus `layer.KeyLayerRequires` (e.g. `glibc>=2.28`). Only `patch` may set `layer.KeyLayerOverlay` — the framework rejects it from other types |
| `Metadata` | pack | `MetadataFile{Name, MediaType, Data}`; any files trigger a metadata artifact. Keep large data here rather than in annotations (manifest size limits) |
| `Check` | compose, per platform | Return `Finding{Level: types.Warn\|types.Error, Msg}`. `cctx` has the platform, base ref/digest, base env and resolved base libc. `layer.requires` is checked by the framework — don't repeat it |

## Skeleton

```go
package mytype

import (
    "context"

    "github.com/laiaqwq/sandwichcake/pkg/layer"
    "github.com/laiaqwq/sandwichcake/pkg/types"
)

type myType struct{}

func init() { types.Register(myType{}) }

func key(k string) string { return layer.TypeKey("mytype", k) }

func (myType) Name() string { return "mytype" }
func (myType) Schema() int  { return 1 }
func (myType) Options() []types.Option {
    return []types.Option{{Name: "flavor", Usage: "which flavor to record", Default: ""}}
}
func (myType) Defaults() types.Defaults {
    return types.Defaults{
        Compression: layer.CompressionGzip,
        Split:       true,
        Large:       layer.DefaultLargeThreshold,
        Small:       layer.DefaultSmallThreshold,
    }
}
func (myType) Prepare(ctx context.Context, src *types.Source, opts types.Opts) error { return nil }
func (myType) Layout(src *types.Source) []layer.Rule                                { return nil }
func (myType) Annotate(src *types.Source) map[string]string {
    if f := src.Opts.Get("flavor", ""); f != "" {
        return map[string]string{key("flavor"): f}
    }
    return nil
}
func (myType) Metadata(src *types.Source) []types.MetadataFile { return nil }
func (myType) Check(ctx context.Context, s *types.Slice, cctx *types.ComposeCtx) []types.Finding {
    return nil
}
```

Register it in the binary with a blank import in
`pkg/types/builtin/builtin.go`:

```go
import _ "github.com/laiaqwq/sandwichcake/pkg/types/mytype"
```

## Checklist

- **Determinism.** `Prepare`, `Annotate` and `Metadata` must be pure
  functions of the tree and options — no timestamps, hostnames,
  absolute temp paths or map-iteration order in output.
- **Schema.** Bump `Schema()` when the meaning of your annotations
  changes, and keep reading older schemas in `Check`.
- **Compatibility.** Older binaries compose your slices without
  knowing the type (they warn and skip `Check`), so the generic
  annotations must be enough to compose safely.
- **Project config.** Add the name to `validPackType` in
  `cmd/sandwichcake/project_config.go` so `pack.<name>` and
  `registry.repos.<name>` are accepted.
- **Docs.** Add `docs/content/docs/cli/pack/<name>.md` (the
  `TestCLIDocs` check fails without it), a section in
  [Pack Types](../general/concepts/pack-types.md) and the annotations
  in [Annotations & Media Types](../general/reference/annotations.md).
- **Tests.** Follow `pkg/types/<t>/<t>_test.go`; `internal/testutil`
  can build ELF fixtures and run a fake registry.
