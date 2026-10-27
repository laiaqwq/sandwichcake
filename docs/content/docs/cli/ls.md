---
title: ls
weight: 9
aliases:
  - /docs/commands/ls/
---

List the slices in one or more repositories — only manifests annotated
as slices, not ordinary images. Read-only.

## Synopsis

```text
sandwichcake ls [repo...]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `repo` | Repository to scan (repeatable). Default: the configured `repos` list |

## Flags

No command-specific flags; see the [global flags](_index.md#global-flags).
`--project <dir>` selects the project whose `cake-config.local.yaml`
supplies `repos`.

## What happens

For each repository, the tag list is fetched and every tag's manifest
is checked for `com.github.baba.sandwichcake.kind = slice`. Matching
refs print one per line.

With **no arguments**, `ls` uses the `repos:` list from
`cake-config.local.yaml` (inside a project) if set, else from the user
config — the local list **replaces** the user one rather than merging.
If neither arguments nor configured repos exist, the command errors.

```sh
sandwichcake config set repos registry.example.com/ai/weights,registry.example.com/ai/venv
```

## Output

```text
$ sandwichcake ls registry.example.com/ai/weights registry.example.com/ai/venv
registry.example.com/ai/weights:qwen3-8b
registry.example.com/ai/weights:llama-4-scout
registry.example.com/ai/venv:torch-2.8
```

## Examples

```sh
sandwichcake ls                                   # configured repos
sandwichcake ls registry.example.com/ai/venv      # one repo
sandwichcake ls reg/ai/venv reg/ai/weights        # several
sandwichcake ls | xargs -n1 sandwichcake inspect  # quick inventory
```

## Notes

- Per-tag failures are skipped silently: fetch errors, manifest parse
  errors, and tags pointing at indexes or non-slice manifests.
- Only top-level tag manifests are inspected — a slice reachable only
  through an index tag is not reported.
- If the tag list itself cannot be fetched for a repo, the command
  aborts with `<repo>: <error>`; output already printed stays.
- Use [`discover`](discover.md) first to find candidate repo names.
