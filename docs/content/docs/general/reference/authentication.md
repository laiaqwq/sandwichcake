---
title: Authentication
weight: 3
---

sandwichcake has no login command and never stores credentials in its
own config files, annotations, locks, caches or logs.

## Registries {#registries}

Registry credentials come from the standard **Docker credential
chain**: `~/.docker/config.json` (or `$DOCKER_CONFIG/config.json`) and
the credential helpers configured there (`credsStore`,
`credHelpers`). Authenticate with `docker login` or your registry's
helper first:

```sh
docker login registry.example.com
```

Anonymous access is used when no credential matches — enough for
public pulls, `plan` against public images and similar read-only
commands.

Required permissions:

| Command | Needs |
|---------|-------|
| `plan`, `inspect`, `diff`, `ls`, `discover` | pull |
| `pack`, `compose`, `tag` | push on the target repo; pull on source repos (cross-repo mount needs pull on the repo the blob is mounted from) |
| `rm` | delete |
| `config push` / `pull` (OCI) | push / pull on the project repo |

### Plain-HTTP registries

```sh
sandwichcake --insecure-registry localhost:5000 compose -f cake.yaml
```

The flag is a global switch: any non-empty value makes **all**
registry traffic in that invocation plain HTTP, whatever host you
name. It is repeatable only for readability.

## Model sources {#model-sources}

Remote model sources (`pack weight hf://…`, `modelscope://…`,
`https://…`) read tokens from the environment or the upstream CLI's
token file only:

| Source | Token lookup order |
|--------|--------------------|
| Hugging Face (and mirrors) | `HF_TOKEN`, `HUGGING_FACE_HUB_TOKEN`, then the `huggingface-cli login` token file: `$HF_TOKEN_PATH`, else `$HF_HOME/token`, else `$XDG_CACHE_HOME/huggingface/token` or `~/.cache/huggingface/token` |
| ModelScope | `MODELSCOPE_API_TOKEN` |
| Other HTTP(S) hosts | `$NETRC`, else `~/.netrc` (`machine`, then `default` entry) |

Tokens are only needed for gated or private models. They are dropped
when a request is redirected to another host (for example to a CDN).

## git remotes

Git-mode `config pull`/`push` shells out to the system `git`, so it
uses whatever git is configured with: SSH keys and agent for
`git+ssh://`, credential helpers for `git+https://`. Configure your git
commit identity (`user.name`, `user.email`) before pushing.
