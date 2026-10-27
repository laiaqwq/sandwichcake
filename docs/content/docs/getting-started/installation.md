---
title: Installation
weight: 1
aliases:
  - /docs/releases/
---

sandwichcake is a single Go binary. Install a prebuilt release or build
it from source.

## Prebuilt binaries

Prebuilt binaries are published on the
[GitHub releases page](https://github.com/laiaqwq/sandwichcake/releases)
for these targets:

- macOS on Apple Silicon (`darwin/arm64`)
- Linux on ARM64 (`linux/arm64`)
- Linux on x86_64 (`linux/amd64`)

### Assets

Each release contains one archive per target plus a checksum file:

```text
sandwichcake_<version>_<os>_<arch>.tar.gz
checksums.txt
```

for example `sandwichcake_v0.0.1_darwin_arm64.tar.gz`. Every archive
holds a single executable `sandwichcake` at its root; `checksums.txt`
lists the SHA-256 of each archive by basename.

### Verify and install

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

### macOS Gatekeeper

The macOS binary gets the usual ad-hoc signature of a Go build, but it
is **not** Developer-ID signed or notarized. Gatekeeper may warn on
first run; if you trust the release, allow it under System Settings ->
Privacy & Security.

## Build from source

Requires Go matching `go.mod` (currently 1.27.2):

```sh
git clone https://github.com/laiaqwq/sandwichcake
cd sandwichcake
make build                              # produces bin/sandwichcake with version stamped in
make install PREFIX="$HOME/.local"      # installs to $HOME/.local/bin plus a `swcake` symlink
```

The rootless `make install` avoids recompiling as root; make sure
`$HOME/.local/bin` is on your `PATH`. For a system-wide install use
`sudo make install` (which builds and installs as root).

Or without the Makefile (the version then prints as `dev`):

```sh
go install ./cmd/sandwichcake
```

See [Building](../developer-guide/building.md) for build details.

## Check the installation

```sh
sandwichcake version
sandwichcake types      # lists the compiled-in pack types
```

## Prerequisites

- **A registry you can push to** for `pack`, `compose`, `tag` and
  `rm`; `plan`, `inspect`, `diff`, `ls` and `discover` only need read
  access. Credentials come from the Docker credential chain — run
  `docker login` (or configure your registry's credential helper)
  first. See [Authentication](../general/reference/authentication.md).
- **Optional tools**, only for the flows that shell out:
  - [`uv`](https://docs.astral.sh/uv/) for `pack venv --from-lock`;
  - `git` for git-mode `config pull`/`push`.

No Docker daemon or BuildKit is needed.

## Shell completion

```sh
sandwichcake completion zsh > "$(brew --prefix)/share/zsh/site-functions/_sandwichcake"
```

Other shells: see [`completion`](../cli/completion.md).
