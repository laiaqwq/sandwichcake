---
title: Composition
weight: 3
---

`compose` turns a `cake.yaml` into an image: it resolves the base and
every slice, checks that they fit together, stacks their layers, patches
the config and pushes the result. It never creates layers — every blob
in the output already exists in some registry. CLI:
[`compose`](../../cli/compose.md); file format:
[cake.yaml](../reference/cake-yaml.md).

## Layer sources {#layer-sources}

| Source | Where it comes from | Position | Safety |
|--------|---------------------|----------|--------|
| **Base** | Every layer of `base.ref`, in original order | Always first | Safe — the base's own diff chain stays intact |
| **File** | Layers of a packed slice | Any, in `slices` order | Safe — self-contained, no whiteouts, roots checked for overlap |
| **External** | Layers selected (`layers: [...]`) from a plain, non-slice image | Any | Must be judged a *pure file layer* |

An external layer counts as a pure file layer when:

1. it carries a `com.github.baba.sandwichcake.layer.type` annotation (it
   was packed by sandwichcake), or
2. its `history[].created_by` is a `COPY`/`ADD` step (BuildKit or legacy
   `#(nop) COPY` form).

Anything else (`RUN`, missing or misaligned history) is rejected unless
the entry sets `force: true`. `verify: true` additionally downloads the
layer and rejects `.wh.*` whiteout entries. External layers get
`source.image` / `source.index` annotations recording where they came
from.

## Layer order and dedup

The output's layers are: base layers, then each slice's layers in
`slices` order (honoring `layers:` selections). Layers with an
identical diff_id are deduplicated — the first occurrence wins.

## Config merge

The base image's config is the starting point; the `config:` patch is
applied on top:

| Field | Rule |
|-------|------|
| `env` | Per key. A scalar sets/replaces; `{prepend: x}` / `{append: x}` joins with the base value using `:`. OCI `Env` is not shell-expanded at runtime, so the join is computed at compose time and written literally. |
| `entrypoint`, `cmd` | Replaced when given; `[]` clears the base value; omitted keeps it. |
| `workdir`, `user` | Replaced when given. |
| `labels` | Merged; patch keys win. |
| `history` | Base history verbatim (including `empty_layer` entries), then one entry per kept slice layer. A base whose history does not align with its layers produces a warning. |
| `rootfs.diff_ids` | Follows the layer order above. |

Docker-specific fields (`Healthcheck`, `Shell`, `OnBuild`) are kept
as-is.

## Checks

Before anything is pushed, compose runs metadata-only checks (plus tar
scans for `verify`ed layers):

- **Type checks** — each known slice type runs its own check: `glibc`
  requirements against the [base libc](#libc), binary arch vs target
  platform, patch `--base` digest. See [Pack Types](pack-types.md).
- **Root overlap** — non-overlay layer roots must not nest or coincide
  (identical roots within one slice are exempt). `patch` layers
  (`layer.overlay=true`) are exempt.
- **Verified paths** — tar paths of `verify`ed plain-image layers must
  not land in another slice's root or recur in two verified layers.
- **Platform** — every target platform must resolve for every ref; a
  mismatch is a hard error at resolve time.

Findings are `warn:` (printed, compose continues) or errors (compose
aborts as `Error: compose: …`). `force` never downgrades a finding; it
only bypasses the pure-file judgment.

## libc resolution {#libc}

Slices packed by `binary`/`venv` may declare a `glibc>=X.Y` requirement.
`compose` resolves the base libc in this order:

1. `base.libc` in cake.yaml — an explicit declaration wins.
2. The `com.github.baba.sandwichcake.libc` manifest annotation or config
   label on the base.
3. `--verify` (or `verify: true` on the base): base layer tars are
   downloaded and scanned for `ld-musl-*`, `libc-X.Y.so`, then
   `libc.so.6` contents ("GNU C Library … version X.Y"); later layers
   win.
4. Otherwise libc is unknown — `glibc` requirements produce warnings;
   a requirement against musl is an error.

## Platforms {#platforms}

The platform list is `target.platforms`, else the base's: every
platform in a base index, or the base config's os/arch for a
single-manifest base. One platform produces a single manifest; more
than one produces an OCI index with one manifest per platform.

For each platform, every ref is resolved to its matching manifest. A
ref's `platform:` picks an `os/arch` from an index, or `any` for a
platform-independent manifest. Platform-independent slices (weights)
are shared by all platform manifests, so their blobs are transferred
once.

## Output format

The output is always an **OCI** manifest/index, even when the base is a
Docker schema2 image: base layer descriptors are rewritten to the
equivalent OCI media types (blobs and digests unchanged) and the config
is re-serialized as an OCI config. Foreign (Windows) layers and Windows
base images are rejected.

One manifest may mix compressions (gzip base, uncompressed weights, zstd
venv). Docker ≥ 23, containerd and CRI-O support this; for older Docker
daemons `compose --compat docker20` rejects zstd layers.

The output manifest is annotated with:

- `com.github.baba.sandwichcake.slices` — the `repo@digest` of every
  slice used;
- `org.opencontainers.image.base.name` / `.base.digest` — the base's
  original ref and digest.

These managed keys override anything set in `target.annotations`.
