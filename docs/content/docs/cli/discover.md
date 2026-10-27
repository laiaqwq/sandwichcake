---
title: discover
weight: 10
aliases:
  - /docs/commands/discover/
---

List repository names under a registry path prefix. Read-only — it
only enumerates names, never manifests. Requires registry support (see
[Registry Compatibility](../general/reference/registry-compatibility.md)).

## Synopsis

```text
sandwichcake discover <prefix>
```

## Arguments

| Argument | Description |
|----------|-------------|
| `prefix` | `registry[/path…]`. The first segment is the registry. A trailing `/` enforces a path boundary |

## Flags

No command-specific flags; see the [global flags](_index.md#global-flags).

## What happens

Two strategies are tried in order:

1. **`_catalog`** — the registry's catalog API. Every repo name
   matching the prefix is printed, sorted. Matching is a raw string
   prefix, not segment-aware: `reg/ai` also matches `ai2/foo`; use
   `reg/ai/` to enforce a boundary.
2. **GAR children** — a Google Artifact Registry–style `tags/list`
   `child` listing. Values are printed as returned: single path
   segments relative to the prefix (`discover gcr.io/proj/ai` prints
   `venv`, not `ai/venv`), unsorted, immediate children only.

The command errors only when `_catalog` fails *and* `tags/list`
returns a non-200 status; both failures are shown. A 200 response with
no `child` field prints nothing and exits 0 — Docker Hub behaves this
way.

## Output

```text
$ sandwichcake discover registry.example.com/ai
ai/venv
ai/weights
ai/infer
```

## Examples

```sh
sandwichcake discover registry.example.com/       # everything
sandwichcake discover registry.example.com/ai/    # one namespace
```

## Notes

- `discover foo` (no `/`) treats `foo` as the `_catalog` registry, then
  falls back to `index.docker.io/library/foo` for the GAR query;
  `discover reg` behaves like `discover reg/`.
- `discover` → [`ls`](ls.md) → [`inspect`](inspect.md) is the intended
  exploration flow: enumerate repos, list slice tags, look at manifests.
