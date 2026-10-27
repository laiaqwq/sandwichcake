---
title: Quickstart
weight: 2
---

Build an inference image from a Python base image, a virtualenv slice
and a model-weight slice. This walkthrough uses explicit refs and needs
no project; [step 6](#6-turn-it-into-a-project) shows the project
setup for repeated use.

Before you start, [install](installation.md) sandwichcake and
`docker login` to your registry. Replace `registry.example.com/ai` with
a repository prefix you can push to.

## 1. Pack directories into slices

```sh
sandwichcake pack venv ./venv \
  --root /opt/venv \
  --to registry.example.com/ai/venv:torch-2.8

sandwichcake pack weight ./models/qwen3-8b \
  --root /models/qwen3-8b \
  --to registry.example.com/ai/weights:qwen3-8b \
  --source hf://Qwen/Qwen3-8B@main
```

`--root` is the absolute path the directory contents take inside the
composed image. Everything else (compression, layer splitting,
ownership) has a per-type default — see [Pack Types](../general/concepts/pack-types.md).

`pack weight` can also stream a model directly from Hugging Face or
ModelScope without a local copy:

```sh
sandwichcake pack weight hf://Qwen/Qwen3-8B@main \
  --root /models/qwen3-8b \
  --to registry.example.com/ai/weights:qwen3-8b
```

See [Remote Models](../general/concepts/remote-models.md).

## 2. Write a cake.yaml

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

All fields: [cake.yaml reference](../general/reference/cake-yaml.md).

## 3. Preview the transfer plan

```sh
sandwichcake plan -f cake.yaml
```

Shows per-blob classification — which layers are already in the target
repo (`=` reuse), which can be cross-repo mounted (`~` mount), and
which must be uploaded (`+` copy) — before anything is pushed. See
[Transfer Plan](../general/concepts/transfer-plan.md).

## 4. Compose and push

```sh
sandwichcake compose -f cake.yaml
```

The composed image or index is pushed to `target.ref`. When blobs must
be copied, a byte-count prompt asks to confirm (pass `--yes` in CI).
A `cake.lock` with pinned digests is written next to `cake.yaml`;
replay it bit-identically with `compose --locked`. See
[Determinism & Locks](../general/concepts/determinism-and-locks.md).

## 5. Inspect the result

```sh
sandwichcake inspect registry.example.com/ai/infer:v1
sandwichcake diff registry.example.com/ai/infer:v1 registry.example.com/ai/infer:v2
```

## 6. Turn it into a project

For repeated packs and several images, create a project. It stores
output settings — registry prefix, per-type repos, pack roots — in
`cake-config.yaml`, so commands get shorter:

```sh
sandwichcake init demo
cd demo
```

Edit `demo/cake-config.yaml`:

```yaml
apiVersion: sandwichcake.io/v1
kind: CakeConfig
registry:
  prefix: registry.example.com/ai
  repos: {venv: venv, weight: weights, image: infer}
pack:
  venv: {root: /opt/venv}
models:
  paths: {hf: "/models/{org}/{name}"}
```

Now targets and roots are derived automatically:

```sh
sandwichcake pack weight hf://Qwen/Qwen3-8B@main
# -> registry.example.com/ai/weights:Qwen--Qwen3-8B, root /models/Qwen/Qwen3-8B
```

Cakes live in `cake/<name>.yaml` and may use short refs:

```yaml
# cake/serve.yaml
apiVersion: sandwichcake.io/v1
kind: Cake
target: {ref: "image:serve"}
base: {ref: "python:3.12-slim"}
slices:
  - {ref: "venv:torch-2.8"}
  - {ref: "weight:Qwen--Qwen3-8B"}
```

```sh
sandwichcake compose serve
```

Machine-local settings (cache dir, upload concurrency) go in the
optional user config created by `sandwichcake config init --global`.
See [Projects](../general/concepts/projects.md) and
[Configuration Files](../general/reference/cake-config.md).

## Next steps

- [Concepts](../general/concepts/_index.md) — how slices, types and
  composition fit together.
- [CLI Reference](../cli/_index.md) — every command and flag.
