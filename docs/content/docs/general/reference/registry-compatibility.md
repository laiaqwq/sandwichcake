---
title: Registry Compatibility
weight: 6
---

sandwichcake speaks the standard OCI distribution API. A few features
depend on optional registry capabilities; this page lists how each one
degrades.

| Feature | Requires | Without it |
|---------|----------|------------|
| Cross-repo mount (`~` in the plan) | `POST …/blobs/uploads/?mount=&from=` and pull access to the source repo | Falls back to a chunked upload through the client |
| Resumable uploads | Chunked `PATCH` uploads with upload status queries | Interrupted uploads restart from zero |
| Metadata as OCI 1.1 referrers | Registry acknowledges `subject` (`OCI-Subject` response header) | A fallback index is maintained at tag `sha256-<subject hex>` |
| `--artifact-compat` | – | Forces the fallback index and a standard image config type, for registries that reject the empty config or `subject` |
| `discover` | `_catalog`, or GAR-style `child` in `tags/list` | Errors, or prints nothing (Docker Hub returns no `child`) |
| `ls` | `tags/list` (universally supported) | – |
| `rm` | `DELETE` on manifests | Fails; Docker Hub does not expose it |
| zstd layers | Clients: Docker ≥ 23, containerd ≥ 1.5, CRI-O | Use `compose --compat docker20` to reject zstd, and pack with `--compression gzip` |

## Repository path depth

Project naming can produce nested repository paths such as
`registry/ai/weights/qwen--qwen3-8b` (`repoFormat: "{prefix}/{repo}/{name}"`).
GHCR, GAR, Harbor and ECR accept these; Docker Hub (two segments) and
Quay (`org/repo`) do not. Keep the default `{prefix}/{repo}` there.

## Blob size

Layer splitting caps a layer at the large-file threshold (default
5 GiB) unless a single file is bigger. Some registries enforce soft
per-blob limits around 10 GiB or proxy timeouts on long uploads;
chunked uploads keep each request small, but very large single files
remain one blob.

## Garbage collection

The registry API cannot delete blobs, only manifests. `rm` deletes the
manifest and its referrers and lists layers that may now be
unreferenced; reclaiming space is up to the registry's GC.

## Base images

Docker schema2 base images are accepted and converted to OCI on output.
Windows images and foreign (non-distributable) layers are rejected.
