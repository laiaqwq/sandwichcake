---
title: diff
weight: 5
---

`sandwichcake diff <ref-a> <ref-b>` compares two images' layers by
**diff_id** (uncompressed layer digest) and prints a line-oriented
comparison. Read-only.

## What it shows

Each layer of both refs is matched by diff_id, so compression changes
(gzip → zstd) or different tar layouts for identical content still
compare equal:

| Marker | Meaning |
|--------|---------|
| `=` | Layer present in both refs — `shared` |
| `-` | Layer only in `<ref-a>` |
| `+` | Layer only in `<ref-b>` |

```text
$ sandwichcake diff reg/ai/infer:v1 reg/ai/infer:v2
= sha256:9be4…           28.4MiB shared
- sha256:51cd…          410.0MiB only in reg/ai/infer:v1
+ sha256:8fa0…          412.3MiB only in reg/ai/infer:v2
= sha256:77e2…            4.9GiB shared
```

## Examples

Check what changed between releases:

```sh
sandwichcake diff registry.example.com/app:v1 :v2
```

Verify a `--locked` rerun produced the same layers:

```sh
sandwichcake diff app:rebuilt registry.example.com/app:v1
```

## Notes

- Both indexes and single manifests work; index children are flattened
  before comparison.
- Output order is not sorted — pipe to `sort` for stable diffs.
