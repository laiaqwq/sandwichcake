---
title: pack weight
linkTitle: weight
weight: 2
---

Pack model weights from a local directory, or stream a remote model
straight into the registry.

## Synopsis

```text
sandwichcake pack weight <dir> [--source <uri>] [flags]
sandwichcake pack weight <hf://… | modelscope://… | https://…> [--include <glob>]... [--exclude <glob>]... [--keep-dir <dir>] [flags]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `dir` | A local model directory |
| remote source | A model or file URL, see [Remote sources](#remote-sources) |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--source <uri>` | – | Local packs: provenance URI (`hf://org/name@rev`, `https://…`, `s3://…`) recorded as `weight.source`; drives naming and `models.paths`. Derived automatically for remote sources (setting it is an error there) |
| `--source-commit <sha>` | – | Resolved upstream commit, recorded as `weight.source.commit`. Set automatically for remote sources |
| `--include <glob>` | all files | Remote only: pack only repo files matching this gitignore-style glob (repeatable; replaces `pack.weight.include`) |
| `--exclude <glob>` | – | Remote only: skip repo files matching this glob (repeatable; replaces `pack.weight.exclude`) |
| `--keep-dir <dir>` | – | Remote only: also write the verified files to this directory |

Default compression: `none`. Plus the
[common pack flags](_index.md#common-flags) and the
[global flags](../_index.md#global-flags).

## Remote sources {#remote-sources}

| Source | Meaning |
|--------|---------|
| `hf://org/name[@rev]` | Whole Hugging Face model (default rev `main`) |
| `https://huggingface.co/org/name[/tree/<rev>[/<dir>]]` | Same; `/tree/<rev>/<dir>` keeps only that subdirectory (paths stay repo-relative) |
| `https://huggingface.co/org/name/blob/<rev>/<file>` (or `/resolve/`) | One file |
| `modelscope://org/name[@rev]`, `https://modelscope.cn/models/org/name[/files\|/summary][?Revision=<rev>]` | ModelScope model (default rev `master`) |
| `https://modelscope.cn/models/org/name/resolve/<rev>/<file>` | One ModelScope file |
| any other `http(s)://…/file[#sha256=<hex>]` | One file; the optional fragment is verified |

URLs on the configured mirror host (`download.hf.endpoint`, e.g.
`https://hf-mirror.com`) are recognized as Hugging Face URLs. Only
models are supported, not datasets or spaces. Tokens:
[Authentication](../../general/reference/authentication.md#model-sources).

How streaming, verification and resume work:
[Remote Models](../../general/concepts/remote-models.md). With
`--dry-run`, a remote pack prints the per-layer plan (name, bytes,
entry count) without downloading content.

## What it records

`weight.source`, `weight.source.commit`, `weight.format` (dominant
tensor extension) and `weight.files.digest`; metadata files
`files.json` (path, size, sha256 per file) and a root `config.json` or
`model_index.json`. Details:
[Pack Types → weight](../../general/concepts/pack-types.md#weight).

## Output

```text
$ sandwichcake pack weight https://huggingface.co/Qwen/Qwen3-8B/tree/main \
    --exclude 'original/**' --to registry.example.com/ai/weights/qwen--qwen3-8b:main
layer 1/6: streaming model-00001-of-00005.safetensors (4189112320 bytes uncompressed)
…
pushed registry.example.com/ai/weights/qwen--qwen3-8b:main
```

## Examples

Pack a local model directory:

```sh
sandwichcake pack weight ./models/qwen3-8b \
  --root /models/qwen3-8b \
  --to registry.example.com/ai/weights:qwen3-8b \
  --source hf://Qwen/Qwen3-8B@main
```

Stream from Hugging Face in a project (target and root from
`cake-config.yaml`):

```sh
export HF_TOKEN=hf_…            # gated/private models only
sandwichcake pack weight hf://meta-llama/Llama-3.1-8B-Instruct@main \
  --include '*.safetensors' --include '*.json' --include 'tokenizer*'
```

Through a mirror:

```sh
HF_ENDPOINT=https://hf-mirror.com sandwichcake pack weight hf://Qwen/Qwen3-8B
```

Legacy auto-naming without a project (`hf://` only — other sources fall
back to `<prefix>/weight:<name>`):

```sh
sandwichcake pack weight ./models/qwen3-8b \
  --root /models/qwen3-8b \
  --to-prefix registry.example.com/ai/weights \
  --source hf://Qwen/Qwen3-8B@main
```
