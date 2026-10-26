---
title: tag
weight: 6
---

`sandwichcake tag <ref> <new-ref>` copies a manifest to another tag —
the fast way to promote, rename or mirror an image or slice.

## What happens

Two paths depending on the destination:

- **Same repository** — a registry-side tag write only. No blobs are
  read or transferred at all; this is effectively free and instant.
- **Different repository** — the manifest (or index) is re-pushed to
  the destination repo. Blobs that exist in the source repo are
  cross-repo **mounted** where the registry supports it; anything that
  can't be mounted is uploaded.

Either way the command prints `tagged <src> -> <dst>` on success. Both
images and multi-arch indexes are handled; digest refs (`repo@sha256:…`)
work as the source.

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

## Notes

- Metadata artifacts (referrers) are **not** copied — `tag` copies only
  the manifest/index itself.
- Cross-repo copy uses the same chunked, resumable upload machinery as
  `pack`/`compose` for blobs that can't be mounted.
