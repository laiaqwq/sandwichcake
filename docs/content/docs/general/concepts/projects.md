---
title: Projects
weight: 6
---

A **project** is a directory that holds everything needed to build a
family of images: registry naming rules, pack defaults and any number
of cakes with their locks. Projects are optional — standalone
`cake.yaml` files and explicit flags keep working — but they make
commands short and builds shareable.

Create one with [`init`](../../cli/init.md); share it with
[`config pull`/`push`](../../cli/config.md#config-pull).

## Layout

```text
<project>/                  # project name = directory name
  cake-config.yaml          # project config (synced)
  cake-config.local.yaml    # machine-local overrides (not synced, gitignored)
  .cakeignore               # extra sync excludes (gitignore syntax)
  cake/<name>.yaml          # one Cake per image
  cake/<name>.lock          # its CakeLock, next to it
  .cake/state.json          # pull/push bookkeeping (not synced)
```

- The project root is found by walking up from the working directory to
  the first `cake-config.yaml`, like git. `--project <dir>` overrides
  discovery.
- `compose <name>` and `plan <name>` resolve to `cake/<name>.yaml`; a
  path argument (containing `/` or ending in `.yaml`) or `-f` still
  works.
- Relative paths inside a config file resolve against that file's
  directory, not the working directory.

## Two kinds of settings {#field-classes}

Every setting belongs to exactly one class, and the loader enforces it:

| Class | Lives in | Examples | Why |
|-------|----------|----------|-----|
| **Output** — affects digests, tags or targets | `cake-config.yaml` only | `registry.*`, `pack.*`, `models.paths` | Everyone building the project must get the same images |
| **Environment** — affects only how this machine works | user config or `cake-config.local.yaml` only | `cache.dir`, `upload.*`, `download.*`, `repos` | Differs per machine; never synced |

Putting an output field in the user or local config is a load error
that tells you to move it to `cake-config.yaml`. Credentials never go
in any config file — see [Authentication](../reference/authentication.md).

### Precedence

- **Environment fields:** flag > `SANDWICHCAKE_*` / `HF_ENDPOINT` /
  `MODELSCOPE_DOMAIN` env > `cake-config.local.yaml` > user config
  (`~/.config/sandwichcake/config.yaml`) > built-in default. Lists (such
  as `repos`) replace rather than merge.
- **Output fields:** explicit flag (`--to`, `--root`, `--compression`,
  `--registry-prefix`, …) > `cake-config.yaml` > built-in default.
  `pack.defaults` and `pack.<type>` merge per field, including `split`.

`sandwichcake config show --origin` prints where each value came from.

All fields: [Configuration Files](../reference/cake-config.md).

## Naming {#naming}

When `pack` or a cake's target has no explicit ref, the project derives
one:

```text
repo = repoFormat  (default "{prefix}/{repo}")
tag  = tagFormat.<type> or tagFormat.default  (default "{name}")
```

| Variable | Value |
|----------|-------|
| `{prefix}` | `registry.prefix` (or `--registry-prefix`) |
| `{repo}` | `registry.repos.<type>` (`image` for compose output) |
| `{type}` | the pack type |
| `{name}` | the slice name: the directory name, or `org--model` for `hf://`/`modelscope://` sources |
| `{rev}` | the source revision (`@rev`); `latest` when there is none |

`{name}` is lowercased inside the repository path (OCI repository names
must be lowercase); tags keep their case, and characters a tag cannot
hold (such as the `/` in `refs/pr/1`) become `-`. There are no
time-based variables — names must be reproducible.

```yaml
registry:
  prefix: registry.example.com/ai
  repos: {weight: weights}
  repoFormat: "{prefix}/{repo}/{name}"
  tagFormat: {weight: "{rev}"}
```

With this config, `pack weight hf://Qwen/Qwen3-8B@main` targets
`registry.example.com/ai/weights/qwen--qwen3-8b:main`.

`registry.onTagExists` (`fail` | `overwrite` | `skip`, default
overwrite) controls what `pack` and `compose` do when the target tag
already exists; `--merge` and `--locked` bypass it.

`--to-prefix` on `pack` is an explicit escape hatch that ignores the
project naming and uses the type's legacy Namer convention.

## Short refs {#short-refs}

Inside project cakes, `ref: <type>:<tag>` expands through
`registry.prefix` + `registry.repos`:

```yaml
target: {ref: "image:serve"}         # -> registry.example.com/ai/infer:serve
base:   {ref: "python:3.12-slim"}    # unknown name: an ordinary Docker Hub ref
slices:
  - {ref: "venv:torch-2.8"}          # -> registry.example.com/ai/venv:torch-2.8
```

A ref is short only when the part before `:` is a known type or
`image` and contains no `/`. The lock stores the fully expanded ref.

## Model container paths

`models.paths.<provider>` supplies the default `--root` for
`pack weight` when the source is `hf://` or `modelscope://` and no
`pack.weight.root` or `--root` is set:

```yaml
models:
  paths: {hf: "/models/{org}/{name}", modelscope: "/models/{org}/{name}"}
```

It does not download anything by itself; it only decides where the
files land in the image.

## Syncing a project {#sync}

`config push` and `config pull` copy the whole project tree to and
from a remote, so a team (or CI) builds from the same configuration and
locks:

- **OCI** — `oci://registry/repo:tag`: one OCI artifact
  (`application/vnd.sandwichcake.project.v1`), one layer per file.
- **git** — `git+https://host/repo.git@branch#subdir` or `git+ssh://…`:
  a subdirectory of a git repository, via the system `git`. The project
  directory itself does not need to be a checkout.

The sync set is the project tree minus `cake-config.local.yaml`,
`.cake/`, `.git/`, any `*.env` path and `.cakeignore` matches; symlinks
and special files are refused. `from:` in `cake-config.yaml` sets the
default remote.

Both directions are conservative:

- **pull** never overwrites locally modified files without `--force`
  and records the source version plus per-file hashes in
  `.cake/state.json`.
- **push** to OCI requires the remote tag to be empty or still at the
  digest you pulled (an optimistic check, not an atomic
  compare-and-swap); push to git requires the branch head to equal the
  commit you pulled, or publishes a new branch with `--new-branch`.
  Nothing ever merges or force-pushes.

Details: [`config pull` / `push`](../../cli/config.md#config-pull).
