---
title: Slices
weight: 3
---

A **slice** is an OCI image manifest that packages one logical directory
tree as a set of annotated tar layers. Slices are pushed to a registry
like normal images, then picked up later by `compose` and layered onto a
base image.

## Anatomy

- **Manifest** — a standard OCI image manifest. The config blob carries
  sandwichcake annotations describing the slice type, container root,
  provenance and type-specific data.
- **Layers** — `application/vnd.sandwichcake.*` (or standard OCI) tar
  media types, `+gzip` / `+zstd` / uncompressed.
- **Annotations** — all keys live in the
  `com.github.baba.sandwichcake.*` namespace. Type-specific keys are
  namespaced further per type.
- **Metadata artifact** — an OCI artifact pushed by digest with the
  slice manifest as its `subject`, carrying extra files (dependency
  lock files, weight manifests, freeze output, …).

## Deterministic packing

Identical inputs produce identical manifest digests. Tar headers are
normalized: timestamps come from `SOURCE_DATE_EPOCH` (or Unix 0), and
ordering, uid/gid and modes are controlled by `--owner`, `--chmod` and
`--no-parents`. This is what makes `compose --locked` reruns and the
dedup of the transfer plan possible.

## Layer splitting

Unless `--no-split` is given, large trees are split into multiple
layers using per-type rules plus two thresholds:

| Flag | Default | Effect |
|------|---------|--------|
| `--small-threshold` | 64Mi | Files below this may be grouped into a shared layer |
| `--large-threshold` | 5Gi | Files above this always get their own layer |

Splitting matters for transfer efficiency: a rerun that only changes a
few small files only needs to copy the small-files layer, not a 5GiB
weight blob.

## Path rewriting

`--rewrite-prefix src=dst` (repeatable) rewrites path prefixes as files
are packed, and `--root` sets the base path inside the image. Together
they let you pack a build directory into a different runtime layout
without staging a copy.

## Metadata artifacts {#metadata}

After the slice manifest is pushed, sandwichcake PUTs a metadata
artifact by digest with `subject` pointing at the slice. Registries
that acknowledge `subject` (response header `OCI-Subject`) get a proper
OCI 1.1 referrer; other registries get the fallback index maintained at
tag `sha256-<subject hex>`. `--artifact-compat` forces the fallback
index plus a standard image config media type for maximum
compatibility.

Fetch individual metadata files with
`inspect <ref> --file <title>`.

## Merging into an index

`pack --merge` merges the new slice manifest into an existing multi-arch
index at the target tag instead of replacing it — for example when
packing the same slice for `linux/amd64` and `linux/arm64` separately.
