---
title: cake.yaml & cake.lock
weight: 1
aliases:
  - /docs/cake-yaml/
---

`cake.yaml` declares a composition: a base image, a list of slices, and
a config patch. Unknown fields are rejected (strict parsing). How the
pieces are combined is described in
[Composition](../concepts/composition.md).

A standalone file is usually named `cake.yaml` and selected with
`compose -f`; in a [project](../concepts/projects.md) each image has a
`cake/<name>.yaml` selected by name (`compose <name>`). Both use the
same format.

```yaml
apiVersion: sandwichcake.io/v1   # required, only this value
kind: Cake                       # required

target:
  ref: registry.example.com/app:v1        # required
  platforms: [linux/amd64]                # optional; default: the base's platforms
  annotations: {}                         # optional extra manifest annotations

base:                            # required; a normal image ref
  ref: python:3.12-slim
  platform: linux/amd64          # optional: pick from an index, or "any"
  verify: true                   # optional: download + scan base layers
  libc: "glibc 2.35"             # optional: declared base libc (see below)
  name: debian-slim              # optional informational label

slices:                          # optional list, applied in order
  - ref: registry.example.com/ai/venv:torch-2.8   # packed slice or plain image
    platform: linux/amd64        # optional: "linux/amd64" or "any"
    layers: [0, "sha256:…"]      # index or digest; required for a plain image
    verify: true                 # optional: verify layers at resolve
    force: true                  # optional: skip the pure-file check on plain-image layers
    name: torch                  # optional informational label

config:                          # optional OCI config patch
  env:
    KEY: value                   # set env var
    PATH: {prepend: /opt/venv/bin}   # or prepend/append, ":"-joined
  entrypoint: ["python", "serve.py"]   # [] clears the base value
  cmd: ["--port", "8080"]            # [] likewise
  workdir: /app
  user: "1000"
  labels:
    org.opencontainers.image.title: "my app"
```

## Field rules

- `target.ref`, `base.ref` and each `slices[].ref` are required. A ref may
  also use `<type>:<tag>` shorthand (`venv:torch-2.8`, `image:serve`),
  expanded via `registry.prefix`/`repos` — see [short refs](../concepts/projects.md#short-refs);
  ordinary refs like `python:3.12-slim` pass through unchanged.
- `target.platforms` defaults to the base's platforms (index entries,
  else the base config's os/arch). One platform emits a single manifest,
  more than one an OCI index.
- Managed keys `com.github.baba.sandwichcake.slices`,
  `org.opencontainers.image.base.name` and
  `org.opencontainers.image.base.digest` override `target.annotations`.
- `base` does not allow `layers` or `force` — it is consumed whole.
- `libc` is only valid on `base`, not on slices.
- A `slices[]` ref may name a packed slice or a plain image; on a plain
  image `layers` is required ("is not a slice; select layers explicitly").
  Entries are layer indexes or full `sha256:<hex>` digests.
- `platform` accepts any `os/arch` plus `any` (platform-independent
  manifest).
- `force` skips only the pure-file check when selecting layers from a
  plain image (COPY/ADD history judgment); every other check still runs.
- `verify` downloads each selected layer and scans its tar, rejecting
  `.wh.*` whiteout entries.
- `name` is informational: stored in the lock and restored by `--locked`,
  never displayed or consumed.
- `config.env` values are either a scalar (`KEY: value`, replace/set) or
  a mapping with exactly `prepend` / `append` joined to the base value
  with `:`; unknown keys are rejected and `env: {}` is a no-op.
- `cmd: []` / `entrypoint: []` explicitly clears the base value.
- Duplicate layers are deduped by diff_id (first wins).

## libc {#libc}

`base.libc` accepts `"glibc X.Y"` or `"musl"` and takes precedence over
every other way of determining the base libc. The full resolution order
is described under [libc resolution](../concepts/composition.md#libc).

## cake.lock {#cakelock}

After a successful push — never on `--dry-run`/`plan` — `compose` writes
a lock file (`kind: CakeLock`) next to the cake file: `cake.lock` for
`cake.yaml`, otherwise `<name>.lock` (`compose serve` →
`cake/serve.lock`). `target.ref` keeps the tag, `target.digest` the
pushed manifest/index digest, `target.platforms` the built platforms.
Each base/slice `ref` is pinned to `repo@digest`, with `input` holding
the original tag ref (keeps `org.opencontainers.image.base.name`
stable). Also echoed per ref: `platform`, `layers`, `force`, `verify`,
`name` (`libc` on base) and the `config` patch;
`resolved.<platform-or-"any">` records the selected `manifest` and full
`layers` digest list:

```yaml
apiVersion: sandwichcake.io/v1
kind: CakeLock
target:
  ref: registry.example.com/app:v1   # tag kept
  digest: sha256:…            # final pushed manifest/index digest
  platforms: [linux/amd64]
base:
  ref: python@sha256:…        # resolved repo@digest
  input: python:3.12-slim     # original tag ref
  resolved:
    linux/amd64:              # or "any"
      manifest: sha256:…
      layers: [sha256:…, …]
slices:
  - ref: registry.example.com/ai/venv@sha256:…
    input: registry.example.com/ai/venv:torch-2.8
    layers: [0]               # original selection echoed
    resolved:
      any:
        manifest: sha256:…
        layers: [sha256:…]
```

`sandwichcake compose --locked` replays the lock without resolving any
tags, reproducing the same manifest digest. Only `kind` is validated
when a lock is loaded; the written `apiVersion` is not checked. Commit
the lock file to make builds fully reproducible; see
[Determinism & Locks](../concepts/determinism-and-locks.md) for what the
lock does and does not pin.
