---
title: Concepts
weight: 1
---

sandwichcake composes images from content that already lives in a
registry. There are two stages:

```text
 local dir / hf:// ──pack──▶  slice (registry)  ─┐
                                                 ├──compose──▶ image or index (registry)
 base image (registry) ──────────────────────────┘        + cake.lock
```

1. **pack** turns one directory (or remote model) into a *slice* and
   pushes it. This is the only stage that reads local files.
2. **compose** reads a `cake.yaml`, resolves the base image and slices,
   stacks their layers, patches the config and pushes the result. It
   never builds new layers; it only rearranges existing blobs.

## Glossary

| Term | Meaning |
|------|---------|
| **Slice** | An OCI image manifest whose layers package one logical directory tree, annotated with `kind = slice`. The unit `pack` produces. See [Slices](slices.md). |
| **Layer** | A tar blob. sandwichcake distinguishes *base* layers (the whole base image, always first), *file* layers (packed by sandwichcake) and *external* layers (selected from some other image). See [Composition](composition.md#layer-sources). |
| **Type** | The kind of content a slice holds — `venv`, `weight`, `binary`, `patch`, `other`. A type decides defaults, layer layout, annotations, metadata and compose-time checks. See [Pack Types](pack-types.md). |
| **Root** | The absolute path a slice's files occupy in the image (`--root`). Roots of different slices must not overlap. |
| **Metadata artifact** | An OCI artifact attached to a slice via `subject`, carrying lock files, file manifests and similar. See [Slices](slices.md#metadata). |
| **Cake** | A `cake.yaml`: target ref, base, slices and config patch. See [cake.yaml](../reference/cake-yaml.md). |
| **Lock** | `cake.lock`, written by `compose`, pinning every input to a digest. See [Determinism & Locks](determinism-and-locks.md). |
| **Plan** | The per-blob classification — reuse, mount or copy — computed before pushing. See [Transfer Plan](transfer-plan.md). |
| **Project** | A directory with `cake-config.yaml` holding registry naming, pack defaults and several cakes. See [Projects](projects.md). |
| **diff_id** | The sha256 of an *uncompressed* layer tar. Layer identity for dedup and `diff`; the compressed *digest* is what is transferred. |

## Design principles

- **Compose, don't build.** No `RUN` steps, no daemon. Output is a
  standard OCI image any runtime can pull.
- **Reuse over transfer.** Blobs already in the target repository are
  reused; blobs elsewhere on the same registry are mounted server-side.
- **Deterministic.** Identical inputs produce identical digests.
- **Registry-native metadata.** Provenance lives in annotations and
  OCI 1.1 referrers, not in side databases.
- **Types are a pack-time concept.** `compose` only reads generic
  annotations, so images built with unknown or newer types still
  compose.

{{< cards >}}
  {{< card link="slices" title="Slices" icon="collection" >}}
  {{< card link="pack-types" title="Pack Types" icon="template" >}}
  {{< card link="composition" title="Composition" icon="view-grid" >}}
  {{< card link="transfer-plan" title="Transfer Plan" icon="switch-horizontal" >}}
  {{< card link="determinism-and-locks" title="Determinism & Locks" icon="lock-closed" >}}
  {{< card link="projects" title="Projects" icon="folder" >}}
  {{< card link="remote-models" title="Remote Models" icon="cloud-download" >}}
{{< /cards >}}
