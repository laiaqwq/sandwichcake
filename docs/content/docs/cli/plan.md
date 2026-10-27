---
title: plan
weight: 5
aliases:
  - /docs/commands/plan/
---

Show the transfer plan for a cake without pushing anything.

## Synopsis

```text
sandwichcake plan [name] [flags]
sandwichcake plan -f <cake.yaml> [flags]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `name` | A project cake name (`plan serve`), or a path if it contains `/` or ends in `.yaml`. Mutually exclusive with `-f`. Default: `./cake.yaml` |

## Flags

| Flag | Description |
|------|-------------|
| `-f`, `--file <path>` | cake.yaml path (default `./cake.yaml`) |
| `--verify` | Download layer tars and scan headers during resolve (libc detection, overlay validation) |

Plus the [global flags](_index.md#global-flags) (`-v` is a no-op —
the plan is always rendered).

## What happens

`plan` runs the same pipeline as `compose --dry-run` — resolve → check
→ compose → classify — and always renders the plan. It **never pushes
anything**, never prompts and never writes a lock. Unlike
`compose --dry-run` it prints only the plan: `warn:` findings are
dropped and there is no `dry-run complete` marker. There is no
`--locked`: `plan` always resolves tags fresh, so its output can drift
from a locked compose.

## Output

One line per unique blob (all layers plus the config), sorted by
digest, then a summary:

| Marker | Strategy | Meaning |
|--------|----------|---------|
| `=` | `reuse` | Already in the target repo — zero transfer |
| `~` | `mount` | In another repo on the same registry — server-side cross-repo mount |
| `+` | `copy` | Must be uploaded from the client |

Blob names come from the layer `name` annotation (with the pack type in
parentheses), falling back to the digest prefix.

```text
  = config                                          1.5KiB  reuse
  = debian-slim (base)                             28.4MiB  reuse
  ~ site-packages (venv)                          410.0MiB  mount
  + qwen3-8b (weight)                               4.9GiB  copy

Plan: 28.4MiB reuse (2 layers), 410.0MiB mount (1 layers), 4.9GiB copy (1 layers)
```

## Examples

```sh
sandwichcake plan -f cake.yaml
sandwichcake plan serve --verify
```

## Notes

- `mount` is best-effort: a blob classified `~` may still fall back to
  a chunked upload at push time if the registry rejects the mount.
- Identical blobs across refs are deduplicated before classification.
- A per-ref `verify: true` triggers the same scan as `--verify`.

## See also

- [Transfer Plan](../general/concepts/transfer-plan.md)
- [`compose`](compose.md)
