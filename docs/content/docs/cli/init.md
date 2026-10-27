---
title: init
weight: 1
aliases:
  - /docs/commands/init/
---

Create a project without overwriting existing files.

## Synopsis

```text
sandwichcake init [dir] [--migrate]
```

## Arguments

| Argument | Description |
|----------|-------------|
| `dir` | Project directory, created if missing. Default: the current directory |

## Flags

| Flag | Description |
|------|-------------|
| `--migrate` | Move legacy output settings from the user config into the new project (see [below](#migrating-legacy-settings)) |

Plus the [global flags](_index.md#global-flags).

## What happens

1. Creates `<dir>/cake/` for `<name>.yaml` cakes.
2. Appends `cake-config.local.yaml` and `.cake/` to `<dir>/.gitignore`
   (existing entries are preserved, duplicates are not added).
3. Writes `<dir>/cake-config.yaml`:

   ```yaml
   apiVersion: sandwichcake.io/v1
   kind: CakeConfig
   registry:
     prefix: ""
     repos: {venv: venv, weight: weights, binary: bin, image: infer}
   ```

An existing `cake-config.yaml` (even a dangling symlink) is never
overwritten — the command fails instead.

## Output

```text
created /abs/path/my-project/cake-config.yaml
```

## Examples

```sh
sandwichcake init my-project
cd my-project
# set registry.prefix and pack roots in cake-config.yaml
```

Then set `registry.prefix` and any `pack.<type>.root` — see
[Configuration Files](../general/reference/cake-config.md#project-config).
To create the optional user-wide environment config, run
[`config init --global`](config.md#config-init).

## Migrating legacy settings {#migrating-legacy-settings}

Older versions kept output settings (`toPrefix`, `tagFormat`, `roots`,
`cacheDir`) in the user config. Those keys are now rejected there, so
plain `init` **fails** with
`global config: … (for legacy output settings, use init --migrate)`.

`sandwichcake init <dir> --migrate`:

- validates the legacy values first — each `roots.<type>` must be a
  valid pack type with an absolute path, and `tagFormat` may only use
  `{name}`/`{type}`/`{rev}`; any failure aborts the migration;
- writes the new project with `toPrefix` → `registry.prefix`,
  `tagFormat` → `registry.tagFormat`, `roots.<type>` →
  `pack.<type>.root`;
- rewrites the user config with environment-only settings
  (`cacheDir` → `cache.dir`);
- prints `migrated output settings from <path>`.

With no legacy keys it behaves like plain `init`. It also refuses to
overwrite an existing project config. Check the resulting weight
repository/tag names afterwards: the project naming scheme uses
`registry.repos.weight`.

## See also

- [Projects](../general/concepts/projects.md)
- [`config`](config.md)
