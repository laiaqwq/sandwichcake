---
title: pack venv
linkTitle: venv
weight: 1
---

Pack a Python virtualenv, or build one from a lock file and pack it.

## Synopsis

```text
sandwichcake pack venv <dir> --root <path> [flags]
sandwichcake pack venv --from-lock <lockfile> --python <version> --root <path> [flags]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `dir` | The virtualenv directory. Omitted with `--from-lock` |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--python <mode\|version>` | `embedded` | `embedded`: the venv carries its own interpreter under `.python/`; `system`: it uses the base image's interpreter. With `--from-lock`, a Python version such as `3.12` |
| `--lock <file>` | – | Attach `uv.lock`, `poetry.lock` or `requirements.txt` as slice metadata |
| `--from-lock <file>` | – | Build the venv from a lock file with local `uv` (needs `--python <version>`); the result is always `embedded` |
| `--rewrite-prefix <src=dst>` | – | Rewrite absolute paths inside `pyvenv.cfg`, `bin/` scripts, `*.pth` and the interpreter's sysconfig files (repeatable) |

Default compression: `zstd`. Plus the
[common pack flags](_index.md#common-flags) and the
[global flags](../_index.md#global-flags).

## What it checks and records

- Requires `pyvenv.cfg`, `bin/python` and `lib/python3.X/site-packages`;
  `embedded` additionally requires `.python/` and
  `pyvenv.cfg` `home = <root>/.python/bin`.
- `.python/` always goes into its own layer.
- Scans ELF files: the maximum `glibc>=X.Y` requirement becomes
  `layer.requires`; the architecture becomes `venv.arch` and the
  default `--platform`.
- Generates `requirements.freeze.txt` metadata.

Details: [Pack Types → venv](../../general/concepts/pack-types.md#venv).

## Examples

Pack an existing venv that was built at its final path:

```sh
sandwichcake pack venv /opt/venv --root /opt/venv \
  --lock uv.lock \
  --to registry.example.com/ai/venv:torch-2.8
```

Pack a venv built elsewhere, rewriting its embedded paths:

```sh
sandwichcake pack venv ./build/venv --root /opt/venv \
  --rewrite-prefix "$PWD/build/venv=/opt/venv" \
  --to registry.example.com/ai/venv:torch-2.8
```

Build from `uv.lock` without creating the venv yourself (needs `uv`;
run on Linux for Linux images):

```sh
sandwichcake pack venv --root /opt/venv \
  --from-lock uv.lock --python 3.12 \
  --to registry.example.com/ai/venv:torch-2.8
```
