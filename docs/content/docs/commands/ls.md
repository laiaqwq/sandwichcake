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
(usually `~/.config/sandwichcake/config.yaml`):

```yaml
repos:
  - registry.example.com/ai/weights
  - registry.example.com/ai/venv
```

If neither arguments nor config repos exist, the command errors.
Use [`discover`](discover.md) first to find candidate repo names.

## Examples

```sh
sandwichcake ls                                   # configured repos
sandwichcake ls registry.example.com/ai/venv      # one repo
sandwichcake ls reg/ai/venv reg/ai/weights        # several
```

## Notes

- Tags pointing at indexes or non-slice manifests are skipped silently.
- One line per `repo:tag` — pipe to `xargs sandwichcake inspect` for a
  quick inventory.
