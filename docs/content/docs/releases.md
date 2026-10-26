---
title: Releases
weight: 7
---

Prebuilt binaries are published on the
[GitHub releases page](https://github.com/laiaqwq/sandwichcake/releases)
for these targets:

- macOS on Apple Silicon (`darwin/arm64`)
- Linux on ARM64 (`linux/arm64`)
- Linux on x86_64 (`linux/amd64`)

## Assets

Each release contains one archive per target plus a checksum file:

```text
sandwichcake_<version>_<os>_<arch>.tar.gz
checksums.txt
```

for example `sandwichcake_v0.0.1_darwin_arm64.tar.gz`. Every archive
holds a single executable `sandwichcake` at its root; `checksums.txt`
lists the SHA-256 of each archive by basename.

## Install

Download the archive for your platform plus `checksums.txt` into the
same directory, then verify just your archive.

Linux:

```sh
archive=sandwichcake_v0.0.1_linux_amd64.tar.gz   # or linux_arm64
grep -F "  $archive" checksums.txt | sha256sum -c -
```

macOS:

```sh
archive=sandwichcake_v0.0.1_darwin_arm64.tar.gz
grep -F "  $archive" checksums.txt | shasum -a 256 -c -
```

Then extract and install without root:

```sh
tar -xzf "$archive"
mkdir -p "$HOME/.local/bin"
install -m 755 sandwichcake "$HOME/.local/bin/sandwichcake"
ln -sf sandwichcake "$HOME/.local/bin/swcake"   # optional short alias
```

Make sure `$HOME/.local/bin` is on your `PATH`. For a system-wide
install use `sudo install -m 755 sandwichcake /usr/local/bin/` and an
equivalent `sudo ln -sf` for the alias.

sandwichcake requires a registry you can push to (via the standard
Docker credential chain) for `pack`/`compose`; `plan` and `inspect`
only need read access. See [Getting Started](getting-started.md) for
auth details.

### macOS Gatekeeper

The macOS binary gets the usual ad-hoc signature of a Go build, but it
is **not** Developer-ID signed or notarized. Gatekeeper may warn on
first run; if you trust the release, allow it under System Settings ->
Privacy & Security.

## Maintainers: cutting a release

Releases are built by `.github/workflows/release.yml`, which runs only
on version tags pushed to the repository:

```sh
git tag -a v0.0.1 -m "Release v0.0.1" <reviewed-commit>
git push origin <branch> v0.0.1
```

Checklist before tagging:

- Pick a **reviewed commit** — the tag must point at a commit whose
  contents were actually reviewed, including the workflow, tests and
  docs, not a worktree with unrelated uncommitted edits folded in.
- Run the full gate from [Testing](testing.md): `go build ./...`,
  `go vet ./...`, `go test -count=1 ./...`, the targeted `-race` runs,
  and the three-target packaging + native version/checksum rehearsal.
- Note the status of the opt-in `SANDWICHCAKE_TEST_UV=1` integration
  test (requires `uv` and PyPI access).
- Confirm you are tagging on the correct repository and that Actions
  has permission to create releases (the workflow uses the built-in
  `GITHUB_TOKEN`; the release job alone is granted `contents: write`,
  no extra secrets).
- Tag with an annotated tag (`-a`) and push only after explicit
  approval.

Workflow details:

- The tag must be strict SemVer with a `v` prefix
  (`vMAJOR.MINOR.PATCH`, optional `-prerelease` and/or `+build`
  metadata); the workflow rejects anything else before building.
- `go vet ./...` and `go test -count=1 ./...` gate the build — a
  failing test means no assets are published.
- Tags with a `-` before any `+build` suffix (for example
  `v0.2.0-rc.1` or `v0.2.0-rc.1+build.3`) are marked as pre-releases on
  GitHub automatically; `+build` metadata alone does not make a
  pre-release.
- Published release assets should be treated as **immutable**: a
  successful release's content must not be silently replaced.
  Re-running the workflow on the same tag (via **Re-run all jobs** in
  the Actions tab — re-pushing an unchanged tag does not emit a new
  push event) is a retry for a failed or incomplete delivery: the
  release action updates the existing release and its assets rather
  than creating a second one.
