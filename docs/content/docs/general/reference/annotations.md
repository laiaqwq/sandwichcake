---
title: Annotations & Media Types
weight: 5
---

All sandwichcake annotation keys live in the
`com.github.baba.sandwichcake.` namespace (abbreviated `…` below). The
namespace is fixed; keys are only ever added. Inspect them with
[`inspect`](../../cli/inspect.md) or `inspect --json`.

## Manifest annotations

| Key | Set on | Value |
|-----|--------|-------|
| `…kind` | Slice manifests | `slice` — what `ls` and `compose` use to recognize a slice |
| `…platform` | Platform-independent slices | `any` |
| `…slices` | Composed images | Comma-separated `repo@digest` of every slice used |
| `…libc` | Base images (set by you or your base build) | `glibc X.Y` or `musl`; read during [libc resolution](../concepts/composition.md#libc). Also honored as a config label |
| `org.opencontainers.image.base.name` | Composed images | The base's original ref (tag kept, stable across `--locked`) |
| `org.opencontainers.image.base.digest` | Composed images | The base's resolved digest |

## Layer annotations (generic)

Written by the framework for every packed layer and carried unchanged
into composed images, so `inspect` on a final image shows where each
layer came from. `compose`, `plan` and `inspect` only rely on these.

| Key | Value |
|-----|-------|
| `…layer.type` | Pack type: `venv`, `weight`, `binary`, `patch`, `other`, … |
| `…layer.schema` | Version of the type's annotation format, e.g. `1` |
| `…layer.root` | Container root of the layer's files, e.g. `/opt/venv` |
| `…layer.name` | Logical name (directory or model name) |
| `…layer.part` | Position when split, e.g. `3/7` |
| `…layer.files` | Number of files in the layer |
| `…layer.overlay` | `true` for layers allowed to overwrite other paths (`patch` only) |
| `…layer.requires` | Comma-separated runtime requirements on the base, e.g. `glibc>=2.17` |
| `…tool.version` | sandwichcake version that packed the layer |
| `…source.image` | External layers only (stamped at compose time): source image `repo@digest` |
| `…source.index` | External layers only: layer index in the source image |

## Layer annotations (type-specific)

Keys under `…<type>.`; each type defines and interprets its own.

| Key | Value |
|-----|-------|
| `…venv.python` | Interpreter version, e.g. `3.12.4` |
| `…venv.python.mode` | `embedded` or `system` |
| `…venv.python.home` | `pyvenv.cfg` `home` value |
| `…venv.lock.kind` | Kind of the attached `--lock` file |
| `…venv.lock.digest` | `sha256:` of the attached lock file |
| `…venv.libc` | `glibc`, `glibc X.Y` or `musl` (informational) |
| `…venv.arch` | Detected ELF architecture |
| `…weight.source` | Provenance URI (`--source`) |
| `…weight.source.commit` | Resolved upstream commit |
| `…weight.format` | Dominant tensor format, e.g. `safetensors` |
| `…weight.files.digest` | `sha256:` of `files.json` |
| `…binary.arch` | Detected ELF architecture |
| `…binary.elf.interp` | Comma-separated ELF interpreters |
| `…patch.base` | Expected base `repo@digest` (`--base`) |

## Media types

Slice and composed-image layers use only standard OCI media types:

| Media type | Used for |
|------------|----------|
| `application/vnd.oci.image.layer.v1.tar` | Uncompressed layers (`--compression none`, weight default) |
| `application/vnd.oci.image.layer.v1.tar+gzip` | gzip layers |
| `application/vnd.oci.image.layer.v1.tar+zstd` | zstd layers |
| `application/vnd.oci.image.manifest.v1+json`, `…index.v1+json` | All manifests sandwichcake writes |

sandwichcake-specific types appear only in metadata artifacts and
project artifacts:

| Media type | Used for |
|------------|----------|
| `application/vnd.sandwichcake.metadata.v1` | `artifactType` of slice metadata artifacts |
| `application/vnd.sandwichcake.files+json` | Weight `files.json` |
| `application/vnd.sandwichcake.uv.lock` | Attached `uv.lock` |
| `application/vnd.sandwichcake.poetry.lock` | Attached `poetry.lock` |
| `application/vnd.sandwichcake.pip.requirements` | Attached `requirements.txt` |
| `application/vnd.sandwichcake.pip.freeze` | Generated `requirements.freeze.txt` |
| `application/vnd.sandwichcake.project.v1` | `artifactType` of projects pushed with `config push oci://…` |

Metadata files carry their file name in
`org.opencontainers.image.title`; `inspect --file <title>` fetches them.
