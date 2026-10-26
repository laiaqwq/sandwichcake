---
title: CLI Reference
weight: 6
---

## Global flags

Available on every command:

| Flag | Description |
|------|-------------|
| `--cache-dir` | Metadata cache directory; overrides `SANDWICHCAKE_CACHE_DIR` and `cache.dir` (default `~/.cache/sandwichcake`) |
| `--project` | Project directory (otherwise discover `cake-config.yaml` upwards from cwd) |
| `--insecure-registry` | Registry host to access over plain HTTP (repeatable). When set, **all** registry traffic in that invocation is treated as insecure |
| `-v`, `--verbose` | Verbose output (also makes `compose` render the transfer plan) |

## Saved defaults

See the [`init`](init.md) and [`config`](config.md) command pages for setup,
all supported settings, examples, and override precedence.

Run `sandwichcake init [dir]` to create a project. Run
`sandwichcake config init --global` to create optional user defaults.
Use `config show --origin`, `get`, `set`, and `unset` to inspect or change
environment settings:

```yaml
repos:
  - registry.example.com/ai/weights
cache:
  dir: /tmp/sandwichcake-cache
upload: {concurrency: 4, retries: 5}
```

Project `cake-config.yaml` holds registry prefix, type repos, pack defaults
(root, compression, split, ownership), model container paths, and short-ref
expansion. Those settings cannot be put in global or local configuration.
`config pull/push` syncs the whole project over OCI or git. See
[`config`](config.md) for the full schema, precedence and safeguards.

## Authentication

sandwichcake uses the standard Docker credential chain
(`~/.docker/config.json` and configured credential helpers). There are
no separate login commands — authenticate with `docker login` (or your
registry's helper) first.

## Commands

{{< cards >}}
  {{< card link="init" title="init" subtitle="Create a project" >}}
  {{< card link="config" title="config" subtitle="Configure and sync a project" >}}
  {{< card link="pack" title="pack" subtitle="Pack a directory into a slice" >}}
  {{< card link="compose" title="compose" subtitle="Compose slices onto a base image" >}}
  {{< card link="plan" title="plan" subtitle="Show the transfer plan" >}}
  {{< card link="inspect" title="inspect" subtitle="Inspect an image or slice" >}}
  {{< card link="diff" title="diff" subtitle="Compare two images by diff_id" >}}
  {{< card link="tag" title="tag" subtitle="Copy a manifest to a new tag" >}}
  {{< card link="ls" title="ls" subtitle="List slices in repos" >}}
  {{< card link="discover" title="discover" subtitle="List repos under a prefix" >}}
  {{< card link="rm" title="rm" subtitle="Delete a manifest and its metadata" >}}
  {{< card link="types" title="types" subtitle="List registered pack types" >}}
  {{< card link="version" title="version" subtitle="Print build version" >}}
  {{< card link="completion" title="completion" subtitle="Shell completion scripts" >}}
{{< /cards >}}

Read-only commands (`plan`, `inspect`, `diff`, `ls`, `discover`,
`types`, `version`, `completion`) never write to the registry or the local
filesystem beyond the metadata cache. Commands that push (`pack`,
`compose`) print the pushed ref and manifest digest on success; `tag`
prints the source and destination refs.
