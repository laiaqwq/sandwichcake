---
title: Environment Variables
weight: 4
---

Environment variables override the user and local config files, but
not explicit flags. See [Configuration Files](cake-config.md) for the
corresponding config keys.

## sandwichcake

| Variable | Overrides | Description |
|----------|-----------|-------------|
| `SANDWICHCAKE_CACHE_DIR` | `cache.dir` | Metadata cache directory (`--cache-dir` still wins) |
| `SANDWICHCAKE_UPLOAD_CONCURRENCY` | `upload.concurrency` | Concurrent blob uploads (positive integer) |
| `SANDWICHCAKE_UPLOAD_RETRIES` | `upload.retries` | Upload retries (positive integer) |
| `SANDWICHCAKE_DOWNLOAD_CONCURRENCY` | `download.concurrency` | Remote model layers streamed in parallel |
| `SANDWICHCAKE_DOWNLOAD_RETRIES` | `download.retries` | Download retries per request |
| `SANDWICHCAKE_TEST_UV` | – | Test-only: `1` enables the `uv` integration test, see [Testing](../../developer-guide/testing.md#optional-integration-test) |

## Build reproducibility

| Variable | Description |
|----------|-------------|
| `SOURCE_DATE_EPOCH` | Unix seconds used for tar entry mtimes and the image config `created` time, for both `pack` and `compose`. Default `0`. Must be identical across runs for identical digests — see [Determinism & Locks](../concepts/determinism-and-locks.md) |

## Model sources

| Variable | Description |
|----------|-------------|
| `HF_ENDPOINT` | Hugging Face endpoint or mirror (overrides `download.hf.endpoint`) |
| `MODELSCOPE_DOMAIN` | ModelScope endpoint (overrides `download.modelscope.endpoint`); `https://` is prepended to a bare domain |
| `HF_TOKEN`, `HUGGING_FACE_HUB_TOKEN` | Hugging Face token |
| `HF_TOKEN_PATH`, `HF_HOME` | Location of the `huggingface-cli login` token file |
| `MODELSCOPE_API_TOKEN` | ModelScope token |
| `NETRC` | Path of the netrc file used for other HTTP(S) hosts (default `~/.netrc`) |

These are the same variables the `huggingface_hub` and `modelscope`
SDKs honor. Details: [Authentication](authentication.md#model-sources).

## Paths

| Variable | Description |
|----------|-------------|
| `XDG_CONFIG_HOME` | User config is `$XDG_CONFIG_HOME/sandwichcake/config.yaml` (default `~/.config/…`) |
| `XDG_CACHE_HOME` | Default cache is `$XDG_CACHE_HOME/sandwichcake` (default `~/.cache/…`); also used for the Hugging Face token file |
| `DOCKER_CONFIG` | Directory of the Docker `config.json` used for registry credentials |
