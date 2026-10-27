---
title: Releasing
weight: 7
---

Releases are built by `.github/workflows/release.yml`, which runs only
on version tags pushed to the repository. User-facing install steps
are in [Installation](../getting-started/installation.md).

## Cutting a release

```sh
git tag -a v0.0.1 -m "Release v0.0.1" <reviewed-commit>
git push origin <branch> v0.0.1
```

Before tagging:

- Pick a **reviewed commit** — the tag must point at a commit whose
  contents were actually reviewed, including the workflow, tests and
  docs, not a worktree with unrelated uncommitted edits folded in.
- Run the [pre-release checklist](#pre-release-checklist).
- Note the status of the opt-in `SANDWICHCAKE_TEST_UV=1` integration
  test (requires `uv` and PyPI access).
- Confirm you are tagging on the correct repository and that Actions
  has permission to create releases (the workflow uses the built-in
  `GITHUB_TOKEN`; the release job alone is granted `contents: write`,
  no extra secrets).
- Tag with an annotated tag (`-a`) and push only after explicit
  approval.

## Workflow details

- The tag must be strict SemVer with a `v` prefix
  (`vMAJOR.MINOR.PATCH`, optional `-prerelease` and/or `+build`
  metadata); the workflow rejects anything else before building.
- `go vet ./...` and `go test -count=1 ./...` gate the build — a
  failing test means no assets are published.
- Targets: `darwin/arm64`, `linux/arm64`, `linux/amd64`. Each produces
  `sandwichcake_<version>_<os>_<arch>.tar.gz` with a single
  `sandwichcake` binary; `checksums.txt` lists SHA-256 per archive.
- Releases get GitHub's auto-generated release notes on top of the
  uploaded assets (`generate_release_notes: true`).
- Tags with a `-` before any `+build` suffix (for example
  `v0.2.0-rc.1` or `v0.2.0-rc.1+build.3`) are marked as pre-releases
  automatically; `+build` metadata alone does not make a pre-release.
- Published release assets should be treated as **immutable**: a
  successful release's content must not be silently replaced.
  Re-running the workflow on the same tag (via **Re-run all jobs** in
  the Actions tab — re-pushing an unchanged tag does not emit a new
  push event) is a retry for a failed or incomplete delivery: the
  release action updates the existing release and its assets rather
  than creating a second one.

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

## Pre-release checklist {#pre-release-checklist}

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
