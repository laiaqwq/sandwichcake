---
title: Determinism & Locks
weight: 5
---

Identical inputs produce identical digests — for packed slices and for
composed images. This is what makes reuse, `diff` and reproducible
releases work.

## Deterministic packing

A slice's digest depends only on the file tree, the pack options and
the tool version:

- **Tar headers are normalized.** Entries are ordered by path; the
  only timestamp written is the modification time, taken from
  `SOURCE_DATE_EPOCH` (Unix 0 when unset); uid/gid default
  to `0:0` (`--owner`), modes are normalized (`--chmod`); symlinks are
  stored, never followed.
- **Ancestor directories.** Directories above `--root` (e.g. `opt/` for
  `/opt/venv`) are written as `0755 root:root`, like BuildKit `COPY`.
  These entries override the base's attributes for the same directory;
  `--no-parents` omits them when the base must keep its own.
- **No whiteouts.** A `.wh.*` file in the source is an error.
- **Compression** output is independent of thread count. Reuse is
  decided by diff_id (uncompressed), so a different compression of the
  same tar is still recognized as the same layer by `diff`.
- **Tool version.** Every layer carries
  `com.github.baba.sandwichcake.tool.version`, so digests are stable
  within one sandwichcake version, not across versions.

Repacking an unchanged directory with the same options therefore
yields the same manifest digest, and a re-push transfers nothing.

## Deterministic composition

The composed config's `created` time also comes from
`SOURCE_DATE_EPOCH`. Given the same resolved base and slice digests,
the same `config:` patch and annotations, `compose` produces the same
manifest (or index) digest.

Tags, however, move. That is what the lock is for.

## cake.lock

After a successful push — never on `--dry-run` or `plan` — `compose`
writes a lock next to the cake file (`cake.yaml` → `cake.lock`,
`cake/serve.yaml` → `cake/serve.lock`). It records:

- the target tag, the pushed digest and the built platforms (in order);
- every base/slice ref resolved to `repo@digest`, with the original tag
  ref kept as `input:`;
- per platform, the selected manifest and full layer digest list.

`compose --locked` replays the lock without resolving any tags. Each
`repo@digest` is still resolved per platform and checked against the
recorded manifest and layer digests, so a registry that serves
different content fails loudly. Because `input:` keeps the original tag
ref, `org.opencontainers.image.base.name` stays identical across
replays, and so does the output digest.

In a project, the lock stores fully expanded refs, so later edits to
`cake-config.yaml` (prefix, repos) do not change what a locked build
uses; `--registry-prefix` that would expand refs differently from the
lock is rejected.

{{< callout type="warning" >}}
The lock does not pin everything. `config:` and `target.annotations`
are still read from cake.yaml (the lock's echoed `config:` is never
read back), and `SOURCE_DATE_EPOCH` must match. Change any of them and
`--locked` produces a different digest.
{{< /callout >}}

Commit the lock alongside the cake file. Format:
[cake.lock reference](../reference/cake-yaml.md#cakelock).

## Checking reproducibility

```sh
sandwichcake compose serve --locked --yes      # prints the digest
sandwichcake diff registry.example.com/ai/infer:serve registry.example.com/ai/infer:serve-v1
```

`diff` compares by diff_id, so it shows any layer-level difference
between two builds.
