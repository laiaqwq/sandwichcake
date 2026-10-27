---
title: Getting Started
weight: 1
---

sandwichcake builds container images by **composition** instead of by
running build steps. You pack each independently versioned piece — a
Python virtualenv, a set of model weights, a directory of binaries —
into its own *slice* once, push it to a registry, and then declare in a
`cake.yaml` which base image and slices make up the final image.
`compose` stitches the existing blobs together and pushes the result,
moving only the bytes the target repository does not already have.

## When it fits

- Images made of a few large, slow-changing parts (model weights,
  CUDA/PyTorch environments) plus small fast-changing ones.
- Many images sharing the same parts: a weight slice is uploaded once
  and cross-repo mounted everywhere else.
- Builds that must be reproducible and reviewable: digests are
  deterministic, `plan` shows every transfer before it happens and
  `cake.lock` pins every input.

## Compared to a Dockerfile

| | Dockerfile | sandwichcake |
|---|---|---|
| Unit of reuse | Build cache, local to a builder | Slices — ordinary registry manifests |
| Changing one part | Rebuilds every later layer | Replaces only that slice's layers |
| Needs | Docker daemon / BuildKit | One Go binary and registry access |
| Runs commands at build time | Yes (`RUN`) | No — content is packed from directories |

sandwichcake does not run code inside the image. Anything that needs a
`RUN` step (apt packages, compiling) belongs in the base image or in a
directory you pack.

{{< cards >}}
  {{< card link="installation" title="Installation" icon="download" >}}
  {{< card link="quickstart" title="Quickstart" icon="play" >}}
  {{< card link="examples" title="Examples" icon="collection" >}}
{{< /cards >}}
