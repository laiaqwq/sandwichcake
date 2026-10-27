---
title: inspect
weight: 6
aliases:
  - /docs/commands/inspect/
---

Fetch a manifest and print a human-readable report: what kind of object
it is, its annotations, layers and attached metadata files. Read-only.

## Synopsis

```text
sandwichcake inspect <ref> [--json] [--file <title>]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `ref` | A tag or `repo@sha256:…` of a slice, image or index |

## Flags

| Flag | Description |
|------|-------------|
| `--json` | Emit the full report as JSON (ref, kind, digest, platform, annotations, layers, metadata, children) |
| `--file <title>` | Print the raw content of a metadata file by its `org.opencontainers.image.title`; wins over `--json` |

Plus the [global flags](_index.md#global-flags).

## Output

- **Kind** — `slice` (manifest carries
  `com.github.baba.sandwichcake.kind = slice`), `image` or `index`. For
  an index, every child manifest is inspected and indented underneath
  with its platform.
- **Annotations** — all manifest annotations, sorted.
- **Layers** — index, digest prefix, size, media type, plus
  sandwichcake annotations decoded inline (`type=`, `name=`, `root=`,
  `part=`, `requires=`, `src=`). Image layers add an indented
  `CreatedBy` line from the config history.
- **Metadata files** — titled blobs from every referrer manifest (no
  artifactType filter), listed as `meta: <title> (<media type>, <size>)`.
- **Platform** — from the config, or `any` for platform-agnostic
  slices; index children take the index entry's `platform`.

```text
$ sandwichcake inspect registry.example.com/ai/weights:qwen3-8b
slice  registry.example.com/ai/weights:qwen3-8b  sha256:3a1f…  [any]
  com.github.baba.sandwichcake.kind = slice
  com.github.baba.sandwichcake.platform = any
  [0] sha256:9be4…  5261334937  application/vnd.oci.image.layer.v1.tar  type=weight name=qwen3-8b root=/models/qwen3-8b part=1/1
  meta: files.json (application/json, 1284 B)
```

JSON keys are capitalized Go field names (`Ref`, `Kind`, `Digest`,
`Platform`, `Annotations`, `Layers`, `Metadata`, `Children`); layer
rows add `Index`, `DiffID`, `Files`, `Source` and `CreatedBy`.

## Examples

Extract the lock file a venv slice was built from:

```sh
sandwichcake inspect registry.example.com/ai/venv:torch-2.8 --file uv.lock
```

See what a composed image contains:

```sh
sandwichcake inspect registry.example.com/ai/infer:v1
```

Pipe a report to jq:

```sh
sandwichcake inspect registry.example.com/ai/weights:qwen3-8b --json \
  | jq '.Layers[] | {Type, Name, Root, Digest}'
```

## Notes

- `inspect` follows referrers, so `--file <title>` works without
  knowing the metadata artifact digest; it searches index children
  recursively.
- For multi-arch indexes, use `--json` and look at `Children` for
  per-platform details.
- The `tool.version` layer annotation is not displayed; read it from
  the raw manifest (e.g. `crane manifest`).

## See also

- [Annotations & Media Types](../general/reference/annotations.md)
- [Slices → Metadata artifacts](../general/concepts/slices.md#metadata)
