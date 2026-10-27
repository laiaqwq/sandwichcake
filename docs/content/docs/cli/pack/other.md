---
title: pack other
linkTitle: other
weight: 5
---

Pack any directory tree that does not fit a specialized type.

## Synopsis

```text
sandwichcake pack other <dir> --root <path> [flags]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `dir` | Directory to pack |

## Flags

No type-specific flags. Default compression: `gzip`. See the
[common pack flags](_index.md#common-flags) and the
[global flags](../_index.md#global-flags).

No validation, annotations beyond the generic ones, metadata artifact
or compose-time check.

## Examples

```sh
sandwichcake pack other ./serving-config --root /etc/serving \
  --to registry.example.com/ai/misc:serving-config-3
```
