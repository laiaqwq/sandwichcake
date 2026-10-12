---
title: Getting Started
weight: 2
---

## Install

Build from source (requires Go):

```sh
git clone https://github.com/laiaqwq/sandwichcake
cd sandwichcake
make build          # produces bin/sandwichcake with version stamped in
sudo make install   # installs to $(PREFIX)/bin plus a `swcake` symlink
```

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

`--root` is required: it is the absolute path the directory contents take
inside the composed image. Everything else (compression, layer splitting,
ownership) has a per-type default you can override.

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
sandwichcake diff registry.example.com/ai/infer:v1 :v2
```

## Registry auth

sandwichcake uses the standard Docker credential chain
(`~/.docker/config.json`, credential helpers). For plain-HTTP registries
add `--insecure-registry <host>` (repeatable).
