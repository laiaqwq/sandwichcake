---
title: Architecture
weight: 1
---

sandwichcake is a Go module (`github.com/laiaqwq/sandwichcake`) built on
[go-containerregistry](https://github.com/google/go-containerregistry)
v0.22.1. The CLI is a thin cobra layer over library packages that can
also be used directly.

## Package layout

```text
cmd/sandwichcake   cobra CLI: init, config (show/get/set/unset/pull/push), pack, compose,
                   plan, inspect, ls, discover, tag, diff, rm, types, version
pkg/layer          deterministic tar, Scan/Split, compression, annotation keys
pkg/types          Type interface + registry, Opts/Source, CheckRequires, ELF scan
pkg/types/<t>      built-in types: venv, weight, binary, patch, other (+ builtin)
pkg/registry       ggcr remote wrapper: chunked resumable upload, mount, referrers
pkg/fetch          remote model download: HF/ModelScope/HTTP listing + ranged streams
pkg/slice          pack flow (local + remote streaming), slice parsing, metadata artifacts
pkg/cake           cake.yaml / cake.lock schemas + validation (file I/O only)
pkg/resolve        cake ref -> resolved layers (pure-file judgment, verify, libc)
pkg/compose        PURE: base + slices + config patch -> v1.Image / index
pkg/plan           per-blob reuse/mount/copy classification and rendering
pkg/runner         orchestration: resolve -> compose -> plan -> confirm -> push -> lock
pkg/inspect        read/render reports, metadata file fetch, diff
pkg/version        build version (set via -ldflags)
internal/cache     metadata-only JSON file cache
internal/testutil  shared test helpers: fake registry with mount emulation, ELF builder, HF/ModelScope fake
internal/doctest   docs link and CLI-coverage checks (test-only)
internal/releasetest  release workflow contract tests (test-only)
```

## Pack flow

```text
cmd pack <type> ─▶ slice.Pack
                    ├─ layer.Scan(dir)
                    ├─ Type.Prepare   (temp copy, validation, rewrites, uv build)
                    ├─ layer.Split    (size bins + Type.Layout rules)
                    ├─ layer build    (deterministic tar, compression, in parallel)
                    ├─ Type.Annotate / Type.Metadata
                    └─ registry       (blobs, manifest/index, metadata artifact)
```

For remote weights, `pkg/fetch` lists the upstream repo and opens
ranged streams; `pkg/slice/stream.go` computes the tar layout from the
file list and feeds a resumable `registry.BlobStream`, so download and
upload happen at once.

## Compose flow

```text
cmd compose ─┬─ cake.Load / cake.LoadLock   (strict parse; short refs expanded in cmd)
             ├─ runner.Compose
             │   ├─ resolve.Resolve       (per platform: base, slices, libc, verify)
             │   ├─ compose.Compose       (pure: layers, config patch, checks, annotations)
             │   ├─ plan.Compute          (registry.Classify per blob)
             │   ├─ confirm               (prompt or --yes)
             │   └─ registry push         (reuse / mount / chunked upload)
             └─ Lock.Save                 (cake.lock next to the cake file)
```

## Boundaries

- **All network I/O goes through `pkg/registry`** (registries) or
  `pkg/fetch` (model sources). Nothing else opens connections.
- **`pkg/compose` does no I/O.** It takes resolved descriptors and
  returns lazy `v1.Image`/`v1.ImageIndex` values; blobs are only read
  when pushed. This keeps composition unit-testable.
- **Layers are never wrapped.** `remote.MountableLayer` carries the
  source repository that cross-repo mounts need; compose passes layer
  objects through untouched so mount capability survives.
- **Types are pack-time plugins.** `compose` relies on generic
  annotations and calls `Type.Check` only for types it knows.
- **The cache holds metadata only** — never layer blobs.

## Configuration in the CLI

`cmd/sandwichcake` owns all configuration handling:

| File | Responsibility |
|------|----------------|
| `config.go` | Environment config (user + local), env overrides, `config` show/get/set/unset/init |
| `project_config.go` | `cake-config.yaml` loading, validation, naming, short refs, model paths |
| `project_init.go` | `init` and `--migrate` |
| `config_sync.go` | Sync set, `.cakeignore`, state, `config pull/push` |
| `config_oci.go`, `config_git.go` | OCI and git sync backends |
| `pack_remote.go` | Remote weight flags and listing |
| `target_policy.go` | `registry.onTagExists` |
