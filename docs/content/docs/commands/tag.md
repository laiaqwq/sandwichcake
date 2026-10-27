---
title: tag
weight: 6
---

`sandwichcake tag <ref> <new-ref>` copies a manifest to another tag —
the fast way to promote, rename or mirror an image or slice.

## What happens

Two paths depending on the destination:

- **Same repository** — a manifest GET + PUT under the new tag (there
  is no dedicated tag-copy API). No blobs are read or transferred at
  all; this is effectively free and instant.
- **Different repository** — the manifest (or index) is re-pushed to
  the destination repo. Blobs already present there are **skipped**
  after a HEAD check; the rest are cross-repo **mounted** when source
  and destination share a registry — a cross-registry destination
  always uploads — with a chunked upload as mount fallback.

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
- Same-repo destinations must be tags (`name:tag`); the cross-repo path
  also accepts a digest destination (`repo@sha256:…`).
- A cross-repo push of an index writes each child manifest by digest
  before the tagged index itself.
- Cross-repo copy uses the same chunked, resumable upload machinery as
  `pack`/`compose` for blobs that can't be mounted.
