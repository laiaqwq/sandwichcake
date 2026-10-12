---
title: inspect
weight: 4
---

`sandwichcake inspect <ref>` fetches a manifest and prints a
human-readable report: what kind of object it is, its annotations,
layers, and attached metadata files. Read-only.

## What it shows

- **Kind** — `slice` (manifest carries the
  `com.github.baba.sandwichcake.kind = slice` annotation), `image`, or
  `index`. For an index, every child manifest is inspected and indented
  underneath with its platform.
- **Annotations** — all manifest annotations, sorted.
- **Layers** — index, digest prefix, size, media type, plus
  sandwichcake annotations decoded inline (`type=`, `name=`, `root=`,
  `part=`, `requires=`, `src=`).
- **Metadata files** — blobs found through the slice's metadata
  artifact referrer, listed as `meta: <title> (<media type>, <size>)`.
- **Platform** — from config, or `any` for platform-agnostic slices.

```text
$ sandwichcake inspect registry.example.com/ai/weights:qwen3-8b
slice  registry.example.com/ai/weights:qwen3-8b  sha256:3a1f…  [any]
  com.github.baba.sandwichcake.kind = slice
  com.github.baba.sandwichcake.platform = any
  [0] sha256:9be4…        4.9GiB  application/vnd.sandwichcake.weight.v1.tar
      type=weight name=qwen3-8b root=/models/qwen3-8b part=1/1 src=hf://Qwen/Qwen3-8B@main
  meta: files.json (application/json, 1284 B)
```

## Flags

| Flag | Description |
|------|-------------|
| `--json` | Emit the full report as JSON (kind, digest, annotations, layers, metadata, children) |
| `--file` | Print the raw content of a metadata file by its `title` |

## Examples

Extract the lock file a venv slice was built from:

```sh
sandwichcake inspect registry.example.com/ai/venv:torch-2.8 --file uv.lock
```

See what a compose actually contains:

```sh
sandwichcake inspect registry.example.com/ai/infer:v1
```

Pipe a report to jq:

```sh
sandwichcake inspect registry.example.com/ai/weights:qwen3-8b --json | jq '.Layers[].Annotations'
```

## Notes

- `<ref>` may be a tag or `repo@sha256:…`.
- `inspect` follows referrers — metadata files listed in the report can
  be fetched with `--file <title>` without knowing the artifact digest.
- For multi-arch indexes, add `--json` and look at `children` for
  per-platform details.
