---
title: Compose ModelScope models onto a vLLM base
linkTitle: ModelScope models + vLLM
weight: 1
---

Pack two OCR models from ModelScope as separate weight slices, then
compose them with an existing vLLM image. The model IDs and
`registry.example.com` below are fictional: replace the IDs, base ref
and registry prefix before running this example. `modelscope.cn` is the
actual ModelScope host.

Before starting, [install sandwichcake](../installation.md) and log in
to a registry where you can push images.

## 1. Set up a project

```sh
sandwichcake init ocr-demo
cd ocr-demo
```

Set `cake-config.yaml` to:

```yaml
apiVersion: sandwichcake.io/v1
kind: CakeConfig
registry:
  prefix: registry.example.com/ai
  repos: {weight: weights, image: infer}
  onTagExists: fail
pack:
  weight:
    include: ["*.safetensors"]
```

The project derives weight tags from ModelScope model IDs and refuses
to overwrite existing tags. The weights-only `include` default above
is overridden in the next step to pack model configs and tokenizers too.

## 2. Pack both models

Preview the remote file lists and layer plans without downloading
weights or pushing anything:

```sh
sandwichcake pack weight https://modelscope.cn/models/ExampleOrg/OCR-Base \
  --root /models/ExampleOrg/OCR-Base --include '**' --dry-run
sandwichcake pack weight https://modelscope.cn/models/ExampleOrg/OCR-Adapter \
  --root /models/ExampleOrg/OCR-Adapter --include '**' --dry-run
```

Then stream each complete model directly into the registry:

```sh
sandwichcake pack weight https://modelscope.cn/models/ExampleOrg/OCR-Base \
  --root /models/ExampleOrg/OCR-Base --include '**'
sandwichcake pack weight https://modelscope.cn/models/ExampleOrg/OCR-Adapter \
  --root /models/ExampleOrg/OCR-Adapter --include '**'
```

`--include '**'` replaces `pack.weight.include`. Each `--root` is the
model's path inside the final image, not a local download directory.
The project names the resulting slices
`registry.example.com/ai/weights:ExampleOrg--OCR-Base` and
`registry.example.com/ai/weights:ExampleOrg--OCR-Adapter`.

## 3. Describe the image

Create `cake/ocr-vllm.yaml`:

```yaml
apiVersion: sandwichcake.io/v1
kind: Cake
target:
  ref: "image:ocr-v1"
base:
  ref: registry.example.com/base/vllm-openai:v1
slices:
  - ref: "weight:ExampleOrg--OCR-Base"
  - ref: "weight:ExampleOrg--OCR-Adapter"
```

The project expands `image:ocr-v1` to
`registry.example.com/ai/infer:ocr-v1` and the two `weight:` refs to
the packed slices. Platforms default to those of the base image.

## 4. Plan, compose and inspect

```sh
sandwichcake plan ocr-vllm
sandwichcake compose ocr-vllm --yes
sandwichcake inspect registry.example.com/ai/infer:ocr-v1
```

`plan` shows which blobs can be mounted from existing repositories and
which require copying. `--yes` skips the copy confirmation (useful in
CI); omit it to confirm interactively. The push writes
`cake/ocr-vllm.lock`, pinning the base and both weight slices.

Composition adds model files but does not start the server or select
which model vLLM loads. Supply the appropriate model path, such as
`/models/ExampleOrg/OCR-Base`, when running the image.

For more on this workflow, see [Remote Models](../../general/concepts/remote-models.md),
[Projects](../../general/concepts/projects.md) and
[Composition](../../general/concepts/composition.md).
