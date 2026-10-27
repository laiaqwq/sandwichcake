---
title: Examples
weight: 3
---

{{< callout type="info" >}}
**TODO** — end-to-end examples are coming.
{{< /callout >}}

Until then, see the [Quickstart](../quickstart.md) and the examples on
each [CLI Reference](../../cli/_index.md) page.

Planned scenarios:

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
