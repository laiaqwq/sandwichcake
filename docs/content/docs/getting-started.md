---
title: Getting Started
weight: 2
---

## Install

Prebuilt binaries for macOS (Apple Silicon) and Linux (ARM64, x86_64)
are on the
[GitHub releases page](https://github.com/laiaqwq/sandwichcake/releases) —
see [Releases](releases.md) for checksums and install steps.

Build from source (requires Go matching `go.mod` — currently 1.27.2):

```sh
git clone https://github.com/laiaqwq/sandwichcake
cd sandwichcake
make build                              # produces bin/sandwichcake with version stamped in
make install PREFIX="$HOME/.local"      # installs to $HOME/.local/bin plus a `swcake` symlink
```

The rootless `make install` avoids recompiling as root; make sure
`$HOME/.local/bin` is on your `PATH`. For a system-wide install use
`sudo make install` (which builds and installs as root).

Or without the Makefile:

```sh
go build ./... && go install ./cmd/sandwichcake
```

## Quickstart

### 1. Pack a directory into a slice

```sh
sandwichcake pack weight ./models/qwen3-8b \
  --root /models/qwen3-8b \
  --to registry.example.com/ai/weights:qwen3-8b \
  --source hf://Qwen/Qwen3-8B@main
```

`--root` is required unless `pack.<type>.root` is set in the project's
`cake-config.yaml`: it is the absolute path the directory
contents take inside the composed image. Everything else (compression,
layer splitting, ownership) has a per-type default you can override.

### 2. Write a cake.yaml

```yaml
apiVersion: sandwichcake.io/v1
kind: Cake
target:
  ref: registry.example.com/ai/infer:v1
base:
  ref: python:3.12-slim
slices:
  - ref: registry.example.com/ai/venv:torch-2.8
  - ref: registry.example.com/ai/weights:qwen3-8b
config:
  env:
    PATH:
      prepend: /opt/venv/bin
  entrypoint: ["python", "serve.py"]
```

### 3. Preview the transfer plan

```sh
sandwichcake plan -f cake.yaml
```

Shows per-blob classification — which layers are already in the target
repo, which can be cross-repo mounted, and which must be copied — before
anything is pushed.

### 4. Compose and push

```sh
sandwichcake compose -f cake.yaml
```

Confirm the plan and the composed manifest is pushed to `target.ref`,
along with metadata artifacts for each slice. A `cake.lock` with pinned
digests is written next to `cake.yaml`; replay it bit-identically with
`compose --locked`.

### 5. Inspect the result

```sh
sandwichcake inspect registry.example.com/ai/infer:v1
sandwichcake diff registry.example.com/ai/infer:v1 registry.example.com/ai/infer:v2
```

## Save defaults for repeated packs

For repeated commands, use [`init`](commands/init.md) to create a project.
Put output-affecting settings in `cake-config.yaml` and local cache
preferences in the optional global config:

```sh
sandwichcake init demo
cd demo
# Edit cake-config.yaml: set registry.prefix and pack.weight.root
sandwichcake config init --global
sandwichcake config set cache-dir /var/tmp/sandwichcake-cache
sandwichcake pack weight ./models/qwen3-8b --source hf://Qwen/Qwen3-8B@main
```

With `registry.prefix: registry.example.com/ai` and
`registry.repos.weight: weights`, this targets
`registry.example.com/ai/weights:Qwen--Qwen3-8B`.
The project may have multiple `cake/<name>.yaml` files. Use
`sandwichcake compose <name>` to build one; see [config](commands/config.md)
for short refs and OCI/git project synchronization. You can still use
standalone `cake.yaml` and explicit flags without a project.

## Registry auth

sandwichcake uses the standard Docker credential chain
(`~/.docker/config.json`, credential helpers). For plain-HTTP registries
add `--insecure-registry <host>` (repeatable).
