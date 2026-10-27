---
title: CLI Reference
weight: 3
aliases:
  - /docs/commands/
---

```text
sandwichcake [global flags] <command> [args] [flags]
```

The binary is also installed as `swcake`; both names behave
identically. Every command supports `-h`/`--help`.

## Commands

| Command | Purpose | Writes to registry |
|---------|---------|--------------------|
| [`init`](init.md) | Create a project | – |
| [`config`](config.md) | Show/change environment settings; sync a project over OCI or git | `push` only |
| [`pack`](pack/_index.md) | Pack a directory or remote model into a slice | yes |
| [`compose`](compose.md) | Compose a base and slices into an image | yes |
| [`plan`](plan.md) | Show the transfer plan without pushing | – |
| [`inspect`](inspect.md) | Inspect an image, index or slice | – |
| [`diff`](diff.md) | Compare two images by diff_id | – |
| [`tag`](tag.md) | Copy a manifest to a new tag or repo | yes |
| [`ls`](ls.md) | List slices in repositories | – |
| [`discover`](discover.md) | List repositories under a prefix | – |
| [`rm`](rm.md) | Delete a manifest and its referrers | yes (delete) |
| [`types`](types.md) | List registered pack types | – |
| [`completion`](completion.md) | Generate shell completion scripts | – |
| [`version`](version.md) | Print the build version | – |

Read-only commands never write to the registry or the local filesystem
beyond the metadata cache.

## Global flags {#global-flags}

Available on every command:

| Flag | Description |
|------|-------------|
| `--cache-dir <dir>` | Metadata cache directory; overrides `SANDWICHCAKE_CACHE_DIR` and `cache.dir` (default `$XDG_CACHE_HOME/sandwichcake`, else `~/.cache/sandwichcake`, else the OS temp dir) |
| `--project <dir>` | Project directory (otherwise discover `cake-config.yaml` upwards from cwd) |
| `--registry-prefix <prefix>` | Override `registry.prefix` from `cake-config.yaml` for this invocation (requires a project; other naming rules still apply). `compose --locked` fails if it would expand refs differently from the lock |
| `--insecure-registry <host>` | Access registries over plain HTTP (repeatable). Any non-empty value makes **all** registry traffic in that invocation insecure |
| `-v`, `--verbose` | Verbose output (also makes `compose` render the transfer plan) |

## Conventions

- **Refs** are ordinary image references: `registry/repo:tag` or
  `registry/repo@sha256:…`. Inside project cakes, short refs such as
  `venv:torch-2.8` are expanded (see
  [short refs](../general/concepts/projects.md#short-refs)).
- **Sizes** in flags and config accept `Ki`/`Mi`/`Gi` suffixes
  (`64Mi`, `5Gi`).
- **Output.** Pushing commands print the pushed ref and digest on
  stdout: `pack` and `compose` print `pushed <ref>` followed by
  indented `digest`, `metadata` or `wrote <lock>` lines; `tag` prints
  `tagged <src> -> <dst>`. Warnings print as `warn: …`; prompts go to
  stderr.
- **Exit status.** `0` on success, `1` on any error (the message is
  printed as `Error: …`). Differences found by `diff` are not errors.
- **Confirmation.** `compose` asks before copying bytes; pass `--yes`
  in non-interactive runs. `rm` never asks.

## Configuration and credentials

- Settings: [Configuration Files](../general/reference/cake-config.md)
  and [Projects](../general/concepts/projects.md).
- Credentials: [Authentication](../general/reference/authentication.md).
- Environment: [Environment Variables](../general/reference/environment-variables.md).
