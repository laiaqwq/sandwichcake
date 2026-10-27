---
title: rm
weight: 11
aliases:
  - /docs/commands/rm/
---

Delete a manifest/tag **and** the metadata artifacts attached to it.

{{< callout type="warning" >}}
This is destructive and not recoverable unless you still have the
content elsewhere. There is no confirmation prompt.
{{< /callout >}}

## Synopsis

```text
sandwichcake rm <ref>
```

## Arguments

| Argument | Description |
|----------|-------------|
| `ref` | Tag or `repo@sha256:…`. Deleting by digest removes the manifest regardless of which tags point at it |

## Flags

No command-specific flags; see the [global flags](_index.md#global-flags).

## What happens

1. The ref's manifest digest is resolved (HEAD).
2. **All** referrers of the subject are deleted — unfiltered by
   artifact type, so referrers written by other tools (cosign
   signatures, SBOMs) are removed too. The fallback `sha256-<subject>`
   index is updated so no dangling entry remains.
3. The manifest/tag itself is deleted.
4. The deleted referrers and ref are printed, followed by layers that
   are now **possibly unreferenced**. sandwichcake never deletes blobs
   (the registry API cannot); garbage collection is the registry's job.

## Output

```text
$ sandwichcake rm registry.example.com/ai/weights:qwen3-8b-rc1
deleted referrer sha256:77e2…
deleted registry.example.com/ai/weights:qwen3-8b-rc1
possibly unreferenced layers (GC is registry-side):
  sha256:9be4…
```

## Examples

```sh
sandwichcake rm registry.example.com/ai/weights:qwen3-8b-rc1
sandwichcake rm registry.example.com/app@sha256:8de0…
```

## Notes

- `rm` on an index deletes only the index manifest; child manifests and
  layers remain until the registry's GC removes them. Output is just
  `deleted <ref>` — no unreferenced-layer list for indexes.
- Referrer cleanup is best-effort: a failed referrers listing or delete
  is skipped, and if the deleted manifest was itself a referrer of
  another subject, that subject's fallback index is not updated.
- Whether deleting a tag also deletes the manifest depends on the
  registry; most delete it when its last tag goes. Docker Hub does not
  expose manifest deletion.
