---
title: pack
weight: 1
---

`sandwichcake pack <type> <dir>` packs a local directory into a
**slice** and pushes it to a registry. Types: `venv`, `weight`,
`binary`, `patch`, `other` — see [Pack Types](../types.md).

## What happens

1. **Scan** — `<dir>` is walked; files, symlinks and modes are recorded.
2. **Prepare** — the type's prepare hook runs. Types that need to modify
   content work on a private temp copy (your directory is never touched).
   For `venv --from-lock` the venv is built here from the lock file with
   local `uv`.
3. **Split** — entries are grouped into layers by the type's layout
   rules plus `--small-threshold` / `--large-threshold`, unless
   `--no-split`.
4. **Build** — deterministic tars are built in parallel: timestamps
   come from `SOURCE_DATE_EPOCH` (else Unix 0), ownership/modes are
   normalized per `--owner` / `--chmod` / `--no-parents`, path prefixes
   are rewritten per `--rewrite-prefix`, then each layer is compressed
   (`gzip` / `zstd` / `none`, per-type default).
5. **Annotate** — every layer descriptor gets
   `com.github.baba.sandwichcake.*` annotations (type, schema, root,
   name, part, file count, tool version, type-specific keys).
6. **Push** — the image manifest goes to `--to` (or merges into the
   existing index with `--merge`), then a metadata artifact is pushed
   with the manifest as `subject` — an OCI 1.1 referrer where supported,
   else the `sha256-<digest>` fallback index (`--artifact-compat` forces
   the fallback).
7. **Print** — the pushed ref, manifest digest, and metadata artifact
   ref.

Nothing is pushed with `--dry-run`; layers are built under a temp dir
and discarded.

## Usage

```sh
sandwichcake pack <type> <dir> --root <path> [flags]
```

`--root` is required unless `pack.<type>.root` or a model path template
is set in the project's `cake-config.yaml`. `--to` sets a full target;
`--to-prefix` explicitly enables legacy prefix auto-naming. When a
project config sets `registry.prefix`, `registry.repos`, and optional
`registry.repoFormat`/`registry.tagFormat`, they determine automatic
targets. Explicit flags take precedence. See [`config`](config.md).

## Flags

| Flag | Description |
|------|-------------|
| `--root` | Container root path (e.g. `/opt/venv`); required unless a project root or model path is configured |
| `--to` | Target `repo:tag`; overrides saved auto-naming |
| `--to-prefix` | `registry/repo` prefix for auto-naming (overrides project `registry.prefix`) |
| `--compression` | `gzip` \| `zstd` \| `none` (per-type default) |
| `--level` | Compression level |
| `--no-split` | Disable automatic layer splitting |
| `--large-threshold` | Large-file threshold (default `5Gi`) |
| `--small-threshold` | Small-file threshold (default `64Mi`) |
| `--platform` | `linux/amd64` \| `any`; auto-detected from ELF arch where the type scans it — a conflicting explicit value is an error |
| `--rewrite-prefix` | `src=dst` path prefix rewrite (repeatable) |
| `--owner` | `uid:gid` for all layer files |
| `--chmod` | `fileMode:dirMode` octal, e.g. `644:755` |
| `--no-parents` | Do not write ancestor directory entries |
| `--artifact-compat` | Force fallback-index metadata + standard image config type |
| `--merge` | Merge the manifest into an existing index at the target tag |
| `--dry-run` | Build locally, do not push |

Type-specific flags (e.g. `venv --python/--lock/--from-lock`,
`weight --source`, `patch --base`) are listed by
[`sandwichcake types`](types.md) and on the [types](../types.md) page.

## Examples

Pack a model directory as a weight slice:

```sh
$ sandwichcake pack weight ./models/qwen3-8b \
    --root /models/qwen3-8b \
    --to registry.example.com/ai/weights:qwen3-8b \
    --source hf://Qwen/Qwen3-8B@main
pushed registry.example.com/ai/weights:qwen3-8b
  digest sha256:3a1f…
  metadata sha256:77e2…
```

Same thing with auto-naming (Namer derives the ref from `--source`):

```sh
sandwichcake pack weight ./models/qwen3-8b \
  --root /models/qwen3-8b \
  --to-prefix registry.example.com/ai/weights \
  --source hf://Qwen/Qwen3-8B@main
```

Build a venv from a lock file without creating it yourself:

```sh
sandwichcake pack venv --root /opt/venv \
  --from-lock uv.lock --python 3.12 \
  --to registry.example.com/ai/venv:torch-2.8
```

Preview what would be built (no push):

```sh
$ sandwichcake pack binary ./dist/bin --root /usr/local/bin --dry-run \
    --to registry.example.com/tools:latest
dry-run: registry.example.com/tools:latest -> sha256:9c04… (1 layers)
```

Re-pack for a second architecture into the same tag's index:

```sh
sandwichcake pack binary ./dist-arm64 --root /usr/local/bin \
  --platform linux/arm64 --merge \
  --to registry.example.com/tools:latest
```
