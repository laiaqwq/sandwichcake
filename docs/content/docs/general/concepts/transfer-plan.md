---
title: Transfer Plan
weight: 4
---

Before pushing, `compose` classifies every blob of the composed image
(all layers plus the config) by how it will reach the target
repository. [`plan`](../../cli/plan.md) prints this classification
without pushing; `compose -v` prints it on real runs too.

## Strategies

| Marker | Strategy | When | Client upload |
|--------|----------|------|---------------|
| `=` | **reuse** | The blob already exists in the target repo (`HEAD /v2/<repo>/blobs/<digest>`) | none — only the manifest is written |
| `~` | **mount** | The blob lives in another repo on the same registry | none — server-side cross-repo mount (`POST …/blobs/uploads/?mount=<digest>&from=<repo>`); needs pull access to the source repo |
| `+` | **copy** | The blob is on a different registry (or mount is impossible) | full blob, streamed through the client without touching disk |

```text
  = config                                          1.5KiB  reuse
  = debian-slim (base)                             28.4MiB  reuse
  ~ site-packages (venv)                          410.0MiB  mount
  + qwen3-8b (weight)                               4.9GiB  copy

Plan: 28.4MiB reuse (2 layers), 410.0MiB mount (1 layers), 4.9GiB copy (1 layers)
```

Practical consequences:

- Keep slices on the same registry as the images that use them — then
  composing is nearly free regardless of slice size.
- Rerunning an unchanged cake transfers nothing: every blob is `reuse`.
- `mount` is best-effort: if the registry rejects the mount at push
  time, the blob falls back to a chunked upload.

## Confirmation

If any blob classifies `copy`, `compose` prints the byte count on
stderr and asks before transferring; only `y`/`Y` proceeds. In a
non-interactive shell it fails instead and asks for `--yes`, so CI
never hangs on an unanswerable prompt.

## Uploads

All registry I/O goes through one client that:

- uploads layers concurrently (`upload.concurrency`, default 4) and the
  config last;
- uses **chunked uploads** (64 MiB `PATCH` chunks) with retries on
  network errors, `429` and `5xx` (`upload.retries`);
- records upload sessions in the metadata cache, so an interrupted
  `pack`, `compose` or `tag` resumes from the offset the registry
  acknowledged when rerun;
- reuses authenticated transports per registry and scope.

Tune concurrency and retries in the user or local config — see
[Configuration Files](../reference/cake-config.md#environment-config).

## Metadata cache

The cache directory (`cache.dir`, default `~/.cache/sandwichcake`)
holds **metadata only** — digest ↔ diff_id mappings, manifests, tar
header listings, upload sessions and stream hash state. Layer blobs are
never cached on disk. Deleting the cache is always safe; it only costs
extra registry round trips and loses resumable upload state.
