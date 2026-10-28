---
title: Examples
weight: 3
---

## End-to-end examples

- [Compose two ModelScope models onto a vLLM base](modelscope-vllm.md) —
  stream complete model weights into separate slices, plan the transfer
  and compose an inference image.

For a shorter walkthrough, see the [Quickstart](../quickstart.md) or
the examples on each [CLI Reference](../../cli/_index.md) page.

More scenarios planned:

- An LLM inference image: Python base + venv built from `uv.lock` +
  weights streamed from `hf://`, in project mode.
- A team project workflow: `init`, several `cake/*.yaml` with short
  refs, and sharing the project with `config push`/`pull` over OCI or
  git.
- Multi-architecture slices with `pack --merge` and `target.platforms`.
- Reusing layers from an existing image (`layers`, `verify`, `force`).
- Release promotion: `compose --locked`, `tag`, `diff` and `rm`.
- Mirrors and private registries: hf-mirror, ModelScope,
  `--insecure-registry`.
