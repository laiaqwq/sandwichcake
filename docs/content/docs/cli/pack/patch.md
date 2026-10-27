---
title: pack patch
linkTitle: patch
weight: 4
---

Pack files that deliberately overwrite paths of the base image or of
other slices — configuration replacements, hotfixes.

## Synopsis

```text
sandwichcake pack patch <dir> --root <path> [--base <repo@digest>] [flags]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `dir` | Directory whose contents overlay `--root` |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--base <repo@digest>` | – | Base this patch was made for. At compose time, a different base is an error |

Default compression: `gzip`. Plus the
[common pack flags](_index.md#common-flags) and the
[global flags](../_index.md#global-flags).

## What it records

Every layer gets `layer.overlay=true`, which exempts it from the
root-overlap check, and `patch.base` when `--base` is set. Details:
[Pack Types → patch](../../general/concepts/pack-types.md#patch).

## Examples

```sh
sandwichcake pack patch ./nginx-conf --root /etc/nginx \
  --base docker.io/library/nginx@sha256:… \
  --to registry.example.com/patches/nginx-conf:1
```
