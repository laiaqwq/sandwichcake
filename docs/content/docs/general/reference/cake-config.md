---
title: Configuration Files
weight: 2
---

sandwichcake reads up to three YAML files. All use strict parsing
(unknown fields are errors) and camelCase field names. Why the fields
are split this way, precedence and naming rules are explained in
[Projects](../concepts/projects.md).

| File | Location | Holds | Synced by `config push` |
|------|----------|-------|-------------------------|
| Project config | `<project>/cake-config.yaml` | Output fields | yes |
| Local config | `<project>/cake-config.local.yaml` | Environment fields | no (gitignored) |
| User config | `$XDG_CONFIG_HOME/sandwichcake/config.yaml`, else `~/.config/sandwichcake/config.yaml` | Environment fields | no |

Create them with [`init`](../../cli/init.md) (project) and
[`config init --global`](../../cli/config.md#config-init) (user). The
local file is created by hand. Inspect the merged result with
[`config show --origin`](../../cli/config.md#config-show).

## cake-config.yaml {#project-config}

```yaml
apiVersion: sandwichcake.io/v1
kind: CakeConfig
from: oci://registry.example.com/ai/projects:demo
registry:
  prefix: registry.example.com/ai
  repos: {venv: venv, weight: weights, binary: bin, image: infer}
  repoFormat: "{prefix}/{repo}"
  tagFormat: {default: "{name}", weight: "{name}-{rev}"}
  onTagExists: fail
pack:
  defaults: {owner: "0:0", chmod: "644:755"}
  venv:   {root: /opt/venv, compression: zstd, level: 3}
  weight:
    root: /models
    compression: none
    split: {enabled: true, large: 5Gi, small: 64Mi}
    include: ["*.safetensors", "*.json", "tokenizer*"]
    exclude: ["original/**"]
models:
  paths: {hf: "/models/{org}/{name}", modelscope: "/models/{org}/{name}"}
```

### Top level

| Field | Required | Description |
|-------|----------|-------------|
| `apiVersion` | yes | Must be `sandwichcake.io/v1` |
| `kind` | yes | Must be `CakeConfig` |
| `from` | no | Default remote for `config pull`/`push` (`oci://…` or `git+https://…`, `git+ssh://…`) |
| `registry` | no | Naming rules, below |
| `pack` | no | Pack defaults, below |
| `models` | no | Model container paths, below |

### registry

| Field | Description |
|-------|-------------|
| `prefix` | Registry/namespace prefix, e.g. `registry.example.com/ai`. Overridable per run with `--registry-prefix` |
| `repos` | Map of `venv`/`weight`/`binary`/`patch`/`other`/`image` → repository name under the prefix. `image` is the compose output. Values must not contain `:`, `@`, `..` or start with `/` |
| `repoFormat` | Repository template; variables `{prefix}`, `{repo}`, `{name}`, `{type}`. Default `{prefix}/{repo}` |
| `tagFormat` | Map of `default`/`image`/`<type>` → tag template; variables `{name}`, `{type}`, `{rev}`. Default `{name}` |
| `onTagExists` | `fail` \| `overwrite` \| `skip` when a push target tag exists. Unset behaves like `overwrite`. `--merge` and `--locked` bypass it |

### pack

`pack.defaults` applies to every type; `pack.<type>` (`venv`, `weight`,
`binary`, `patch`, `other`) overrides it field by field. Command-line
flags override both.

| Field | Flag equivalent | Description |
|-------|-----------------|-------------|
| `root` | `--root` | Container root path (absolute) |
| `compression` | `--compression` | `gzip` \| `zstd` \| `none` |
| `level` | `--level` | Compression level |
| `split.enabled` | `--no-split` (inverse) | Automatic layer splitting |
| `split.large` | `--large-threshold` | Large-file threshold, e.g. `5Gi` |
| `split.small` | `--small-threshold` | Small-file threshold, e.g. `64Mi` |
| `owner` | `--owner` | `uid:gid` for layer files |
| `chmod` | `--chmod` | `fileMode:dirMode` octal, e.g. `644:755` |
| `include` | `--include` | `pack.weight` only: globs selecting remote model files |
| `exclude` | `--exclude` | `pack.weight` only: globs skipping remote model files |

### models

| Field | Description |
|-------|-------------|
| `paths.hf`, `paths.modelscope` | Default weight root for `hf://` / `modelscope://` sources when no `root` is configured; variables `{org}`, `{name}`, `{rev}`. The result must be absolute |

## Environment config {#environment-config}

The user config and `cake-config.local.yaml` share one schema. The
local file wins per field; lists (`repos`) replace rather than merge.
Output fields (`registry`, `pack`, `models`, and the legacy `toPrefix`,
`tagFormat`, `roots`) are rejected here — move legacy keys with
[`init --migrate`](../../cli/init.md#migrating-legacy-settings).

```yaml
repos:
  - registry.example.com/ai/venv
  - registry.example.com/ai/weights
cache:
  dir: ~/.cache/sandwichcake
upload:
  concurrency: 4
  retries: 5
download:
  concurrency: 2
  connections: 4
  segmentSize: 32Mi
  retries: 5
  hf: {endpoint: https://hf-mirror.com}
  modelscope: {endpoint: https://www.modelscope.cn}
```

| Field | `config set` key | Env override | Default | Description |
|-------|------------------|--------------|---------|-------------|
| `repos` | `repos` | – | – | Repositories `ls` lists without arguments |
| `cache.dir` | `cache-dir` | `SANDWICHCAKE_CACHE_DIR` | `$XDG_CACHE_HOME/sandwichcake`, else `~/.cache/sandwichcake` | Metadata cache; relative paths resolve against the config file's directory, `~` expands |
| `upload.concurrency` | `upload-concurrency` | `SANDWICHCAKE_UPLOAD_CONCURRENCY` | 4 | Concurrent blob uploads per image |
| `upload.retries` | `upload-retries` | `SANDWICHCAKE_UPLOAD_RETRIES` | 3 | Retries per upload request |
| `download.concurrency` | `download-concurrency` | `SANDWICHCAKE_DOWNLOAD_CONCURRENCY` | 2 | Remote model layers streamed in parallel |
| `download.connections` | `download-connections` | – | 4 | Parallel ranged requests per file |
| `download.segmentSize` | `download-segment-size` | – | `32Mi` | Bytes per ranged request |
| `download.retries` | `download-retries` | `SANDWICHCAKE_DOWNLOAD_RETRIES` | 5 | Retries per download request |
| `download.hf.endpoint` | `hf-endpoint` | `HF_ENDPOINT` | `https://huggingface.co` | Hugging Face endpoint or mirror |
| `download.modelscope.endpoint` | `modelscope-endpoint` | `MODELSCOPE_DOMAIN` | `https://www.modelscope.cn` | ModelScope endpoint (`https://` is prepended to a bare domain) |

Precedence: flag (`--cache-dir`) > environment variable > local config
> user config > default.

## .cakeignore

Extra exclusions for `config push`/`pull`, in full gitignore syntax:
`*`, `?`, `**`, `[]`, leading-`/` anchors, trailing-`/` directory
rules, ordered `!` negations and `#` comments. The built-in exclusions
(`cake-config.local.yaml`, `.cake/`, `.git/`, `*.env`, a cache
directory inside the project) cannot be negated.

## .cake/state.json

Written by `config pull`/`push`: the remote address, the pulled digest
or commit and a sha256 per synced file. It is what lets `pull` detect
local modifications and `push` detect remote changes. Not synced; do
not edit by hand.
