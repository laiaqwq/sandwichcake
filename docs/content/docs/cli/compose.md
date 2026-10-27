---
title: compose
weight: 4
aliases:
  - /docs/commands/compose/
---

Resolve a base image and slices, compose them into a new image, push it
to `target.ref` and write the lock.

## Synopsis

```text
sandwichcake compose [name] [flags]
sandwichcake compose -f <cake.yaml> [flags]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `name` | A project cake name (`serve` → `cake/serve.yaml`), or a path if it contains `/` or ends in `.yaml`. Mutually exclusive with `-f`. Default: `./cake.yaml` |

## Flags

| Flag | Description |
|------|-------------|
| `-f`, `--file <path>` | cake.yaml path (default `./cake.yaml`) |
| `--dry-run` | Resolve, check, compose and compute the plan — then stop. Prints findings and `dry-run complete`; the plan itself only with `-v` |
| `--locked` | Take refs and platforms from the lock; skip tag resolution but still resolve each `repo@digest` per platform and verify it against the lock. Bypasses `onTagExists` |
| `--verify` | Download layer tars and scan headers during resolve — union with per-ref `verify:`; also scans base layers for libc detection |
| `--compat <target>` | `docker20`: reject zstd-compressed layers (Docker < 23 cannot pull them) |
| `--yes` | Skip the copy-confirmation prompt (required in non-interactive runs that copy bytes) |

Plus the [global flags](_index.md#global-flags); `-v` renders the
transfer plan on real pushes too.

## What happens

1. **Load** — the cake file is parsed strictly; in a project, short
   refs are expanded.
2. **Resolve** — tags become digests, indexes select each target
   platform's manifest, `layers` selections apply, `verify` entries are
   scanned. With `--locked`, refs and platforms come from the lock.
3. **Check** — type checks (glibc, arch, patch base), root overlap and
   verified-path checks. `warn:` findings print; errors abort as
   `Error: compose: …`.
4. **Compose** — base layers + slice layers (in order) + config patch,
   per platform: one manifest, or an OCI index for several platforms.
5. **Plan** — every blob is classified `reuse`, `mount` or `copy`.
6. **Confirm** — if bytes must be copied, a prompt on stderr asks
   first; only `y`/`Y` proceeds. Non-interactive shells fail and ask
   for `--yes`.
7. **Push** — blobs (reused or mounted where possible), then the
   manifest/index.
8. **Lock** — the lock is written next to the cake file:
   `cake.yaml` → `cake.lock`, `cake/<name>.yaml` → `cake/<name>.lock`.

In a project, `registry.onTagExists` applies on non-locked,
non-dry-run runs: `skip` prints `skipped <ref> (tag exists)` and exits
without pushing or writing the lock; `fail` errors early.

Semantics in depth: [Composition](../general/concepts/composition.md),
[Transfer Plan](../general/concepts/transfer-plan.md),
[Determinism & Locks](../general/concepts/determinism-and-locks.md).

## Output

```text
warn: cannot verify glibc>=2.17: base libc unknown
pushed registry.example.com/ai/infer:v1
  digest sha256:8de0…
  wrote cake.lock
```

## Examples

Normal compose with confirmation:

```sh
$ sandwichcake compose -f cake.yaml
482816409 bytes will be copied to the target registry. Continue? [y/N] y
pushed registry.example.com/ai/infer:v1
  digest sha256:8de0…
  wrote cake.lock
```

A project cake, in CI:

```sh
sandwichcake compose serve --yes
```

Reproduce a build exactly (e.g. after checking out a release):

```sh
sandwichcake compose serve --locked --yes
```

Verify layers and forbid zstd for an old Docker daemon:

```sh
sandwichcake compose -f cake.yaml --verify --compat docker20 --yes
```

## Notes

- Re-running an unchanged cake produces the same digest (with the same
  `SOURCE_DATE_EPOCH`) and transfers nothing — everything is `reuse`.
- `--locked` reproduces the digest only while the cake's `config:`,
  `target.annotations` and `SOURCE_DATE_EPOCH` are unchanged; the lock's
  echoed `config:` is never read back.
- Commit the lock — see [cake.lock](../general/reference/cake-yaml.md#cakelock).

## See also

- [`plan`](plan.md) — the same pipeline without pushing
- [cake.yaml reference](../general/reference/cake-yaml.md)
