---
title: ls
weight: 7
---

`sandwichcake ls [repo…]` prints the refs of every slice found in the
given repositories — only manifests annotated as slices are listed, not
ordinary images. Read-only.

## What happens

For each repo argument, the tag list is fetched and every tag's manifest
is checked for `com.github.baba.sandwichcake.kind = slice`. Matching
refs print one per line:

```text
$ sandwichcake ls registry.example.com/ai/weights registry.example.com/ai/venv
registry.example.com/ai/weights:qwen3-8b
registry.example.com/ai/weights:llama-4-scout
registry.example.com/ai/venv:torch-2.8
```

## Config file

With **no arguments**, `ls` reads default repos from
`$XDG_CONFIG_HOME/sandwichcake/config.yaml`
(usually `~/.config/sandwichcake/config.yaml`). Inside a project — the
nearest `cake-config.yaml` walking up from cwd, or `--project <dir>` —
`<project>/cake-config.local.yaml` is also read, and its `repos:` list
**overrides** the global one rather than merging.

```yaml
repos:
  - registry.example.com/ai/weights
  - registry.example.com/ai/venv
```

Run `sandwichcake config init --global` to create the file, then use
`sandwichcake config set repos registry.example.com/ai/weights,registry.example.com/ai/venv`
or edit the YAML directly. See [`config`](config.md) for saved-default
settings. If neither arguments nor config repos exist, the command errors.
Use [`discover`](discover.md) first to find candidate repo names.

## Examples

```sh
sandwichcake ls                                   # configured repos
sandwichcake ls registry.example.com/ai/venv      # one repo
sandwichcake ls reg/ai/venv reg/ai/weights        # several
```

## Notes

- The global flags apply: `--project <dir>` selects the project whose
  `cake-config.local.yaml` supplies `repos`, and `--insecure-registry`
  permits plain-HTTP registry access.
- Per-tag failures are skipped silently: fetch errors (404, auth,
  network mid-list), manifest parse errors, and tags pointing at
  indexes or non-slice manifests.
- Only top-level tag manifests are inspected — a slice reachable only
  through an index tag is never reported.
- If the tag list itself cannot be fetched for a repo, the command
  aborts with `<repo>: <error>`: output already printed stays, later
  repos are never processed.
- One line per `repo:tag` — pipe to `xargs sandwichcake inspect` for a
  quick inventory.
