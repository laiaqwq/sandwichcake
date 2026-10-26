---
title: init
weight: 13
---

`sandwichcake init [dir]` creates a project (default: current directory):

```sh
sandwichcake init my-project
# created my-project/cake-config.yaml
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

If you previously stored `toPrefix`, `tagFormat`, `roots`, or `cacheDir` in
the global config, use `sandwichcake init my-project --migrate` instead.
It moves output settings into the new project and rewrites the user file
with environment-only settings; it refuses to overwrite an existing
project config. Check the resulting weight repository/tag names: the
new project naming scheme uses `registry.repos.weight`.
