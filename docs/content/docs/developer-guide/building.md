---
title: Building
weight: 3
---

## Requirements

- Go matching `go.mod` (currently 1.27.2).
- `make` for the convenience targets (optional).

## Make targets

| Target | Does |
|--------|------|
| `make build` | `go build` with the version stamped in → `bin/sandwichcake` |
| `make test` | `go test ./...` |
| `make vet` | `go vet ./...` |
| `make install` | `build`, then install to `$(DESTDIR)$(PREFIX)/bin` (default `PREFIX=/usr/local`) plus a `swcake` symlink |

```sh
make build
make install PREFIX="$HOME/.local"    # rootless
```

## Version stamping

The version string lives in `pkg/version.Version` (default `dev`) and is
set with `-ldflags`:

```sh
go build -ldflags "-X github.com/laiaqwq/sandwichcake/pkg/version.Version=v0.1.0" \
  -o bin/sandwichcake ./cmd/sandwichcake
```

`make build` uses `git describe --tags --always --dirty` (override with
`VERSION=…`). Because every packed layer records the version, builds
with different version strings produce different slice digests.

Release builds additionally use `CGO_ENABLED=0 -trimpath -ldflags "-s -w …"`;
see [Releasing](releasing.md).

## Module proxy

If `proxy.golang.org` is unreachable, point Go at a mirror:

```sh
go env -w GOPROXY=https://goproxy.cn,direct
```

To build strictly from the module cache (fails instead of fetching):

```sh
GOPROXY=off GOTOOLCHAIN=local go build ./...
```

## Quick check

```sh
go build ./... && go vet ./... && go test ./...
```

Full test guidance: [Testing](testing.md).
