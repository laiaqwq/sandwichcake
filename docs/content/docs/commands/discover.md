---
title: discover
weight: 8
---

`sandwichcake discover <prefix>` lists repository names under a registry
path prefix. Read-only — it only enumerates names, never manifests.

## What happens

Two strategies are tried in order:

1. **`_catalog`** — the registry's catalog API (the registry is taken
   as the first path segment of `<prefix>`). Every repo name matching
   the prefix is printed, sorted.
2. **GAR children** — for registries without `_catalog` but with a
   Google Artifact Registry–style children listing.

If neither API is available the command errors with both failures shown.
Whether a registry supports this at all is a server-side choice —
Docker Hub does not, most self-hosted registries and GAR do.

```text
$ sandwichcake discover registry.example.com/ai
ai/venv
ai/weights
ai/infer
```

## Examples

Find where slices live before configuring [`ls`](ls.md):

```sh
sandwichcake discover registry.example.com/
```

Narrow to a namespace:

```sh
sandwichcake discover registry.example.com/ai/
```

## Notes

- Output is repo names (no tags), one per line.
- `discover` + `ls` + `inspect` is the intended exploration flow:
  enumerate repos → list slice tags → look at manifests.
