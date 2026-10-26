---
title: compose
weight: 2
---

`sandwichcake compose -f cake.yaml` (or `compose <name>` in a project) resolves the base image and every
slice, composes them into a new image, pushes it to `target.ref`, and
writes `cake.lock`.

## What happens

1. **Load** — `cake.yaml` is parsed with strict schema validation
   (unknown fields are errors).
2. **Resolve** — base and each slice ref are resolved: tags become
   digests, indexes select the requested platform manifest, `layers`
   subsets are applied, `verify` entries get their tars downloaded and
   scanned. With `--locked` this whole step is skipped — `cake.lock`'s
   pinned digests are used verbatim.
3. **Check** — each slice type runs its compose-time checks: `glibc`
   requirements against the [resolved base libc](../cake-yaml.md#libc),
   patch/base compatibility, platform mismatches. Findings print as
   `warn:`/`error:` lines; errors abort unless the slice has `force`.
4. **Compose** — base layers + slice layers (in order) + config patch
   produce the final image or index, deterministically.
5. **Plan** — every blob is classified for the target repo:
   `reuse` (already present), `mount` (cross-repo mount), `copy`
   (must upload). See [`plan`](plan.md) for the rendering.
6. **Confirm** — if any bytes must be copied, an interactive prompt
   asks before transferring. Non-interactive shells get an error
   telling you to pass `--yes` (this prevents CI from hanging on a
   prompt nobody can answer).
7. **Push** — configs, layers (reused/mounted where possible) and the
   manifest/index are written to the registry.
8. **Lock** — `cake.lock` is written next to `cake.yaml` with every ref
   pinned to its resolved digest, plus the final output digest.

Output on success:

```
warn: cannot verify glibc>=2.17: base libc unknown
pushed registry.example.com/ai/infer:v1
  digest sha256:8de0…
  wrote cake.lock
```

## Flags

| Flag | Description |
|------|-------------|
| `-f`, `--file` | cake.yaml path (default `./cake.yaml`); named project cakes `cake/<name>.yaml` use `cake/<name>.lock` |
| `--dry-run` | Resolve, check, compose and compute the plan — then stop. Prints `dry-run complete`. The plan itself is rendered only with `-v` |
| `--locked` | Use `cake.lock` only; no tag resolution. Reproduces the locked manifest digest |
| `--verify` | Download layers and scan tar headers during resolve (overrides per-slice `verify`) |
| `--compat` | `docker20`: forbid zstd-compressed layers (Docker <20.10 can't pull them) |
| `--yes` | Skip the copy-confirmation prompt |

## Examples

Normal compose with confirmation:

```sh
$ sandwichcake compose -f cake.yaml
460.2MiB will be copied to the target registry. Continue? [y/N] y
pushed registry.example.com/ai/infer:v1
  digest sha256:8de0…
  wrote cake.lock
```

Non-interactive / CI usage:

```sh
sandwichcake compose -f cake.yaml --yes
```

Reproduce a build exactly (e.g. after `git checkout` of a release tag):

```sh
sandwichcake compose -f cake.yaml --locked --yes
```

Verify layers and forbid zstd for an old Docker daemon:

```sh
sandwichcake compose -f cake.yaml --verify --compat docker20 --yes
```

## Notes

- `compose` is idempotent by construction: identical resolved inputs
  produce an identical manifest digest, so re-running an unchanged
  cake.yaml reports the same digest and transfers nothing new
  (everything classifies `reuse`).
- `cake.lock` is meant to be committed — `compose --locked` needs no
  tag resolution and yields bit-identical output.
- In a project, `compose serve` loads `cake/serve.yaml`. Short refs like
  `venv:torch` and `image:serve` are expanded using `cake-config.yaml`
  before resolution. Full refs such as `python:3.12-slim` stay unchanged.
