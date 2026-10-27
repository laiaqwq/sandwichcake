---
title: types
weight: 12
aliases:
  - /docs/commands/types/
---

List every registered pack type with its schema version, default
compression and type-specific options. Read-only — it reflects the
types compiled into the binary.

## Synopsis

```text
sandwichcake types
```

## Flags

No command-specific flags; see the [global flags](_index.md#global-flags).

## Output

```text
$ sandwichcake types
binary (schema 1, default compression gzip)
other (schema 1, default compression gzip)
patch (schema 1, default compression gzip)
  --base         expected base repo@digest this patch applies to (optional) (default "")
venv (schema 1, default compression zstd)
  --python       embedded|system: whether the venv carries its own interpreter (default "embedded")
  --lock         path to a lock file (uv.lock, poetry.lock, requirements.txt) to attach as metadata (default "")
weight (schema 1, default compression none)
  --source       provenance, e.g. hf://org/name@rev, https://, s3:// (default "")
  --source-commit resolved upstream commit of --source (set automatically for remote sources) (default "")
```

Each type appears as `<name> (schema <n>, default compression <alg>)`,
followed by one indented line per type-specific option. Flags added by
the CLI itself (`venv --from-lock`, `weight --include/--exclude/--keep-dir`)
are not listed here; see each [`pack`](pack/_index.md) subcommand.

## Notes

- The schema number is recorded in each layer's
  `com.github.baba.sandwichcake.layer.schema` annotation.
- What each type does: [Pack Types](../general/concepts/pack-types.md).
