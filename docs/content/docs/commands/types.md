---
title: types
weight: 10
---

`sandwichcake types` lists every registered pack type with its schema
version, default compression, and type-specific options. Read-only —
it only reflects the types compiled into the binary.

## What it shows

```text
$ sandwichcake types
binary (schema 1, default compression gzip)
other (schema 1, default compression gzip)
patch (schema 1, default compression gzip)
  --base         expected base repo@digest this patch applies to (optional) (default "")
venv (schema 1, default compression zstd)
  --lock         path to a lock file (uv.lock, poetry.lock, requirements.txt) to attach as metadata (default "")
  --python       embedded|system: whether the venv carries its own interpreter (default "embedded")
weight (schema 1, default compression none)
  --source       provenance, e.g. hf://org/name@rev, https://, s3:// (default "")
```

Each type appears as `<name> (schema <n>, default compression <alg>)`,
followed by one indented line per type-specific option with its default.

## Notes

- The same options are accepted as flags by `pack <type>` — see
  [`pack <type> --help`](pack.md) and the [Pack Types](../types.md)
  page for what they mean.
- The schema number is recorded in each layer's
  `com.github.baba.sandwichcake.layer.schema` annotation at pack time.
