---
title: plan
weight: 3
---

`sandwichcake plan -f cake.yaml` is `compose --dry-run` with the
transfer plan always rendered. It performs the full resolve → check →
compose → classify pipeline but **never pushes anything** and never
writes `cake.lock`.

## What it shows

For every unique blob (all layers + the config) in the composed image,
one line in terraform-plan style:

| Marker | Strategy | Meaning |
|--------|----------|---------|
| `=` | `reuse` | Blob already exists in the target repo — zero transfer |
| `~` | `mount` | Blob exists in another repo on the same registry — copied server-side via cross-repo mount, zero client upload |
| `+` | `copy` | Blob must be uploaded from the client |

Blob names come from the layer `name` annotation (with the pack type in
parentheses), falling back to the digest prefix. The summary line totals
bytes and layer counts per strategy.

```text
  = config                                          1.5KiB  reuse
  = debian-slim (base)                             28.4MiB  reuse
  ~ site-packages (venv)                          410.0MiB  mount
  + qwen3-8b (weight)                               4.9GiB  copy

Plan: 28.4MiB reuse (2 layers), 410.0MiB mount (1 layers), 4.9GiB copy (1 layers)
```

## Flags

| Flag | Description |
|------|-------------|
| `-f`, `--file` | cake.yaml path (default `./cake.yaml`); or pass a project cake name, e.g. `plan serve` |
| `--verify` | Download layer tars and scan headers during resolve (libc detection, overlay validation) |

## Examples

```sh
$ sandwichcake plan -f cake.yaml
```

Plan with deep verification:

```sh
sandwichcake plan -f cake.yaml --verify
```

## Notes

- `plan` always prints the plan; `compose --dry-run` prints findings and
  `dry-run complete` but only renders the plan when `-v` is also given.
- `plan` never prompts — the copy-confirmation step only exists in the
  push path.
- A slice that classifies `copy` in `plan` may still be cheap to push:
  chunked uploads resume, and identical blobs across slice refs are
  deduplicated before classification.
