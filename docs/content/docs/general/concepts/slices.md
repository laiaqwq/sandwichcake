---
title: Slices
weight: 1
aliases:
  - /docs/slices/
---

A **slice** is an OCI image manifest that packages one logical directory
tree as a set of annotated tar layers. Slices are pushed to a registry
like normal images, then picked up later by `compose` and layered onto a
base image.

## Anatomy

- **Manifest** — a standard OCI image manifest. Manifest-level
  annotations mark it a slice (`kind = slice`) and may declare the
  platform (`any` for platform-agnostic slices).
- **Layers** — standard OCI tar media types only:
  `application/vnd.oci.image.layer.v1.tar`, `+gzip` or `+zstd`. The
  `application/vnd.sandwichcake.*` media types belong to the metadata
  artifact below, never to slice layers. Each layer *descriptor* in
  the manifest carries the sandwichcake annotations describing that
  layer: type, schema, container root, name, part, file count, tool
  version and type-specific data. Provenance keys are type-specific
  only (e.g. `weight.source`, `patch.base`) — the generic
  `source.image` / `source.index` keys are stamped at compose time
  onto external (non-slice) layers, not written by `pack`.
- **Config** — a standard OCI image config with one `rootfs.diff_ids`
  entry and one `history` entry per layer. Platform-independent slices
  (weights, configuration) use `os`/`architecture` `unknown`, the
  convention BuildKit attestation manifests use, so `docker pull` of a
  slice fails on platform mismatch — slices are not meant to run on
  their own.
- **Annotations** — all keys live in the
  `com.github.baba.sandwichcake.*` namespace. Type-specific keys are
  namespaced further per type. Full list:
  [Annotations & Media Types](../reference/annotations.md).
- **Metadata artifact** — an OCI artifact pushed by digest with the
  slice manifest as its `subject`, carrying extra files (dependency
  lock files, weight manifests, freeze output, …).

## Deterministic packing

Identical inputs produce identical manifest digests. Tar headers are
normalized: timestamps come from `SOURCE_DATE_EPOCH` (or Unix 0),
entries are always ordered by path, and `--owner`, `--chmod` and
`--no-parents` control uid/gid, modes and ancestor-directory entries
respectively. This is what makes `compose --locked` reruns and the
dedup of the transfer plan possible.

## Layer splitting

Unless `--no-split` is given, large trees are split into multiple
layers using per-type rules plus two thresholds:

| Flag | Default | Effect |
|------|---------|--------|
| `--small-threshold` | 64Mi | Files below this may be grouped into a shared layer |
| `--large-threshold` | 5Gi | Files above this always get their own layer |

`--no-split` disables the size binning only; per-type `Separate` layout
rules still apply — `pack venv --no-split` still puts `.python/` in its
own layer, so a single layer is not guaranteed.

Splitting matters for transfer efficiency: a rerun that only changes a
few small files only needs to copy the small-files layer, not a 5GiB
weight blob.

## Path rewriting

`--root` sets the base path inside the image — tar member names are
always `<root>/<relative path>`, so `--root` alone relocates the tree
without staging a copy.

`--rewrite-prefix src=dst` (repeatable) is a venv-only option; other
types accept but silently ignore it. It rewrites file *contents*, not
packed paths: literal `src`→`dst` text replacement in `pyvenv.cfg`,
text files in `bin/` (shebangs), `*.pth` in site-packages, and
`_sysconfigdata_*.py` / `config-*/Makefile` under `.python/lib/`.
ELF and binary files are skipped.

## Metadata artifacts {#metadata}

After the slice manifest is pushed, sandwichcake PUTs a metadata
artifact by digest with `subject` pointing at the slice. Registries
that acknowledge `subject` (response header `OCI-Subject`) get a proper
OCI 1.1 referrer; other registries get the fallback index maintained at
tag `sha256-<subject hex>`. `--artifact-compat` forces the fallback
index plus a standard image config media type for maximum
compatibility.

Fetch individual metadata files with
[`inspect <ref> --file <title>`](../../cli/inspect.md). Registry
behavior is summarized in
[Registry Compatibility](../reference/registry-compatibility.md).

## Merging into an index

`pack --merge` adds the new slice manifest to the target tag instead of
replacing it: an existing index gets the new entry merged in
(replacing any same-platform entry), an existing single manifest is
preserved and converted into a two-entry index, and an unused tag gets
a fresh index — for example when packing the same slice for
`linux/amd64` and `linux/arm64` separately.
