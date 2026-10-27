---
title: Remote Models
weight: 7
---

`pack weight` can take a Hugging Face, ModelScope or plain HTTP(S)
source instead of a local directory. Files are streamed from the
upstream straight into registry blobs — nothing large is staged on
disk, and there is no separate model cache. CLI and accepted source
forms: [`pack weight`](../../cli/pack/weight.md#remote-sources).

```sh
sandwichcake pack weight hf://Qwen/Qwen3-8B@main \
  --exclude 'original/**' \
  --to registry.example.com/ai/weights:qwen3-8b
```

## How streaming works

1. **List** — the provider API lists the repo files at the requested
   revision and resolves it to a commit. Small non-LFS files
   (configs, tokenizers) are fetched first.
2. **Plan layers** — the deterministic tar layout of every layer is
   computed from the file list up front, using the same splitting rules
   as a local pack.
3. **Stream** — each layer's tar is generated on the fly while files
   download; the blob is sent with chunked OCI uploads and its digest is
   computed as it goes.
4. **Verify** — every file is checked against the upstream sha256 (LFS)
   or git blob id while it streams. A mismatch cancels the upload
   session; nothing is committed.

Downloads and uploads overlap. Memory use is bounded by
`download.concurrency` × (`download.connections` ×
`download.segmentSize` + one upload chunk).

## Resume

An interrupted run continues where it stopped when rerun:

- downloads resume with HTTP range requests;
- uncompressed layers (the weight default) resume their upload at the
  last acknowledged chunk, even in a new process, using hash state
  saved in the metadata cache — compressed layers restart from zero;
- layers that already exist in the registry are skipped without
  downloading.

Large files can be fetched with several parallel ranged requests
(`download.connections`, `download.segmentSize`).

## Provenance and naming

The slice is named and annotated exactly like a local pack with
`--source hf://org/name@<rev>`: the revision you wrote goes into
`{rev}` and the tag, while the resolved commit is recorded in the
`weight.source.commit` annotation. In a project, `models.paths`
supplies the default `--root` (see [Projects](projects.md#naming)).

`--keep-dir <dir>` also writes the verified files to disk. Packing that
directory locally — named like the model, with the same `--source` and
`--source-commit` — reproduces the same digests.

## Filtering

`--include` / `--exclude` take gitignore-style globs on repo paths and
replace `pack.weight.include` / `pack.weight.exclude` from
`cake-config.yaml`. Without includes, every file is packed.

## Endpoints, mirrors and tokens

| Setting | Config key | Environment |
|---------|-----------|-------------|
| Hugging Face endpoint (e.g. `https://hf-mirror.com`) | `download.hf.endpoint` | `HF_ENDPOINT` |
| ModelScope endpoint | `download.modelscope.endpoint` | `MODELSCOPE_DOMAIN` |
| Hugging Face token | – | `HF_TOKEN`, `HUGGING_FACE_HUB_TOKEN`, or the `huggingface-cli login` token file |
| ModelScope token | – | `MODELSCOPE_API_TOKEN` |
| Other HTTP hosts | – | `$NETRC` / `~/.netrc` |

URLs on the configured mirror host are recognized as Hugging Face URLs.
Only models are supported, not datasets or spaces. See
[Authentication](../reference/authentication.md#model-sources) and
[Environment Variables](../reference/environment-variables.md).
