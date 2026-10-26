# sandwichcake

OCI image composition tool: packs local directories into **slices**
(OCI image manifests carrying annotated, typed layers) and composes
existing registry content into new images — no Dockerfiles, no build
daemon. See the [cake.yaml reference](docs/content/docs/cake-yaml.md) and
the [documentation site](https://sandwichcake.laiqwq.com/) for details.

## Install

Prebuilt binaries are published on the
[GitHub releases page](https://github.com/laiaqwq/sandwichcake/releases)
for:

- macOS on Apple Silicon (`darwin/arm64`)
- Linux on ARM64 (`linux/arm64`)
- Linux on x86_64 (`linux/amd64`)

Each archive contains a single `sandwichcake` executable plus a
`checksums.txt` — see the
[releases doc](docs/content/docs/releases.md) for verification and
install steps.

From source — requires Go as declared in [go.mod](go.mod) (currently
1.27.2):

```sh
git clone https://github.com/laiaqwq/sandwichcake
cd sandwichcake
make build                          # bin/sandwichcake, version stamped
make install PREFIX="$HOME/.local"  # installs plus a `swcake` symlink
```

`make build` stamps the version via `-ldflags`; a plain
`go install ./cmd/sandwichcake` builds fine but reports version `dev`.
`sandwichcake version` prints what a given binary was stamped with.

## Quickstart

```sh
# 1. pack a directory into a slice and push it
sandwichcake pack weight ./models/qwen3-8b \
  --root /models/qwen3-8b \
  --to registry.example.com/ai/weights:qwen3-8b \
  --source hf://Qwen/Qwen3-8B@main

# 2. describe the composition in cake.yaml (base + slices -> target)

# 3. preview the transfer plan
sandwichcake plan -f cake.yaml

# 4. compose and push the target image
sandwichcake compose -f cake.yaml
```

For multi-image projects, `sandwichcake init my-project` creates
`cake-config.yaml` and `cake/`. Set `registry.prefix` and per-type pack
defaults in the project config, add `cake/<name>.yaml`, then run
`sandwichcake compose <name>`. `sandwichcake config pull/push` syncs the
whole project via an `oci://` or `git+https://...@branch#subdir` source.
See the [config guide](docs/content/docs/commands/config.md) for details.

See [docs/content/docs/getting-started.md](docs/content/docs/getting-started.md)
for a full walkthrough and
[docs/content/docs/commands/](docs/content/docs/commands/) for the CLI
reference.

## Platform notes

`pack venv --from-lock` resolves and builds the environment with your
**local** `uv` and produces host-platform artifacts: a Linux-targeted
venv must be built on Linux — packing on macOS produces Mach-O binaries
that the ELF scanner ignores, so libc requirements go unrecorded and
the venv will not run in a Linux image.

## Development

```sh
go build ./... && go vet ./... && go test ./...
```

See [docs/content/docs/testing.md](docs/content/docs/testing.md) for
the full verification matrix, offline mode, and the release checklist.
