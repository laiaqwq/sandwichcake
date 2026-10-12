---
title: CLI Reference
weight: 6
---

## Global flags

Available on every command:

| Flag | Description |
|------|-------------|
| `--cache-dir` | Metadata cache directory (default `~/.cache/sandwichcake`) |
| `--insecure-registry` | Registry host to access over plain HTTP (repeatable). When set, **all** registry traffic in that invocation is treated as insecure |
| `-v`, `--verbose` | Verbose output (also makes `compose` render the transfer plan) |

## Authentication

sandwichcake uses the standard Docker credential chain
(`~/.docker/config.json` and configured credential helpers). There are
no separate login commands — authenticate with `docker login` (or your
registry's helper) first.

## Commands

{{< cards >}}
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
`compose`, `tag`) print the pushed ref and digest on success.
