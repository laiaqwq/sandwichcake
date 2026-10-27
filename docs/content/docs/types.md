---
title: Pack Types
weight: 4
---

Every `pack` subcommand is a registered **type**. A type decides the
default compression, how files are laid out into layers, which
annotations and metadata files the slice carries, and which checks run
at compose time. List them with `sandwichcake types`.

Common flags for all types are documented under
[`pack`](commands/pack.md). The sections below only list type-specific
options and defaults.

| Type | Default compression | Splitting |
|------|--------------------|-----------|
| venv | zstd | on |
| weight | none | on |
| binary | gzip | on |
| patch | gzip | on |
| other | gzip | on |

## venv

Packs a Python virtualenv.

| Flag | Default | Description |
|------|---------|-------------|
| `--python` | `embedded` | `embedded` = the venv carries its own interpreter; `system` = it expects the base image's |
| `--lock` | – | Path to a lock file (`uv.lock`, `poetry.lock`, `requirements.txt`) attached as slice metadata |
| `--from-lock` | – | Build the venv from a lock file using local `uv`; requires `--python <version>` |

Pack-time validation requires `pyvenv.cfg`, `bin/python` and
`lib/python3.X/site-packages`; `embedded` mode additionally requires a
`.python/` interpreter tree whose `pyvenv.cfg` `home` equals
`<root>/.python/bin` (fix with `--rewrite-prefix`). `.python/` always
packs into its own layer.

At pack time the venv is scanned for ELF binaries; the maximum
`glibc>=X.Y` requirement is recorded in the slice annotations and
checked against the base image at compose time (see
[libc resolution](cake-yaml.md#libc)). musl-linked ELFs only record
`venv.libc=musl` informationally; nothing checks it.

Detected architecture is recorded as `venv.arch` and feeds `--platform`
auto-detection (`linux/<arch>` when unset, error on conflict). A
`requirements.freeze.txt` metadata file is generated from
`*.dist-info/METADATA`. Composing a `python.mode=system` venv warns that
`python.home` cannot be verified against the base.

`--from-lock` resolves and installs the environment locally with `uv`
(you need `uv` on `PATH` and network access to the package index), then
packs the result — no manual venv needed. The venv is built for the
**host** platform: on macOS the binaries are Mach-O, which the ELF
scanner ignores, so libc requirements are not recorded and the result
will not run on Linux. Build venv slices for Linux on a Linux host:

```sh
sandwichcake pack venv --root /opt/venv \
  --from-lock uv.lock --python 3.12 \
  --to registry.example.com/ai/venv:torch-2.8
```

## weight

Packs model weights. Implements the Namer interface: with `--to-prefix`
instead of `--to`, the target ref is derived automatically from
`--source` (e.g. `hf://org/name@rev`).

| Flag | Default | Description |
|------|---------|-------------|
| `--source` | – | Provenance URI (`hf://org/name@rev`, `https://…`, `s3://…`) recorded in annotations; auto-naming is `hf://`-only (other sources fall back to `<prefix>/weight:<name>`) |
| `--source-commit` | – | Resolved upstream commit, recorded as `…weight.source.commit` (set automatically for remote sources) |
| `--include`, `--exclude`, `--keep-dir` | – | Remote sources only, see [Remote models](commands/pack.md#remote-models) |

Instead of a directory, `pack weight` accepts `hf://`, `modelscope://`,
`https://` or `http://` sources and streams the model straight into the
registry ([Remote models](commands/pack.md#remote-models)).

Weight layers default to no compression: quantized tensors do not
compress, so skipping gzip saves the pack time entirely
(`--compression` still overrides).

Every weight slice carries a `files.json` manifest (path, size, sha256
per file) whose digest is recorded as `weight.files.digest`, and the
dominant tensor extension in `weight.format`; a root `config.json` or
`model_index.json` is attached as a metadata file.

## binary

Packs a directory of compiled binaries/libraries. The embedded ELF
scanner extracts architecture, interpreter and the maximum `glibc>=X.Y`
requirement at pack time; mixed architectures in one directory are
rejected. `compose` reports an error when the slice's `binary.arch`
mismatches the target platform or a `glibc` requirement meets a musl
base, and a warning when the base libc cannot be verified (see
[libc resolution](cake-yaml.md#libc)). musl-linked ELFs produce no
`layer.requires`; the scan records musl only via `binary.elf.interp`.

## patch

Packs a file-level patch to overlay onto a base image.

| Flag | Default | Description |
|------|---------|-------------|
| `--base` | – | Expected base `repo@digest` this patch applies to; enforced at compose — a mismatch with the actual base is an error |

## other

Generic type with no extra semantics — use it for directory trees that
don't fit the specialized types.

## Writing your own type

Types implement the `Type` interface in `pkg/types` (`Name`, `Schema`,
`Prepare`, options, defaults, layout rules, annotations, metadata
files, compose-time checks) and register via `types.Register` in
`init()` — a type must be compiled into the binary via blank import
(see `pkg/types/builtin`). The built-ins under `pkg/types/` are the
reference implementations.
