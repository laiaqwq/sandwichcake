---
title: sandwichcake
layout: hextra-home
---

{{< hextra/hero-container >}}
  {{< hextra/hero-headline >}}
    Compose OCI images from reusable slices
  {{< /hextra/hero-headline >}}

  {{< hextra/hero-subtitle >}}
    sandwichcake packs local directories into annotated OCI layer sets —
    "slices" — and composes them onto base images without Dockerfiles,
    without rebuilding what didn't change.
  {{< /hextra/hero-subtitle >}}

  <div class="hx-mt-6 hx-mb-6">
    {{< hextra/hero-button text="Get Started" link="docs" >}}
  </div>
{{< /hextra/hero-container >}}

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="Deterministic slices"
    subtitle="Identical inputs produce identical manifest digests. Tar output is reproducible; SOURCE_DATE_EPOCH controls timestamps."
    link="docs/slices"
  >}}
  {{< hextra/feature-card
    title="Typed packing"
    subtitle="Built-in types for venvs, model weights, binaries and patches — each with its own layout rules, annotations and checks."
    link="docs/types"
  >}}
  {{< hextra/feature-card
    title="Plan before push"
    subtitle="Terraform-style transfer plans show which blobs are reused, mounted or copied before anything moves."
    link="docs/commands/plan"
  >}}
  {{< hextra/feature-card
    title="Lock files"
    subtitle="compose --locked replays a pinned cake.lock for bit-identical reruns."
    link="docs/cake-yaml#cakelock"
  >}}
  {{< hextra/feature-card
    title="Registry-native"
    subtitle="Chunked resumable uploads, cross-repo blob mounts, OCI 1.1 referrers with fallback index for older registries."
    link="docs/slices#metadata"
  >}}
  {{< hextra/feature-card
    title="Single binary"
    subtitle="One Go binary. No daemon, no buildkit, no Docker required."
    link="docs/getting-started"
  >}}
{{< /hextra/feature-grid >}}
