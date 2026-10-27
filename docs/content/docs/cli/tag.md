---
title: tag
weight: 8
aliases:
  - /docs/commands/tag/
---

Copy a manifest (or index) to another tag or repository — the fast way
to promote, rename or mirror an image or slice.

## Synopsis

```text
sandwichcake tag <ref> <new-ref>
```

## Arguments

| Argument | Description |
|----------|-------------|
| `ref` | Source tag or `repo@sha256:…` |
| `new-ref` | Destination. In the same repository it must be a tag; in another repository a tag or digest |

## Flags

No command-specific flags; see the [global flags](_index.md#global-flags).

## What happens

- **Same repository** — a manifest GET + PUT under the new tag. No
  blobs are read or transferred; effectively instant.
- **Different repository** — the manifest (or index, child manifests
  first) is re-pushed to the destination. Blobs already present are
  skipped after a HEAD check; the rest are cross-repo **mounted** when
  source and destination share a registry, with a chunked, resumable
  upload as fallback. A cross-registry destination always uploads.

Metadata artifacts (referrers) are **not** copied.

## Output

```text
tagged <ref> -> <new-ref>
```

## Examples

Promote a slice to a release tag (same repo, instant):

```sh
$ sandwichcake tag reg/ai/weights:qwen3-8b-rc1 reg/ai/weights:v1
tagged reg/ai/weights:qwen3-8b-rc1 -> reg/ai/weights:v1
```

Mirror a slice into a different repo (mounts layers server-side):

```sh
sandwichcake tag reg/ai/weights:qwen3-8b reg/prod/weights:qwen3-8b
```

Retag by digest for an exact pin:

```sh
sandwichcake tag reg/app@sha256:8de0… reg/app:stable
```
