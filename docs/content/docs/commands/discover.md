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
   the prefix is printed, sorted. Matching is a raw string prefix, not
   segment-aware: `reg/ai` also matches `ai2/foo`. Use the
   trailing-slash form (`reg/ai/`) to enforce a path boundary.
2. **GAR children** — a Google Artifact Registry–style `tags/list`
   `child` listing. Raw `child` values are printed as returned: single
   path segments relative to the queried prefix (`discover
   gcr.io/proj/ai` prints `venv`, not `ai/venv`), unsorted, immediate
   children only (not recursive).

The command errors only when `_catalog` fails *and* `tags/list`
returns a non-200 status; both failures are shown. A `tags/list` 200
response with no `child` field prints nothing and exits 0 — Docker
Hub behaves this way, so a registry without real children support may
produce silent empty output rather than an error.

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

- `discover foo` (no `/`) treats `foo` as the `_catalog` registry, then
  falls back to `index.docker.io/library/foo` for the GAR query (ggcr
  default-registry parsing); `discover reg` behaves like `discover
  reg/` and lists everything in the registry.
- One result per line, names only (no tags): repo names under
  `_catalog`, single child segments under GAR children.
- `discover` + `ls` + `inspect` is the intended exploration flow:
  enumerate repos → list slice tags → look at manifests.
