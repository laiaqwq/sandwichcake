---
title: version
weight: 11
---

`sandwichcake version` prints the build version and exits. Read-only.

```text
$ sandwichcake version
v0.0.1
```

The version is stamped at build time via `-ldflags` — `make build` and
release builds set it (from `git describe --tags --always --dirty`, so
local builds may print `abc1234` or `v0.0.1-dirty` rather than a clean
tag); a plain `go build` / `go install` without the project's ldflags
prints the built-in default `dev`. Versioned binaries are on the
[releases page](../releases.md).

## Notes

- Trailing args are silently ignored, and there is no `--version`
  flag — only the subcommand.
- The same version string is written into each slice's
  `com.github.baba.sandwichcake.tool.version` layer annotation at pack
  time — on every layer, queryable in the raw manifest (e.g. via
  `crane manifest`); [`inspect`](inspect.md) does not display it.
