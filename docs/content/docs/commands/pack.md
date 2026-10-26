---
title: pack
weight: 1
---

`sandwichcake pack <type> <dir>` packs a local directory into a
**slice** and pushes it to a registry. Types: `venv`, `weight`,
`binary`, `patch`, `other` — see [Pack Types](../types.md).
`pack weight` can also stream a model from Hugging Face, ModelScope or a
URL straight into the registry — see [Remote models](#remote-models).

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

## Remote models {#remote-models}

```sh
sandwichcake pack weight <source> [--include glob]... [--exclude glob]... [--keep-dir dir] [flags]
```

`<source>` may be any of:

| Source | Meaning |
|--------|---------|
| `hf://org/name[@rev]` | Whole Hugging Face model (default rev `main`) |
| `https://huggingface.co/org/name[/tree/<rev>[/<dir>]]` | Same; `/tree/<rev>/<dir>` keeps only that subdirectory (paths stay repo-relative) |
| `https://huggingface.co/org/name/blob/<rev>/<file>` (or `/resolve/`) | One file |
| `modelscope://org/name[@rev]`, `https://modelscope.cn/models/org/name[/files][?Revision=<rev>]` | ModelScope model (default rev `master`) |
| any other `https://…/file[#sha256=<hex>]` | One file; the optional fragment is verified |

URLs on the configured mirror host (`download.hf.endpoint`, e.g.
`https://hf-mirror.com`) are recognized as Hugging Face URLs. Only models
are supported (not datasets or spaces).

Files are downloaded and uploaded **at the same time**: the deterministic
tar of each layer is computed from the file list up front, every file is
verified against the upstream sha256 (LFS) or git blob id while it
streams, and the layer is sent with chunked OCI uploads whose digest is
computed on the fly. Nothing is staged on disk except small non-LFS files
(configs, tokenizers), which are fetched first. Memory use is bounded by
`download.concurrency` × (`download.connections` × `download.segmentSize`
+ one upload chunk).

- **Naming and provenance** — the slice is named like a local pack with
  `--source hf://org/name@<rev>`: the rev you wrote goes into `{rev}` (and
  the tag), the resolved commit is recorded in the
  `…weight.source.commit` annotation. Repository path components are
  lowercased (OCI requires it); tags keep their case. `models.paths`
  supplies the default `--root`.
- **Filtering** — `--include` / `--exclude` take gitignore-style globs on
  repo paths; they replace `pack.weight.include` / `pack.weight.exclude`
  from `cake-config.yaml`. Without includes, every file is packed.
- **Resume** — an interrupted run continues where it stopped when rerun:
  downloads resume with HTTP range requests, and uncompressed layers
  (the weight default) resume their upload at the last acknowledged chunk
  in a new process, using hash state saved in the metadata cache.
  Compressed layers restart from zero. Layers that already exist in the
  registry are skipped without downloading.
- **Verification failures** cancel the upload session; nothing is
  committed.
- **`--keep-dir <dir>`** also writes the verified files to `<dir>`.
  Packing that directory locally (named like the model, with the same
  `--source` and `--source-commit`) reproduces the same digests.
- **`--dry-run`** lists the files and prints the layer plan without
  downloading content.

Example:

```sh
$ export HF_TOKEN=hf_…            # gated/private models only
$ sandwichcake pack weight https://huggingface.co/Qwen/Qwen3-8B/tree/main \
    --exclude 'original/**' --to registry.example.com/ai/weights/qwen--qwen3-8b:main
layer 1/6: streaming model-00001-of-00005.safetensors (3995 MB uncompressed)
…
pushed registry.example.com/ai/weights/qwen--qwen3-8b:main
```

Tokens and endpoints: see [Authentication](_index.md#authentication) and
[`config`](config.md#local-settings-and-precedence).

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
