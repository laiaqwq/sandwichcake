---
title: diff
weight: 5
---

`sandwichcake diff <ref-a> <ref-b>` compares two images' layers by
**diff_id** (uncompressed layer digest) and prints a line-oriented
comparison. Read-only.

## What it shows

Each layer of both refs is matched by diff_id (the digest of the
*uncompressed* tar), so a compression change (gzip → zstd) with the
same tar content still compares equal. A different tar layout — changed
paths, ordering or metadata — produces a different diff_id and shows as
removed/added even when the file contents look the same:

| Marker | Meaning |
|--------|---------|
| `=` | Layer present in both refs — `shared` |
| `-` | Layer only in `<ref-a>` |
| `+` | Layer only in `<ref-b>` |

```text
$ sandwichcake diff reg/ai/infer:v1 reg/ai/infer:v2
= sha256:9be4…           29801832 shared
- sha256:51cd…          429916160 only in reg/ai/infer:v1
= sha256:77e2…          5261334937 shared
+ sha256:8fa0…          432439296 only in reg/ai/infer:v2
```

The digest shown is the compressed layer digest (19 characters) and the
size is the compressed blob size in bytes — the compressed-layer
representation, matched by uncompressed diff_id.

## Examples

Check what changed between releases:

```sh
sandwichcake diff registry.example.com/app:v1 registry.example.com/app:v2
```

Verify a `--locked` rerun produced the same layers:

```sh
sandwichcake diff app:rebuilt registry.example.com/app:v1
```

## Notes

- Both indexes and single manifests work; index children are flattened
  before comparison.
- All `=`/`-` lines print first, then every `+` line as a trailing
  block; order within each group is unsorted — pipe to `sort` for
  stable diffs.
- Exit code is 0 whenever the diff runs, even when layers differ —
  only errors exit non-zero, so gate scripts on the output, not `$?`.
- `=`/`-` lines always show `<ref-a>`'s compressed digest and size,
  even when `<ref-b>` stores the same diff_id compressed differently.
- Layers sharing a diff_id within one ref collapse to a single line
  (rows are map-keyed by diff_id, not counted).
