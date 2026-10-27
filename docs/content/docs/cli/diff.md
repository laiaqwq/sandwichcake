---
title: diff
weight: 7
aliases:
  - /docs/commands/diff/
---

Compare two images' layers by **diff_id** (uncompressed layer digest).
Read-only.

## Synopsis

```text
sandwichcake diff <ref-a> <ref-b>
```

## Arguments

| Argument | Description |
|----------|-------------|
| `ref-a`, `ref-b` | Images or indexes (tags or digests). Index children are flattened before comparison |

## Flags

No command-specific flags; see the [global flags](_index.md#global-flags).

## Output

Layers are matched by diff_id, so a compression change (gzip → zstd)
with the same tar content still compares equal. A different tar layout
— changed paths, ordering or metadata — produces a different diff_id
and shows as removed/added even when file contents look the same.

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
size is the compressed blob size in bytes. `=`/`-` lines show
`<ref-a>`'s digest and size, even when `<ref-b>` stores the same
diff_id compressed differently.

## Examples

Check what changed between releases:

```sh
sandwichcake diff registry.example.com/app:v1 registry.example.com/app:v2
```

Verify a `--locked` rerun produced the same layers:

```sh
sandwichcake diff registry.example.com/app:rebuilt registry.example.com/app:v1
```

## Notes

- All `=`/`-` lines print first, then every `+` line; order within each
  group is unsorted — pipe to `sort` for stable diffs.
- Exit status is 0 whenever the diff runs, even when layers differ —
  gate scripts on the output, not `$?`.
- Layers sharing a diff_id within one ref collapse to a single line.
