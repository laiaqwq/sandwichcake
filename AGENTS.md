# sandwichcake

OCI image composition tool: packs local directories into "slices" (OCI image
manifests carrying annotated layers) and composes existing registry content
into new images. See DESIGN.md for the full spec.

## Build / test

```sh
go build ./... && go vet ./... && go test ./...
make build        # bin/sandwichcake with -ldflags version
make test install # install to $(PREFIX)/bin + swcake symlink
```

`SANDWICHCAKE_TEST_UV=1` enables the `BuildFromLock` integration test
(requires `uv` on PATH and network access to PyPI; it is skipped otherwise).

## Settled decisions (do not change casually)

- Module: `github.com/laiaqwq/sandwichcake`.
- Annotation namespace is `com.github.baba.sandwichcake.` — NOT
  `io.sandwichcake.` (DESIGN.md predates this). All keys are constants in
  `pkg/layer/annotations.go`; type-specific keys via `layer.TypeKey(type, k)`.
- Layer media types stay `application/vnd.sandwichcake.*` / standard OCI
  (`+gzip`, `+zstd`, uncompressed `tar`). cake.yaml apiVersion stays
  `sandwichcake.io/v1`; cake.lock kind is `CakeLock`.
- Default SOURCE_DATE_EPOCH: env var if set, else Unix 0 (slice.Epoch).
- Dependencies: github.com/google/go-containerregistry v0.22.1,
  github.com/klauspost/pgzip, github.com/klauspost/compress,
  gopkg.in/yaml.v3, github.com/spf13/cobra, golang.org/x/term.
- All registry network I/O goes through pkg/registry (chunked resumable
  uploads, cross-repo mount, cached transports, Classify for plan).
- Metadata artifacts: PUT by digest with `subject`; if the registry does not
  ack referrers (no `OCI-Subject` response header), maintain the OCI 1.1
  fallback index at tag `sha256-<subject hex>` via
  `Client.AddReferrer`/`RemoveReferrer`. `--artifact-compat` always uses the
  fallback index + standard image config type.
- Base libc resolution (resolves DESIGN §9.1): `libc:` on the base ref, else
  the `com.github.baba.sandwichcake.libc` manifest annotation or config
  label, else `--verify` scans base layer tars (`ld-musl-*`, `libc-X.Y.so`,
  then `libc.so.6` content "GNU C Library ... version X.Y"; later layers
  win). Unknown = "" (warns on unverifiable requires).
- Determinism: identical inputs must produce identical manifest digests,
  including `compose --locked` reruns — the lock stores the original tag ref
  (`input:`) so `org.opencontainers.image.base.name` stays stable.
- GOPROXY: this machine cannot reach proxy.golang.org; `go env -w` has set
  `https://goproxy.cn,direct`.

## Project config (IMPLEMENTED)

Implemented in cmd/sandwichcake/project_config.go, config.go,
config_sync.go, config_oci.go, config_git.go, and project_init.go.

### Layout and discovery

```
<project>/                  # project name = directory name
  cake-config.yaml          # project config (synced)
  cake-config.local.yaml    # machine-local overrides (NOT synced, gitignored)
  .cakeignore               # extra sync excludes (common gitignore globs)
  cake/<name>.yaml          # one Cake per image, existing cake.yaml format
  cake/<name>.lock          # its CakeLock, next to it
  .cake/state.json          # pull/push bookkeeping (NOT synced)
```

- Find the project root by walking up from cwd to the first
  `cake-config.yaml` (like git). `--project <dir>` overrides this.
- `compose <name>` resolves to `cake/<name>.yaml`; a path argument still works.
- Resolve relative paths in a config file against that file's directory,
  not against cwd.

### Precedence and field classes

Precedence for environment fields: flag > supported `SANDWICHCAKE_*` env >
`cake-config.local.yaml` > global `~/.config/sandwichcake/config.yaml` >
built-in defaults. Output fields: explicit flag > project config > built-in
defaults. Pack defaults merge per type (including split fields); environment
lists replace. `config show --origin` prints sources for configured
environment fields and groups project fields by project file. Project and
environment configs use separate Go structs and strict YAML fields in
camelCase.

Every field belongs to exactly one class, and the loader enforces it:

- Output-affecting (digest, tag or target): project-only, so it lives in
  `cake-config.yaml`. These fields are `registry.*`, `pack.*` (root,
  compression, level, split, owner, chmod) and `models.paths`. Setting one
  in the global or local file is a load error that tells the user to move
  it to `cake-config.yaml`. This applies to the existing global
  `toPrefix`, `tagFormat` and `roots`. `init --migrate` explicitly moves
  legacy global output fields into the new project and strips them from
  the global config.
- Environment: global or local only, never synced. These fields are
  `cache.dir`, `upload.*` (concurrency, retries) and `repos` (ls).
  Configured relative cache paths resolve against their config directory;
  `~` expands to the home directory. Credentials never go in any config
  file; read them from env or the docker credential store.

### cake-config.yaml schema

```yaml
apiVersion: sandwichcake.io/v1
kind: CakeConfig
from: oci://registry.example.com/sandwichcake/config:proj-a  # optional; pull source / push default
registry:
  prefix: registry.example.com/sandwichcake   # replaces toPrefix
  repos: {venv: venv, weight: weights, binary: bin, image: infer}  # by type; image = compose output
  repoFormat: "{prefix}/{repo}"       # optional, e.g. "{prefix}/{repo}/{name}"
  tagFormat: {default: "{name}", weight: "{name}-{rev}"}  # per type; no time-based vars
  onTagExists: fail                   # fail|overwrite|skip; unset = overwrite
pack:
  defaults: {owner: "0:0", chmod: "644:755"}
  venv:   {root: /opt/venv, compression: zstd, level: 3}
  weight: {root: /models, compression: none, split: {enabled: true, large: 5Gi, small: 64Mi}}
models:
  paths: {hf: "/models/{org}/{name}", modelscope: "/models/{org}/{name}"}  # default: no {rev}
```

Global or local only:

```yaml
cache: {dir: ~/.cache/sandwichcake}
upload: {concurrency: 4, retries: 5}
repos: [registry.example.com/sandwichcake/venv]
```

Project auto-naming uses `registry.repos` for all types (HF weight names
become `org--model`); the explicit `--to-prefix` flag retains the previous
Namer convention without a type repo. `models.paths` provides a default
weight container root for hf:// or modelscope:// provenance when no
`pack.weight.root` or `--root` is set; it does not download models.

### Short refs in cake.yaml (additive)

- `ref: venv:torch-2.4` (`<known-type-or-image>:<tag>`, with no `/`
  before the `:`) expands via `registry.prefix` + `registry.repos`.
  Unknown names like `python:3.12-slim` remain ordinary full refs.
- The lock stores the fully expanded ref in `input:`. Locks therefore do
  not depend on later project-config changes. The target and platform
  order are also pinned in the lock, so
  `org.opencontainers.image.base.name` stays stable.

### init

`sandwichcake init [dir]` scaffolds a project:

- writes `cake-config.yaml`, `cake/`, and a `.gitignore` containing
  `cake-config.local.yaml` and `.cake/`;
- never overwrites existing files.
- `--migrate` reads old `toPrefix`, `tagFormat`, `roots`, `cacheDir`
  from the global config, writes the new project and rewrites the
  global config with environment-only settings.

The global file is created by `config init --global` (behavior change:
the old `init` created the global config).

### config pull / push (sync the whole project)

The sync set is the project tree minus `cake-config.local.yaml`, `.cake/`,
`.git/`, `*.env` and `.cakeignore` matches. It includes `cake/*.yaml` and
`*.lock`. Symlinks are rejected, so they cannot expose files outside
the project.
`.cakeignore` supports `*`, `?`, `**`, root paths and ordered negations;
hard safety exclusions cannot be negated. The source/destination arg
(default `from`) is scheme-based:

- `oci://reg/repo:tag`: one OCI artifact, artifactType
  `application/vnd.sandwichcake.project.v1`, one layer per file with the
  path in an annotation. All I/O goes through pkg/registry.
- `git+https://host/repo.git@<branch>#<subdir>` and `git+ssh://...`:
  - Shell out to system `git`; do not add go-git.
  - If `git` is missing, fail with install hints (`xcode-select --install`,
    `brew install git`, `apt install git`).
  - Require a nonempty `#<subdir>` and work in a cached clone under the
    cache dir. The project dir itself does
    not need to be a git checkout.

pull:

- Copies the tree into the project and records the source digest or
  commit plus per-file sha256 in `.cake/state.json`.
- Refuses to overwrite locally modified files unless `--force`, and prints
  a diff first.

push:

- OCI: permits first push to an empty tag; otherwise refuses if the remote
  tag digest != the recorded digest, unless `--force`. This is an optimistic
  check (no registry-side atomic compare-and-swap).
- git modes:
  - default `update`: fetch, require the remote branch head == the
    recorded commit (otherwise "pull first"), update synced paths in
    `<subdir>`, commit,
    push to the same branch.
  - `--new-branch <name>`: commit on top of the recorded commit and push a
    new branch (for review/MR).
  - Never merge, never force-push.

### Model download (IMPLEMENTED)

`pack weight <hf://org/name[@rev] | modelscope://org/name[@rev] |
https://...>` streams remote model files straight into registry blobs —
cmd/sandwichcake/pack_remote.go, pkg/fetch (provider API + HTTP client)
and pkg/slice/stream.go (resumable tar→blob streams):

- native Go HTTP; endpoints via `download.hf.endpoint` /
  `download.modelscope.endpoint` or HF_ENDPOINT / MODELSCOPE_DOMAIN
  (hf-mirror works);
- tokens from env / CLI token files only (HF_TOKEN,
  HUGGING_FACE_HUB_TOKEN, huggingface-cli token; MODELSCOPE_API_TOKEN);
- ranged streams resume at the current offset after failures, with
  optional parallel segments (`download.connections` /
  `download.segmentSize`);
- `--include`/`--exclude` gitignore globs select repo files;
- the resolved upstream commit is recorded (`source-commit` annotation).
  There is no separate model cache dir; `--keep-dir` optionally tees
  files to disk.

## Layout

```
cmd/sandwichcake cobra CLI (init/config pull/push/pack/compose/plan/inspect/ls/discover/tag/diff/rm/types/version)
pkg/layer        deterministic tar, Scan/Split, compression, annotation keys
pkg/types        Type interface + registry, Opts/Source, CheckRequires, elfscan
pkg/types/<t>    built-in types: venv, weight, binary, patch, other (+ builtin)
pkg/registry     ggcr remote wrapper: chunked resumable upload, mount, referrers
pkg/fetch        remote model download: HF/ModelScope/plain-HTTP listing + ranged streams
pkg/slice        Pack flow (local + remote streaming) + slice manifest parse + metadata artifact push
pkg/cake         cake.yaml / cake.lock schemas + validation (no I/O beyond file r/w)
pkg/resolve      cake.Ref -> resolved layers (pure-file judgment, verify, libc)
pkg/compose      PURE: base + slices + config patch -> v1.Image / index
pkg/plan         per-blob reuse/mount/copy classification, terraform-style render
pkg/runner       orchestration: resolve -> compose -> plan -> confirm -> push -> lock
pkg/inspect      read/render reports, metadata file fetch, Diff
internal/cache   metadata-only JSON file cache (~/.cache/sandwichcake)
internal/testutil shared test helpers (fake registry w/ mount emulation, ELF builder)
internal/doctest docs link-check regression test (test-only)
internal/releasetest release workflow regression test (test-only)
```

## Notes for tests

- ggcr's fake registry shares blobs across repos and does not implement
  `mount=`; `testutil.MountRegistry` emulates mounts and tests force 404 on
  the destination HEAD to exercise per-repo behavior.
- `remote.MountableLayer` is a struct (`ml.Reference` field); compose never
  wraps layer objects so mount capability survives.

## Docs site (docs/)

Hugo + Hextra (Hugo module, pinned in docs/go.mod). GitHub Pages
deployment is planned but the workflow is not yet committed
(.github/workflows/ currently contains only release.yml).

- Preview: `cd docs && hugo server` (needs `brew install hugo` + Go on
  PATH for module resolution; local GOPROXY=goproxy.cn works for the
  hextra module fetch). Build: `cd docs && hugo --gc --minify` ->
  docs/public/ (gitignored).
- Relative body links MUST end in `.md` — hextra's render-link hook
  only resolves `[x](pack.md)`-style targets; bare `[x](pack)` renders
  as a raw relative URL and 404s. Card/shortcode `link` attrs are raw
  hrefs (browser-relative, fine).
- Custom heading anchors `{#id}` need
  `markup.goldmark.parser.attribute` (already on in docs/hugo.toml).
- Search: flexsearch bundle is vendored at
  docs/assets/js/flexsearch.bundle.min.js via
  `params.search.flexsearch.js` — this machine cannot reach
  cdn.jsdelivr.net; do not revert to CDN loading or local builds break.
- GitHub Pages: repo Settings -> Pages -> Source = GitHub Actions.
  CI overrides baseURL via configure-pages output; the `baseURL` in
  docs/hugo.toml assumes the project page `baba.github.io/sandwichcake/`.
- Sidebar order = `weight` in frontmatter; each section `_index.md`
  sets the section position.
- Content tree (docs/content/docs/): `getting-started/` (installation,
  quickstart, examples — examples are TODO), `general/concepts/`,
  `general/reference/`, `cli/` (one page per command; `cli/pack/<type>.md`
  per pack type; all `config` subcommands share `cli/config.md` with one
  H2 each), `developer-guide/`. Moved pages keep old URLs via
  frontmatter `aliases:`.
- CLI pages follow Synopsis → Arguments → Flags → What happens → Output
  → Examples → Notes. `TestCLIDocs` (cmd/sandwichcake/docs_test.go)
  fails if a cobra command lacks a page, its command path, or any flag
  mention (pack common flags may live in `cli/pack/_index.md`; global
  flags in `cli/_index.md`) — update docs with every new command/flag.
