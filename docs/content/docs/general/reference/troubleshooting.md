---
title: Troubleshooting
weight: 7
---

Common errors, what they mean and how to fix them. Add `-v` to most
commands for more detail.

## pack

**`--root required (or set pack.<type>.root in cake-config.yaml)`**
: Every pack needs a container root. Pass `--root /abs/path`, or set
  `pack.<type>.root` / `pack.defaults.root` in the project config. For
  `hf://`/`modelscope://` weights, `models.paths` can supply it.

**`slice: --platform linux/arm64 conflicts with detected arch amd64`**
: The ELF scanner found binaries for a different architecture than
  `--platform` says. Drop `--platform` to use the detected one, or pack
  the right directory.

**`venv: embedded mode requires a .python/ interpreter tree`**
: The venv uses an interpreter outside itself. Build it with an
  interpreter under `<venv>/.python` (e.g. `--from-lock`, or uv with
  `UV_PYTHON_INSTALL_DIR`), or pack with `--python system`.

**`venv: --from-lock requires `uv` on PATH`**
: Install [uv](https://docs.astral.sh/uv/). Build Linux venvs on a
  Linux host — a macOS-built venv does not run on Linux.

**`<ref> already exists (registry.onTagExists: fail)`**
: The project forbids overwriting tags. Pick a new tag, or change
  `registry.onTagExists`.

## compose and plan

**`compose: N bytes must be copied to the target registry; pass --yes or run interactively`**
: A non-interactive run needs explicit consent to upload. Add `--yes`
  (CI), or check with `plan` why blobs are not `reuse`/`mount` — slices
  on another registry are always copied.

**`resolve: <ref> is not a slice; select layers explicitly`**
: A `slices[]` entry points at an ordinary image. Add
  `layers: [<index or sha256 digest>, …]` to pick layers from it.

**`resolve: layer N of <ref> is not a pure file layer (history: "RUN …"); use force: true`**
: Only `COPY`/`ADD` layers (or sandwichcake layers) can be lifted out
  of another image safely. If you know the layer is self-contained, set
  `force: true` on that entry; adding `verify: true` also rejects
  whiteouts.

**`resolve: <ref> platform X does not match Y`**
: A ref has no manifest for a target platform. Set `target.platforms`,
  `platform:` on the ref (`any` for platform-independent slices), or
  pack the missing platform with `pack --merge`.

**`compose: layer roots "/a" (slice 1) and "/a/b" (slice 2) conflict`**
: Two slices would write into the same tree. Give them disjoint
  `--root`s, or pack the overriding one as `patch`.

**`warn: cannot verify glibc>=2.28: base libc unknown`**
: The base libc could not be determined, so the check was skipped. Set
  `base.libc: "glibc 2.36"` in cake.yaml, or run with `--verify` to
  scan the base. See [libc resolution](../concepts/composition.md#libc).

**`compose: glibc>=2.28: base has glibc 2.17`** / **`…: base uses musl libc`**
: The slice's binaries need a newer glibc than the base provides (or
  glibc at all). Use a newer base or rebuild the slice against an older
  glibc.

**`compose: layer sha256:… is zstd, incompatible with docker20`**
: `--compat docker20` is set and a slice uses zstd. Repack that slice
  with `--compression gzip`.

**`… rerun without --locked`**
: `--registry-prefix` (or a changed project prefix) would expand refs
  differently from what the lock pinned. Drop the override, or compose
  without `--locked` to write a new lock.

**The plan shows `copy` for blobs that exist on the registry**
: Mounting needs pull permission on the source repository and the same
  registry host. Check your credentials for the slice repositories.

## Projects and config

**`unknown or project-only config key "…" (use cake-config.yaml for output settings)`**
: `config set` only writes environment settings. Edit
  `cake-config.yaml` for `registry`, `pack` and `models`.

**`global config: … (for legacy output settings, use init --migrate)`**
: Your user config still has `toPrefix`, `tagFormat`, `roots` or
  `cacheDir`. Run `sandwichcake init <dir> --migrate`, see
  [`init`](../../cli/init.md#migrating-legacy-settings).

**`--registry-prefix requires a project (cake-config.yaml); use --to-prefix outside a project`**
: `--registry-prefix` overrides a project setting. Outside a project,
  use `pack --to` or `--to-prefix`.

**`locally modified <file>; use --force`** / **`source changed from X; use --force`**
: `config pull` refuses to clobber local edits or switch remotes
  silently. Review the printed changes, then rerun with `--force`.

**`remote tag changed (recorded A, current B); pull first or use --force`**
: Someone pushed the project since your last pull. `config pull`,
  reconcile, then push.

**`remote branch changed (…); pull first`**
: Same for git remotes. Pull first, or publish your version with
  `config push --new-branch <name>`.

**`git not found; install with xcode-select --install, brew install git, or apt install git`**
: Git-mode sync shells out to the system `git`.

## Registries

**401/403 errors**
: Credentials come from the Docker credential chain; run
  `docker login <registry>`. See [Authentication](authentication.md).

**`http: server gave HTTP response to HTTPS client`**
: The registry is plain HTTP. Pass `--insecure-registry <host>`.

**`discover` prints nothing**
: The registry supports neither `_catalog` nor GAR children. Configure
  `repos` and use `ls` instead.

**Interrupted uploads**
: Rerun the same command; chunked uploads resume from the registry's
  acknowledged offset. If the registry discarded the session, the
  upload restarts. Deleting the cache dir discards resume state.
