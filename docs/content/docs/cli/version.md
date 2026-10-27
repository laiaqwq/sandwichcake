---
title: version
weight: 14
aliases:
  - /docs/commands/version/
---

Print the build version and exit. Read-only.

## Synopsis

```text
sandwichcake version
```

## Flags

No command-specific flags; see the [global flags](_index.md#global-flags).
There is no `--version` flag — only the subcommand.

## Output

```text
$ sandwichcake version
v0.0.1
```

The version is stamped at build time via `-ldflags`. `make build` and
release builds set it — from `git describe --tags --always --dirty`, so
local builds may print `abc1234` or `v0.0.1-dirty`; a plain
`go build`/`go install` prints `dev`. See
[Building](../developer-guide/building.md).

## Notes

- Trailing arguments are ignored.
- The same string is written into every packed layer's
  `com.github.baba.sandwichcake.tool.version` annotation, so slice
  digests are stable only within one version.
