---
title: config
weight: 14
---

`sandwichcake config` inspects project and user settings and synchronizes
entire projects. Create a project with [`init`](init.md). Project configuration
is `cake-config.yaml`; user defaults are in
`$XDG_CONFIG_HOME/sandwichcake/config.yaml` (or
`~/.config/sandwichcake/config.yaml`).

## Usage

```sh
sandwichcake config init --global
sandwichcake config show --origin
sandwichcake config get <key>
sandwichcake config set <key> <value>
sandwichcake config unset <key>
sandwichcake config pull [oci://registry/repo:tag | git+https://host/repo.git@main#subdir]
sandwichcake config push [destination] [--new-branch branch]
```

| Key for `get` / `set` / `unset` | YAML field | Effect |
|-------------------------------|------------|--------|
| `cache-dir` | `cache.dir` | Local metadata cache directory (never stores layer blobs) |
| `upload-concurrency` | `upload.concurrency` | Concurrent registry uploads |
| `upload-retries` | `upload.retries` | Transient upload retries |
| `repos` | `repos` | Default repository list for `ls` without arguments |
| `download-concurrency` | `download.concurrency` | Remote model layers streamed in parallel (default 2) |
| `download-connections` | `download.connections` | Parallel ranged requests per file (default 4) |
| `download-segment-size` | `download.segmentSize` | Bytes per ranged request (default `32Mi`) |
| `download-retries` | `download.retries` | Download retries per request (default 5) |
| `hf-endpoint` | `download.hf.endpoint` | Hugging Face endpoint or mirror (default `https://huggingface.co`) |
| `modelscope-endpoint` | `download.modelscope.endpoint` | ModelScope endpoint (default `https://www.modelscope.cn`) |

`config set repos` takes comma-separated repository names; the YAML field
is a list. `config unset` clears a value. Project output settings
(`registry`, `pack`, `models.paths`) are not accepted in the global or local
config. Migrate existing `toPrefix`, `tagFormat`, `roots` and `cacheDir`
with `sandwichcake init <dir> --migrate`, or move them by hand into
`registry.prefix`, `registry.tagFormat`, `pack.<type>.root`, and `cache.dir`.

```sh
sandwichcake init my-project
sandwichcake config init --global
sandwichcake config set cache-dir /var/tmp/sandwichcake-cache
sandwichcake config set repos registry.example.com/ai/venv,registry.example.com/ai/weights
sandwichcake --project my-project config show --origin
```

The equivalent file (which you may edit manually) is:

```yaml
repos:
  - registry.example.com/ai/venv
  - registry.example.com/ai/weights
cache:
  dir: /var/tmp/sandwichcake-cache
upload:
  concurrency: 4
  retries: 5
download:
  concurrency: 2
  connections: 4
  segmentSize: 32Mi
  hf: {endpoint: https://hf-mirror.com}
```

## Project configuration

The project root is discovered upwards from the working directory, or
specified by `--project <dir>`. A project supports several cakes:
`cake/serve.yaml`, `cake/train.yaml`, with adjacent `serve.lock` and
`train.lock`. `compose serve` and `plan serve` select by name; `-f path`
still works. A standalone `cake.yaml` still writes `cake.lock`.

```yaml
apiVersion: sandwichcake.io/v1
kind: CakeConfig
from: oci://registry.example.com/ai/projects:demo
registry:
  prefix: registry.example.com/ai
  repos: {venv: venv, weight: weights, image: infer}
  repoFormat: "{prefix}/{repo}"
  tagFormat: {default: "{name}", weight: "{name}-{rev}"}
  onTagExists: fail # fail | overwrite | skip
pack:
  defaults: {compression: zstd, level: 3}
  weight:
    root: /models
    compression: none
    split: {enabled: true, large: 5Gi, small: 64Mi}
    include: ["*.safetensors", "*.json", "tokenizer*"] # remote sources only
    exclude: ["original/**"]
models:
  paths: {hf: "/models/{org}/{name}", modelscope: "/models/{org}/{name}"}
```

`pack` auto-names as `<prefix>/<repo>:<name>` when `--to` is absent.
For HF sources, `{name}` is `org--model` (to avoid collisions) and `{rev}`
is the source revision. `{name}` is lowercased inside the repository path
(OCI repository names are lowercase); characters a tag cannot hold (e.g.
the `/` of `refs/pr/1`) become `-`. `--to-prefix` explicitly overrides the
project prefix and uses the legacy type naming; the global
`--registry-prefix` replaces only `registry.prefix` and keeps
`repos`/`repoFormat`/`tagFormat`. `repoFormat` accepts `{prefix}`,
`{repo}`, `{name}`, `{type}`; `tagFormat` accepts `{name}`, `{type}`, `{rev}`.
`models.paths` supplies a default container root for `pack weight` when
the source is `hf://org/name@rev` or `modelscope://org/name@rev` (passed
as `--source` or as the remote argument) and no root is configured.
`pack.weight.include` / `exclude` filter remote model files (see
[`pack`](pack.md#remote-models)). `onTagExists` applies before
pack/compose pushes (default: existing overwrite behavior); `--merge`
and `--locked` bypass this check.

`cake/<name>.yaml` keeps the same Cake format, but may use short references:

```yaml
target: {ref: "image:serve"}
base: {ref: "python:3.12-slim"}
slices:
  - {ref: "venv:torch-2.8"}
  - {ref: "weight:qwen"}
```

Short refs expand to full registry refs before resolution. Existing full
refs, including Docker Hub names like `python:3.12-slim`, remain valid;
the lock records the expanded refs. `--locked` uses the pinned target and
digests even if the project prefix changes later.

## Local settings and precedence

`cake-config.local.yaml` (gitignored) may contain only `cache`,
`upload`, `download`, and the `repos` list used by `ls`. Relative
`cache.dir` is resolved against the project directory. Precedence: CLI
flags > `SANDWICHCAKE_CACHE_DIR` / `SANDWICHCAKE_UPLOAD_CONCURRENCY` /
`SANDWICHCAKE_UPLOAD_RETRIES` / `SANDWICHCAKE_DOWNLOAD_CONCURRENCY` /
`SANDWICHCAKE_DOWNLOAD_RETRIES` / `HF_ENDPOINT` / `MODELSCOPE_DOMAIN` >
local config > user config > built-ins. For project output fields the
global `--registry-prefix` flag overrides `registry.prefix`.
`config show --origin` reports the source of configured values. Registry
credentials use the Docker credential chain; model tokens come from
`HF_TOKEN` (or the `huggingface-cli login` token file),
`MODELSCOPE_API_TOKEN` and `~/.netrc` — never from these files.

## Pull and push

Both commands sync the entire project, including all `cake/*.yaml` and
`cake/*.lock`. They exclude `.git/`, `.cake/`, `cake-config.local.yaml`,
`*.env`, and `.cakeignore` patterns; symlinks are refused. `from` supplies
the default remote address. A configured cache directory inside the project
is also excluded. Use `--project <dir>` to pull into a new directory, or
pull into a project made by `init`. Local modifications are
never overwritten unless `config pull --force`; a pull prints changed
file names before writing and records versions/hashes in `.cake/state.json`.

For OCI, use `oci://registry/repo:tag`. The project is stored as an OCI
artifact with per-file layers. Push refuses a changed remote tag unless
`--force` (optimistic check, not a registry-side atomic compare-and-swap).

For git, use `git+https://host/repo.git@branch#subdir` or `git+ssh://...`.
A **nonempty subdirectory** is required; the tool uses a cached clone and
requires system `git`, but the project directory need not be a checkout.
Pull before pushing; `config push` updates the existing branch if its head
has not changed. `config push --new-branch feature/name` instead commits
from the previously pulled revision to a new branch. Neither mode merges
nor force-pushes. Configure your git commit identity beforehand.
