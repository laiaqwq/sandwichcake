---
title: init
weight: 13
---

`sandwichcake init [dir]` creates a project (default: current directory)
and prints the absolute config path:

```sh
sandwichcake init my-project
# created /abs/path/my-project/cake-config.yaml
```

This creates `cake/` for multiple `<name>.yaml` files and appends
`cake-config.local.yaml` and `.cake/` to `.gitignore`. An existing
`cake-config.yaml` is never overwritten. Existing `.gitignore` entries
are preserved.

Edit the project configuration before packing:

```yaml
apiVersion: sandwichcake.io/v1
kind: CakeConfig
registry:
  prefix: registry.example.com/team
  repos: {venv: venv, weight: weights, binary: bin, image: infer}
```

To create the optional **user-wide** environment defaults file, run
`sandwichcake config init --global`. It writes
`$XDG_CONFIG_HOME/sandwichcake/config.yaml` (or
`~/.config/sandwichcake/config.yaml`) without overwriting an existing file.
See [config](config.md) for project and global settings. Registry credentials
stay in the Docker credential chain, not in either configuration file.

If the global config still contains legacy `toPrefix`, `tagFormat`,
`roots`, or `cacheDir` keys, plain `sandwichcake init` **fails** — the
global config is parsed before scaffolding and the error points at
`sandwichcake init my-project --migrate`, which exists for exactly this
case. It moves output settings into the new project, rewrites the user
file with environment-only settings, and prints `migrated output
settings from <path>`; with no legacy keys it behaves like plain init.
It refuses to overwrite an existing project config. Legacy values are
validated first — each `roots.<type>` must be a valid pack type with an
absolute path and `tagFormat` may only use `{name}`/`{type}`/`{rev}` —
and a failure aborts the migration. Check the resulting weight
repository/tag names: the new project naming scheme uses
`registry.repos.weight`.
