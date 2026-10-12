---
title: cake.yaml Reference
weight: 5
---

`cake.yaml` declares a composition: a base image, a list of slices, and
a config patch. Unknown fields are rejected (strict parsing).

```yaml
apiVersion: sandwichcake.io/v1   # required, only this value
kind: Cake                       # required

target:
  ref: registry.example.com/app:v1        # required
  platforms: [linux/amd64]                # optional
  annotations: {}                         # optional extra manifest annotations

base:                            # required; a normal image ref
  ref: python:3.12-slim
  platform: linux/amd64          # optional: pick from an index
  verify: true                   # optional: download + scan base layers
  libc: "glibc 2.35"             # optional: declared base libc (see below)
  name: debian-slim              # optional display name

slices:                          # optional list, applied in order
  - ref: registry.example.com/ai/venv:torch-2.8
    platform: linux/amd64        # optional: "linux/amd64" or "any"
    layers: [0, "sha256:…"]      # optional: subset by index or digest
    verify: true                 # optional: verify layers at resolve
    force: true                  # optional: skip compose-time checks
    name: torch                  # optional display name

config:                          # optional OCI config patch
  env:
    KEY: value                   # set env var
    PATH: {prepend: /opt/venv/bin}   # or prepend/append to base value
  entrypoint: ["python", "serve.py"]
  cmd: ["--port", "8080"]
  workdir: /app
  user: "1000"
  labels:
    org.opencontainers.image.title: "my app"
```

## Field rules

- `target.ref`, `base.ref` and each `slices[].ref` are required.
- `base` does not allow `layers` or `force` — it is consumed whole.
- `libc` is only valid on `base`, not on slices.
- `config.env` values are either a scalar (`KEY: value`, replace/set) or
  a mapping with exactly `prepend` / `append` applied to the base env.

## libc resolution {#libc}

Slices packed by `binary`/`venv` may declare a `glibc>=X.Y` requirement.
`compose` resolves the base libc in this order:

1. `base.libc` in cake.yaml — an explicit declaration wins.
2. The `com.github.baba.sandwichcake.libc` manifest annotation or config
   label on the base.
3. `--verify` (or `verify: true`): base layer tars are downloaded and
   scanned for `ld-musl-*`, `libc-X.Y.so`, then `libc.so.6` contents
   ("GNU C Library … version X.Y"); later layers win.
4. Otherwise libc is unknown — `glibc` requirements produce warnings;
   a requirement against musl is an error.

## cake.lock {#cakelock}

`compose` writes `cake.lock` (`kind: CakeLock`) next to `cake.yaml`.
Every ref is pinned to the digest it resolved to, with the selected
manifest and full layer digest list per platform:

```yaml
apiVersion: sandwichcake.io/v1
kind: CakeLock
target:
  ref: registry.example.com/app:v1
  digest: sha256:…            # final pushed manifest/index digest
base:
  ref: python@sha256:…        # resolved
  input: python:3.12-slim     # original tag, keeps base.name stable
  resolved:
    linux/amd64:
      manifest: sha256:…
      layers: [sha256:…, …]
```

`sandwichcake compose --locked` replays the lock without resolving any
tags, reproducing the same manifest digest. Commit `cake.lock` to make
builds fully reproducible.
