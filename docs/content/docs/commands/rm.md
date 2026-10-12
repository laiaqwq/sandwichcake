---
title: rm
weight: 9
---

`sandwichcake rm <ref>` deletes a manifest/tag **and** the metadata
artifacts attached to it. **This is destructive** — registry-side, and
not recoverable unless you still have the content locally.

## What happens

1. The ref's manifest digest is resolved (HEAD).
2. Its **referrers** are enumerated and each metadata artifact manifest
   is deleted; the fallback `sha256-<subject>` index is updated so no
   dangling entry remains.
3. The manifest/tag itself is deleted.
4. The deleted referrers and ref are printed, followed by the digests
   of layers that are now **possibly unreferenced** — sandwichcake
   never deletes blobs itself; garbage collection is the registry's job.

```text
$ sandwichcake rm registry.example.com/ai/weights:qwen3-8b-rc1
deleted referrer sha256:77e2…
deleted registry.example.com/ai/weights:qwen3-8b-rc1
possibly unreferenced layers (GC is registry-side):
  sha256:9be4…
```

## Examples

Remove a stale RC slice:

```sh
sandwichcake rm registry.example.com/ai/weights:qwen3-8b-rc1
```

Delete by digest (removes the manifest regardless of tags pointing at
it — be careful):

```sh
sandwichcake rm registry.example.com/app@sha256:8de0…
```

## Notes

- `rm` on an index deletes the index manifest; child manifests keep
  living until the registry's GC removes them (same for layers).
- Whether a tag delete also removes the underlying manifest depends on
  the registry implementation — most delete the manifest when its last
  tag goes. Check your registry's GC policy.
- No confirmation prompt — the command assumes you mean it.
