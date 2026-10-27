---
title: pack binary
linkTitle: binary
weight: 3
---

Pack a directory of compiled binaries and libraries.

## Synopsis

```text
sandwichcake pack binary <dir> --root <path> [flags]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `dir` | Directory of executables and/or shared libraries |

## Flags

No type-specific flags. Default compression: `gzip`. See the
[common pack flags](_index.md#common-flags) and the
[global flags](../_index.md#global-flags).

## What it checks and records

- ELF files are scanned for architecture, interpreter and the maximum
  `glibc>=X.Y` requirement. Mixed architectures in one directory are
  rejected.
- Records `binary.arch`, `binary.elf.interp` and `layer.requires`; the
  architecture becomes the default `--platform`.
- At compose time, an arch mismatch with the target platform or a glibc
  requirement against a musl base is an error.

Details: [Pack Types → binary](../../general/concepts/pack-types.md#binary).

## Examples

```sh
sandwichcake pack binary ./dist/bin --root /usr/local/bin \
  --to registry.example.com/tools/uv:0.5
```

Multi-arch under one tag:

```sh
sandwichcake pack binary ./dist-amd64 --root /usr/local/bin --merge --to registry.example.com/tools/uv:0.5
sandwichcake pack binary ./dist-arm64 --root /usr/local/bin --merge --to registry.example.com/tools/uv:0.5
```
