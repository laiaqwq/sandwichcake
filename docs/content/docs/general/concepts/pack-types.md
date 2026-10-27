---
title: Pack Types
weight: 2
aliases:
  - /docs/types/
---

Every `pack` subcommand is a registered **type**. A type decides the
default compression, how files are laid out into layers, which
annotations and metadata files the slice carries, and which checks run
at compose time. List the compiled-in types with
[`sandwichcake types`](../../cli/types.md).

Types are a **pack-time** concept: `compose` only relies on the generic
annotations (`layer.root`, `layer.overlay`, `layer.requires`) plus each
known type's compose-time check. Slices of a type the running binary
does not know still compose; only that type's specific checks are
skipped.

| Type | Use for | Default compression | Extra layout | Metadata artifact | Compose-time check |
|------|---------|--------------------|--------------|-------------------|--------------------|
| [venv](#venv) | Python virtualenvs | zstd | `.python/` in its own layer | freeze output, optional lock file | `glibc` requirement; `system` mode warns |
| [weight](#weight) | Model weights | none | – | `files.json`, `config.json`/`model_index.json` | – |
| [binary](#binary) | Compiled binaries and libraries | gzip | – | – | arch vs target platform; `glibc` requirement |
| [patch](#patch) | Overlays onto the base | gzip | – | – | `--base` digest must match |
| [other](#other) | Anything else | gzip | – | – | – |

All types split layers by default (see [Slices](slices.md#layer-splitting)).
Flags per type are listed in the [`pack` CLI reference](../../cli/pack/_index.md).

## venv

Packs a Python virtualenv. CLI: [`pack venv`](../../cli/pack/venv.md).

**Embedded interpreter.** By default (`--python embedded`) the venv
carries its own interpreter — typically a
[python-build-standalone](https://github.com/astral-sh/python-build-standalone)
distribution, the same one `uv` installs — under `<root>/.python/`, with
`pyvenv.cfg` `home = <root>/.python/bin`. The venv is then a single
self-contained tree that does not depend on the base image's Python;
the only requirement left on the base is a compatible libc.
`--python system` packs a venv that uses the base image's interpreter
instead.

```text
/opt/venv/
  .python/                      interpreter (bin/, lib/, …) — own layer
  bin/python -> ../.python/bin/python3.12
  lib/python3.12/site-packages/
  pyvenv.cfg                    home = /opt/venv/.python/bin
```

`.python/` always packs into its own layer, so venvs with the same
interpreter version and root share that layer's diff_id.

**Validation.** Pack requires `pyvenv.cfg`, `bin/python` and
`lib/python3.X/site-packages`; `embedded` mode additionally requires the
`.python/` tree and the matching `home`.

**Absolute paths.** A venv embeds absolute paths (`pyvenv.cfg`,
shebangs, `*.pth`, the interpreter's `_sysconfigdata_*.py` and
`config-*/Makefile`). If the venv was built somewhere other than
`--root`, `--rewrite-prefix <built-path>=<root>` rewrites those file
contents (see [Slices](slices.md#path-rewriting)).

**libc and arch.** At pack time the venv is scanned for ELF binaries;
the maximum `glibc>=X.Y` requirement is recorded in `layer.requires`
and checked against the base at compose time (see
[libc resolution](composition.md#libc)). musl-linked ELFs only record
`venv.libc=musl` informationally; nothing checks it. The detected
architecture is recorded as `venv.arch` and feeds `--platform`
auto-detection (`linux/<arch>` when unset, error on conflict).

**Metadata.** A `requirements.freeze.txt` file is generated from
`*.dist-info/METADATA`; `--lock` attaches a `uv.lock`, `poetry.lock` or
`requirements.txt` and records its kind and digest in annotations.

**`--from-lock`** resolves and installs the environment locally with
`uv` (you need `uv` on `PATH` and network access to the package index),
then packs the result — no manual venv needed. The venv is built for
the **host** platform: on macOS the binaries are Mach-O, which the ELF
scanner ignores, so libc requirements are not recorded and the result
will not run on Linux. Build venv slices for Linux on a Linux host.

**Compose.** Composing a `python.mode=system` venv warns that
`python.home` cannot be verified against the base.

## weight

Packs model weights. CLI: [`pack weight`](../../cli/pack/weight.md).

- **Sources.** A local directory, or a remote `hf://`, `modelscope://`
  or `http(s)://` source streamed straight into the registry — see
  [Remote Models](remote-models.md).
- **Provenance.** `--source` records the upstream URI
  (`weight.source`); `--source-commit` (set automatically for remote
  sources) records the resolved commit.
- **No compression by default.** Weight tensors barely compress, so
  skipping gzip saves the pack time entirely (`--compression` still
  overrides).
- **Metadata.** Every weight slice carries a `files.json` manifest
  (path, size, sha256 per file) whose digest is recorded as
  `weight.files.digest`, and the dominant tensor extension in
  `weight.format`; a root `config.json` or `model_index.json` is
  attached as a metadata file.
- **Naming.** In a project, the target is derived from
  `registry.repos.weight` and the source (`org--model`); see
  [Projects](projects.md#naming). Without a project, `--to-prefix`
  derives it via the type's Namer (`hf://` only; other sources fall
  back to `<prefix>/weight:<name>`).

## binary

Packs a directory of compiled binaries/libraries. CLI:
[`pack binary`](../../cli/pack/binary.md).

The embedded ELF scanner extracts architecture, interpreter and the
maximum `glibc>=X.Y` requirement at pack time; mixed architectures in
one directory are rejected. `compose` reports an error when the slice's
`binary.arch` mismatches the target platform or a `glibc` requirement
meets a musl base, and a warning when the base libc cannot be verified
(see [libc resolution](composition.md#libc)). musl-linked ELFs produce
no `layer.requires`; the scan records musl only via
`binary.elf.interp`.

## patch

Packs a file-level patch to overlay onto a base image — configuration
replacements, hotfixes. CLI: [`pack patch`](../../cli/pack/patch.md).

`patch` is the only type that sets `layer.overlay=true`, which exempts
its layers from the root-overlap check, so it may deliberately
overwrite paths of the base or of other slices. `--base <repo@digest>`
pins the base the patch was made for; at compose time a mismatch with
the actual base is an error.

## other

Generic type with no extra semantics — use it for directory trees that
don't fit the specialized types. CLI: [`pack other`](../../cli/pack/other.md).

## Custom types

Types are Go packages compiled into the binary. See
[Writing a Type](../../developer-guide/writing-a-type.md).
