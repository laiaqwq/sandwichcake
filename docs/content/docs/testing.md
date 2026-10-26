---
title: Testing
weight: 8
---

How to verify the tree locally and what gates a release.

## Everyday checks

```sh
go build ./... && go vet ./... && go test -count=1 ./...
```

`-count=1` disables the test cache so the current tree is actually
exercised. The suite uses local `httptest` registries only — no
network access is needed.

Race-detector runs for the packages exercising globals and the report
pipeline:

```sh
go test -race -count=1 ./cmd/sandwichcake ./pkg/inspect ./internal/cache
```

## Offline mode

If module downloads are unavailable, pin the toolchain and proxy:

```sh
GOPROXY=off GOTOOLCHAIN=local go vet ./... && GOPROXY=off GOTOOLCHAIN=local go test -count=1 ./...
```

This fails loudly instead of fetching if a dependency is missing from
the module cache.

## Optional integration test

The `venv --from-lock` integration test is opt-in — it needs `uv` on
`PATH` and live PyPI access:

```sh
SANDWICHCAKE_TEST_UV=1 go test -count=1 ./pkg/types/venv
```

Without the variable it is skipped.

## Release workflow contract

The release workflow has its own regression tests — they parse
`.github/workflows/release.yml` and execute its embedded tag-validation
shell against real tag values:

```sh
go test -count=1 ./internal/releasetest ./internal/doctest
```

`internal/doctest` also walks the docs markdown and rejects broken or
non-`.md` body links (a Hextra requirement).

## Docs site

The Hugo docs build (requires `hugo` installed and the vendored module
cache populated):

```sh
cd docs && hugo --gc --minify --panicOnWarning -d <dest>
```

## Cross-build rehearsal

The same commands the release workflow runs, locally, using
`VERSION=<tag>`:

```sh
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X $(go list -m)/pkg/version.Version=$VERSION" \
  -o build/sandwichcake ./cmd/sandwichcake
```

for each of `darwin/arm64`, `linux/arm64`, `linux/amd64` (`GOOS`/`GOARCH`
per target), then `tar`/`sha256sum` packaging as in
`.github/workflows/release.yml`.

Note the limits of this rehearsal on macOS: cross-compiled Linux
binaries can be checked with `file` but only the `darwin/arm64` binary
can actually run — `sandwichcake version` must print the exact tag.
Linux binaries only truly run on Linux (CI runs on `ubuntu-latest`).
Release binaries are plain Go builds — the macOS one is ad-hoc signed,
not Developer-ID signed or notarized.

## Pre-release checklist

- [ ] `go build ./...` clean
- [ ] `go vet ./...` clean
- [ ] `go test -count=1 ./...` passes
- [ ] `go test -race -count=1 ./cmd/sandwichcake ./pkg/inspect ./internal/cache` passes
- [ ] `go test -count=1 ./internal/releasetest ./internal/doctest` passes
- [ ] `SANDWICHCAKE_TEST_UV=1` integration status noted (or consciously skipped)
- [ ] `hugo --gc --minify --panicOnWarning` docs build clean
- [ ] Cross-build + packaging + checksum rehearsal done for all three targets
- [ ] Native `sandwichcake version` on this platform prints the intended tag
- [ ] Tag commit reviewed — workflow, tests and docs included; no stray worktree edits
