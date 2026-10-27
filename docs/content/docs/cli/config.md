---
title: config
weight: 2
aliases:
  - /docs/commands/config/
---

Show or change environment settings, and synchronize whole projects.

## Synopsis

```text
sandwichcake config show [--origin]
sandwichcake config init --global
sandwichcake config get <key>
sandwichcake config set <key> <value>
sandwichcake config unset <key>
sandwichcake config pull [source] [--force]
sandwichcake config push [destination] [--force | --new-branch <branch>]
```

All subcommands accept the [global flags](_index.md#global-flags);
`--project <dir>` selects the project.

`show`, `get`, `set`, `unset` and `init --global` deal with
**environment** settings (cache, upload, download, `repos`). Output
settings (`registry`, `pack`, `models`) live only in `cake-config.yaml`
and are edited by hand — see [Projects](../general/concepts/projects.md#field-classes)
and [Configuration Files](../general/reference/cake-config.md).

## config show {#config-show}

Print the effective configuration as YAML: the merged environment
settings and, inside a project, the project config.

| Flag | Description |
|------|-------------|
| `--origin` | Append `# <key>: <source>` comment lines naming where each configured environment value came from (file path or environment variable), plus one line for the project file holding `registry.*`, `pack.*`, `models.*` |

```text
$ sandwichcake --project my-project config show --origin
cache:
  dir: /var/tmp/sandwichcake-cache
…
# cache.dir: SANDWICHCAKE_CACHE_DIR
# registry.*, pack.*, models.*: /abs/my-project/cake-config.yaml
```

## config init {#config-init}

Create the user config file, `$XDG_CONFIG_HOME/sandwichcake/config.yaml`
(or `~/.config/sandwichcake/config.yaml`). Never overwrites an existing
file. Prints `created <path>`.

| Flag | Description |
|------|-------------|
| `--global` | Required. Creates the user-wide file (projects are created with [`init`](init.md)) |

## config get {#config-get}

Print the **effective** value of one key — after applying the local
config and environment variables. Prints an empty line when unset.

```sh
sandwichcake config get cache-dir
```

## config set {#config-set}

Write one key to the **user** config file (never to the local or
project file). Numeric keys require positive integers.

```sh
sandwichcake config set cache-dir /var/tmp/sandwichcake-cache
sandwichcake config set repos registry.example.com/ai/venv,registry.example.com/ai/weights
sandwichcake config set hf-endpoint https://hf-mirror.com
```

| Key | YAML field | Description |
|-----|------------|-------------|
| `cache-dir` | `cache.dir` | Metadata cache directory (never stores layer blobs) |
| `upload-concurrency` | `upload.concurrency` | Concurrent registry uploads (default 4) |
| `upload-retries` | `upload.retries` | Upload retries (default 3) |
| `repos` | `repos` | Comma-separated repositories for `ls` without arguments |
| `download-concurrency` | `download.concurrency` | Remote model layers streamed in parallel (default 2) |
| `download-connections` | `download.connections` | Parallel ranged requests per file (default 4) |
| `download-segment-size` | `download.segmentSize` | Bytes per ranged request (default `32Mi`) |
| `download-retries` | `download.retries` | Download retries per request (default 5) |
| `hf-endpoint` | `download.hf.endpoint` | Hugging Face endpoint or mirror (default `https://huggingface.co`) |
| `modelscope-endpoint` | `download.modelscope.endpoint` | ModelScope endpoint (default `https://www.modelscope.cn`) |

Any other key fails with
`unknown or project-only config key "…" (use cake-config.yaml for output settings)`.

## config unset {#config-unset}

Remove one key from the user config file. Accepts the same keys as
`set`.

```sh
sandwichcake config unset upload-concurrency
```

## config pull {#config-pull}

Copy a project from a remote into the local project directory.

| Argument / Flag | Description |
|-----------------|-------------|
| `source` | `oci://registry/repo:tag`, `git+https://host/repo.git@branch#subdir` or `git+ssh://…@branch#subdir`. Default: `from:` in `cake-config.yaml` |
| `--force` | Overwrite locally modified or untracked files and allow switching to a different source |

What happens:

1. The remote tree is fetched — the OCI artifact's per-file layers, or
   `<subdir>` of the branch from a cached clone (needs system `git`; a
   nonempty `#subdir` is required).
2. The incoming tree must contain a valid `cake-config.yaml` and only
   safe relative paths.
3. Without `--force`, the pull refuses if the source differs from the
   recorded one (`source changed from X; use --force`), if a previously
   pulled file was modified locally, or if it would overwrite an
   untracked local file (the scaffold written by `init` is exempt).
4. Changes are printed — `- <path>` for files removed upstream,
   `+ <path>` for new or changed files — then written. Only files a
   previous pull installed are ever deleted.
5. The source, its digest or commit and per-file sha256 are recorded in
   `.cake/state.json`.

A project is required: run [`init`](init.md) first, or pass
`--project <new-dir>` to pull into a fresh directory.

```sh
sandwichcake --project ./demo config pull oci://registry.example.com/ai/projects:demo
sandwichcake config pull git+ssh://git@github.com/org/infra.git@main#images/demo
```

## config push {#config-push}

Upload the local project to a remote.

| Argument / Flag | Description |
|-----------------|-------------|
| `destination` | Same forms as `pull`'s source. Default: `from:` in `cake-config.yaml` |
| `--force` | OCI only: overwrite a remote tag that changed since the recorded pull, or that has no pull history. Rejected for git |
| `--new-branch <branch>` | git only: commit on top of the recorded commit and push it as a new branch (for review) instead of updating the source branch. The branch must not exist on origin |

The sync set is the project tree minus `cake-config.local.yaml`,
`.cake/`, `.git/`, any `*.env` path component, a cache directory inside
the project, and `.cakeignore` matches (see
[.cakeignore](../general/reference/cake-config.md#cakeignore)).
Symlinks, FIFOs and sockets are refused. The local `cake-config.yaml`
is validated before pushing.

**OCI** — the project is stored as one artifact
(`application/vnd.sandwichcake.project.v1`) with one layer per file.
Push is allowed to an empty tag, or to the recorded `oci://` address
whose remote digest still matches the recorded pull; anything else —
a changed remote tag, a different destination than was pulled, an
existing tag with no pull history — needs `--force`. This is an
optimistic check, not an atomic compare-and-swap. The new digest is
recorded in `.cake/state.json`.

**git** — pull before pushing. The default mode fetches, requires the
remote branch head to equal the recorded commit (`remote branch
changed (…); pull first` otherwise), updates the synced paths in
`<subdir>` (propagating deletions of previously synced files), commits
and pushes to the same branch. Prints `pushed <target> at <commit>`, or
`no changes`. Neither mode merges or force-pushes. Configure your git
commit identity beforehand.

```sh
sandwichcake config push oci://registry.example.com/ai/projects:demo
sandwichcake config push git+https://github.com/org/infra.git@main#images/demo --new-branch update-demo
```

## See also

- [Projects](../general/concepts/projects.md#sync) — the sync model
- [Configuration Files](../general/reference/cake-config.md)
- [Environment Variables](../general/reference/environment-variables.md)
