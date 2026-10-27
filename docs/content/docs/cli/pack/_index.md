---
title: pack
weight: 3
aliases:
  - /docs/commands/pack/
---

Pack a local directory into a **slice** and push it to a registry.
`pack weight` can also stream a model from Hugging Face, ModelScope or a
URL straight into the registry.

## Synopsis

```text
sandwichcake pack <type> <dir> [--root <path>] [--to <ref>] [flags]
```

| Subcommand | Packs |
|------------|-------|
| [`pack venv`](venv.md) | A Python virtualenv (or builds one from a lock file) |
| [`pack weight`](weight.md) | Model weights, local or remote |
| [`pack binary`](binary.md) | Compiled binaries and libraries |
| [`pack patch`](patch.md) | An overlay onto the base image |
| [`pack other`](other.md) | Any other directory tree |

What each type means is described in
[Pack Types](../../general/concepts/pack-types.md); the subcommands are
generated from the compiled-in types (`sandwichcake types`).

## Arguments

| Argument | Description |
|----------|-------------|
| `dir` | Directory to pack. Its *contents* land under `--root`; the directory itself is never modified (types that rewrite content work on a temp copy) |

## Target and root

- **`--root`** is required unless the project sets `pack.<type>.root`
  or `pack.defaults.root`, or — for `pack weight` with an `hf://` or
  `modelscope://` source — `models.paths.<provider>` supplies it.
- **Target.** `--to` sets the full target ref. Without it, a project
  derives the target from `registry.prefix`, `registry.repos`,
  `repoFormat` and `tagFormat` (see
  [Naming](../../general/concepts/projects.md#naming)). `--to-prefix`
  bypasses the project naming and uses the type's legacy Namer
  convention. Outside a project, one of `--to` or `--to-prefix` is
  needed.

Explicit flags always override `cake-config.yaml` pack defaults.

## Common flags {#common-flags}

Accepted by every `pack` subcommand:

| Flag | Default | Description |
|------|---------|-------------|
| `--root <path>` | project setting | Container root path (absolute), e.g. `/opt/venv` |
| `--to <ref>` | project naming | Target `repo:tag` (or `repo@digest`) |
| `--to-prefix <registry/repo>` | – | Prefix for legacy auto-naming; overrides project `registry.prefix` |
| `--compression <alg>` | per type | `gzip` \| `zstd` \| `none` |
| `--level <n>` | algorithm default | Compression level |
| `--no-split` | off | Disable size-based layer splitting (type layout rules still apply) |
| `--large-threshold <size>` | `5Gi` | Files at or above this get their own layer |
| `--small-threshold <size>` | `64Mi` | Files below this are grouped into a shared layer |
| `--platform <os/arch>` | detected / `any` | Any `os/arch` (e.g. `linux/amd64`) or `any`; auto-detected from ELF arch where the type scans it — a conflicting explicit value is an error |
| `--rewrite-prefix <src=dst>` | – | Rewrite path strings inside file contents; `venv` only, ignored by other types (repeatable) |
| `--owner <uid:gid>` | `0:0` | Owner of all layer entries |
| `--chmod <file:dir>` | preserved | Octal modes, e.g. `644:755` |
| `--no-parents` | off | Do not write ancestor directory entries above `--root` |
| `--artifact-compat` | off | Force the fallback-index metadata artifact with a standard image config type |
| `--merge` | off | Merge the manifest into an existing index at the target tag instead of replacing it |
| `--dry-run` | off | Build layers locally (in a temp dir) and print the digest; push nothing |

Plus the [global flags](../_index.md#global-flags). Type-specific flags
are on each subcommand's page.

## What happens

1. **Scan** — `<dir>` is walked; files, symlinks and modes are recorded.
2. **Prepare** — the type's prepare hook runs: validation, `venv
   --from-lock` builds the venv with local `uv`, `venv` applies
   `--rewrite-prefix` to file contents (tar entry names are never
   rewritten). Content changes happen on a private temp copy.
3. **Split** — entries are grouped into layers by the type's layout
   rules plus the size thresholds, unless `--no-split`
   ([Layer splitting](../../general/concepts/slices.md#layer-splitting)).
4. **Build** — deterministic tars are built in parallel and compressed
   ([Determinism](../../general/concepts/determinism-and-locks.md)).
5. **Annotate** — every layer descriptor gets
   `com.github.baba.sandwichcake.*` annotations
   ([reference](../../general/reference/annotations.md)).
6. **Push** — layers, config and manifest go to the target (merged into
   the existing index with `--merge`). If the type produces metadata
   files (`venv`, `weight`), a metadata artifact is pushed with the
   manifest as `subject`.
7. **Print** — the pushed ref, manifest digest and metadata artifact.

In a project, `registry.onTagExists` is applied before pushing (not
with `--merge` or `--dry-run`).

## Output

```text
pushed registry.example.com/ai/weights:qwen3-8b
  digest sha256:3a1f…
  metadata registry.example.com/ai/weights@sha256:77e2…
```

With `--dry-run`:

```text
dry-run: registry.example.com/tools:latest -> sha256:9c04… (1 layers)
```

## Examples

Preview what would be built:

```sh
sandwichcake pack binary ./dist/bin --root /usr/local/bin --dry-run \
  --to registry.example.com/tools:latest
```

Pack a second architecture into the same tag's index:

```sh
sandwichcake pack binary ./dist-arm64 --root /usr/local/bin \
  --platform linux/arm64 --merge \
  --to registry.example.com/tools:latest
```

`--merge` adds the manifest to the target tag: an existing index gets
the entry (replacing any same-platform entry), an existing single
manifest becomes a two-entry index, and an unused tag gets a fresh
index.

More examples are on each type's page.
