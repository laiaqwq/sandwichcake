---
title: Testing
weight: 4
aliases:
  - /docs/testing/
---

How to verify the tree locally. The release gate is in
[Releasing](releasing.md#pre-release-checklist).

## Everyday checks

```sh
go build ./... && go vet ./... && go test -count=1 ./...
```

`-count=1` disables the test cache so the current tree is actually
exercised. The suite uses local `httptest` registries only — no
network access is needed. `internal/testutil` also provides
`UploadStatus` (resumable-upload status + lost-ack emulation) and the
`Hub` HF/ModelScope fake (302→CDN, pagination, corrupt/flaky knobs —
`internal/testutil/hub.go`).

A few tests need local tools beyond Go itself: `bash` (releasetest
runs the workflow's embedded script), the `go` toolchain (testutil's
ELF builder), and `git` (guarded by an `exec.LookPath` skip in
project_config_test.go).

Race-detector runs for the packages exercising shared state —
`cmd/sandwichcake`'s package-level flag vars, `internal/cache`'s
mutex + env-derived `Dir()` — and the `pkg/inspect` report pipeline:

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
go test -count=1 ./internal/releasetest
```

## Documentation checks

```sh
go test -count=1 ./internal/doctest
go test -count=1 -run TestCLIDocs ./cmd/sandwichcake
```

- `internal/doctest` walks the docs markdown and `README.md`, rejecting
  broken or non-`.md` body links (a Hextra requirement) and validating
  `#anchor` fragments.
- `TestCLIDocs` walks the cobra command tree and fails when a command
  has no page under `docs/content/docs/cli/`, when the page lacks the
  command's synopsis, or when a flag is not mentioned (see
  [Contributing to Docs](contributing-docs.md#cli-reference)).

The Hugo build itself is covered in
[Contributing to Docs](contributing-docs.md#build-and-preview).
