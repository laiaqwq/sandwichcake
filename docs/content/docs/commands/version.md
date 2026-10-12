---
title: version
weight: 11
---

`sandwichcake version` prints the build version and exits. Read-only.

```text
$ sandwichcake version
v0.3.0
```

The version is stamped at build time via `-ldflags` — `make build` and
release builds set it; a plain `go build` / `go install` without the
project's ldflags prints the built-in default.

## Notes

- The same version string is written into each slice's
  `com.github.baba.sandwichcake.tool.version` layer annotation at pack
  time, so [`inspect`](inspect.md) shows which tool built a slice.
