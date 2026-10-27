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
   scanned. A platform mismatch is a hard error here, not a finding.
   The platform list is `target.platforms`, else the base's: every
   platform entry in a base index, or the base config's os/arch for a
   single-manifest base. A ref's `platform:` picks `os/arch` from an
   index, or `any` for a platform-independent manifest. With
   `--locked` the platform list is pinned from the lock and only tag
   resolution is skipped: refs come from the
   lock as `repo@digest`, still fully resolved per platform, and every
   resolved manifest+layer digest is verified against the lock.
3. **Check** — each slice type runs its compose-time checks: `glibc`
   requirements against the [resolved base libc](../cake-yaml.md#libc),
   patch/base compatibility. Compose adds two of its own: non-overlay
   layer `root`s must not nest or coincide (identical `root`s within
   one slice are exempt), and `verify`ed plain-image tar paths must
   not land in another slice's `root` or recur in two verified
   layers. Only `warn:` findings print; error
   findings abort the run as `Error: compose: …` (co-occurring warns
   are dropped). `force` does not downgrade findings — it only
   bypasses the resolve-time pure-file judgment for plain-image
   layers.
4. **Compose** — base layers + slice layers (in order) + config patch
   produce the final image per platform, deterministically: a plain
   manifest for one platform, an OCI index for several.
5. **Plan** — every blob is classified for the target repo:
   `reuse` (already present), `mount` (cross-repo mount), `copy`
   (must upload). See [`plan`](plan.md) for the rendering.
6. **Confirm** — if any bytes must be copied, a prompt on stderr asks
   before transferring; only `y`/`Y` proceeds (anything else aborts).
   Non-interactive shells get an error telling you to pass `--yes`
   (this prevents CI from hanging on a prompt nobody can answer).
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
| `-f`, `--file` | cake.yaml path (default `./cake.yaml`); named project cakes `cake/<name>.yaml` use `cake/<name>.lock`. Mutually exclusive with the positional arg |
| `--dry-run` | Resolve, check, compose and compute the plan — then stop. Prints `dry-run complete`. The plan itself is rendered only with `-v` |
| `--locked` | Refs and platforms come from `cake.lock`; tag resolution is skipped but each `repo@digest` ref is still fully resolved per platform and verified against the lock. Bypasses `onTagExists`. cake.yaml still supplies `config:`/`target.annotations`, so the locked digest reproduces only while those are unchanged |
| `--verify` | Download layer tars and scan headers during resolve — union with per-slice `verify:`; also scans base layers and feeds base libc detection |
| `--compat` | `docker20`: forbid zstd-compressed layers (Docker <20.10 can't pull them) |
| `--yes` | Skip the copy-confirmation prompt |
| `-v`, `--verbose` | Render the transfer plan — on real pushes too, not just `--dry-run` |

## Examples

Normal compose with confirmation:

```sh
$ sandwichcake compose -f cake.yaml
482816409 bytes will be copied to the target registry. Continue? [y/N] y
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
  produce an identical manifest digest provided `SOURCE_DATE_EPOCH` is
  held constant (unset = Unix 0), so re-running an unchanged
  cake.yaml reports the same digest and transfers nothing new
  (everything classifies `reuse`).
- `cake.lock` is meant to be committed — it pins each `input:`
  original ref plus per-platform `resolved:` manifest+layer digests
  (see [cake.lock](../cake-yaml.md#cakelock)). The lock's `config:` is
  written but never read back; `--locked` yields bit-identical output
  only while cake.yaml's `config:`/`target.annotations` and
  `SOURCE_DATE_EPOCH` are unchanged.
- `registry.onTagExists` (see [project config](config.md#project-configuration))
  is honored on non-locked, non-dry-run runs: `skip` prints
  `skipped <ref> (tag exists)` and exits without pushing or writing
  the lock; `fail` errors early; unset/`overwrite` replaces the tag.
- In a project, `compose serve` loads `cake/serve.yaml`. Short refs like
  `venv:torch` and `image:serve` are expanded using `cake-config.yaml`
  before resolution. Full refs such as `python:3.12-slim` stay unchanged.
