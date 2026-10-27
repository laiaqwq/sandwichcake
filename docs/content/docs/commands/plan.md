---
title: plan
weight: 3
---

`sandwichcake plan -f cake.yaml` runs the same pipeline as
`compose --dry-run` — resolve → check → compose → classify — and
always renders the transfer plan. It **never pushes anything** and
never writes `cake.lock`. Unlike `compose --dry-run` it prints only
the plan: `warn:` findings are dropped silently and there is no
`dry-run complete` marker. There is no `--locked` — `plan` never
reads `cake.lock` and always resolves tags fresh, so its output can
drift from a locked compose.

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
| `-f`, `--file` | cake.yaml path (default `./cake.yaml`). A positional arg also works: a project cake name (`plan serve`), or any arg containing `/` or ending `.yaml` as a path (`plan ./dir/foo.yaml`). `-f` + arg together is an error |
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
- `mount` is best-effort: a blob classified `~` may still fall back to
  chunked upload at push time if the registry rejects the mount.
- Plan lines are sorted by digest, not layer order. `-v` is a no-op —
  `plan` always renders anyway.
- A per-slice `verify: true` in cake.yaml triggers the same scan as
  `--verify` (they union), so plan output reflects it.
