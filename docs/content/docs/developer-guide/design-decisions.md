---
title: Design Decisions
weight: 2
---

Settled choices. Change them only deliberately — several affect digests
or the on-registry format and cannot be undone for already-pushed
content.

## Scope

- **Compose, don't build.** No `RUN`, no daemon, no Dockerfile
  frontend. The output is a standard OCI image.
- **Linux images only.** Windows container images and foreign layers
  are rejected; Windows is at most a client platform.
- **No bundled toolchains.** `pack venv --from-lock` shells out to the
  user's `uv`; git sync shells out to system `git` (no go-git).
- **Slices are images, not artifacts.** Slices and composed images are
  image manifests with standard configs, so any registry stores them
  and any runtime can pull the result. OCI 1.1 artifacts are used only
  for side metadata.
- **Signatures are not carried over** (they bind to the old digest);
  provenance is kept in annotations and users sign the new manifest.

## Format

- **Annotation namespace** is `com.github.baba.sandwichcake.`. All keys
  are constants in `pkg/layer/annotations.go`; type-specific keys use
  `layer.TypeKey(type, key)`. (`DESIGN.md` predates this and says
  `io.sandwichcake.`.)
- **Media types.** Layers use standard OCI types (`tar`, `+gzip`,
  `+zstd`). `application/vnd.sandwichcake.*` is reserved for metadata
  and project artifacts.
- **File formats.** `cake.yaml` and `cake-config.yaml` use
  `apiVersion: sandwichcake.io/v1`; kinds are `Cake`, `CakeLock`,
  `CakeConfig`. Parsing is strict.
- **Platform-independent slices** use `os`/`architecture` `unknown` plus
  `…platform=any`, following BuildKit attestation manifests.

## Determinism

- Identical inputs must produce identical manifest digests, including
  `compose --locked` reruns.
- `SOURCE_DATE_EPOCH` if set, else Unix 0 (`slice.Epoch`).
- The lock stores the original tag ref (`input:`) so
  `org.opencontainers.image.base.name` stays stable, and pins the target
  and platform order.
- Every layer records `tool.version`; digests are only promised stable
  within one version.

## Registry behavior

- **Metadata artifacts** are PUT by digest with `subject`. If the
  registry does not acknowledge it (no `OCI-Subject` response header),
  the OCI 1.1 fallback index at tag `sha256-<subject hex>` is
  maintained via `Client.AddReferrer`/`RemoveReferrer`.
  `--artifact-compat` always uses the fallback index plus a standard
  image config type.
- **Discovery** relies only on `tags/list` (`ls` + configured repos);
  `_catalog`/GAR children are a best-effort extra (`discover`).
- **Blob deletion** is left to registry GC; `rm` only deletes manifests
  and referrers.
- **Weight naming** is repo-per-model (`…/weights/org--model:rev`) when
  `repoFormat` includes `{name}`; this is not portable to Docker Hub or
  Quay, which is why the default `repoFormat` is `{prefix}/{repo}`.

## libc resolution

Base libc comes from `base.libc`, else the `…libc` manifest annotation
or config label, else a `--verify` scan of base layer tars
(`ld-musl-*`, `libc-X.Y.so`, then `libc.so.6` content
"GNU C Library … version X.Y"; later layers win). Unknown is `""` and
produces warnings for unverifiable requirements.

## Configuration

- **Output vs environment fields** are strictly separated: output
  fields only in `cake-config.yaml`, environment fields only in the
  user or local config. The loader enforces it.
- **Credentials never live in config files**; registries use the Docker
  credential chain, model sources use env/token files.
- **Sync safety.** `config pull` never overwrites local edits without
  `--force`; `config push` never merges or force-pushes. The OCI check
  is optimistic, not an atomic compare-and-swap.

## Dependencies

go-containerregistry v0.22.1, klauspost/pgzip, klauspost/compress,
gopkg.in/yaml.v3, spf13/cobra, golang.org/x/term. Prefer the standard
library over new dependencies.
