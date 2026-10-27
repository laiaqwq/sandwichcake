---
title: Contributing to Docs
weight: 6
---

The site is [Hugo](https://gohugo.io/) with the
[Hextra](https://imfing.github.io/hextra/) theme, imported as a Hugo
module pinned in `docs/go.mod`. Content lives in `docs/content/`.

## Build and preview {#build-and-preview}

Requires `hugo` (extended) and Go on `PATH` (Hugo resolves the theme
module through Go; a `GOPROXY` mirror such as `goproxy.cn` works).

```sh
cd docs
hugo server                                     # live preview
hugo --gc --minify --panicOnWarning -d <dest>   # build; default output docs/public/ is gitignored
```

The flexsearch search bundle is vendored at
`docs/assets/js/flexsearch.bundle.min.js` (`params.search.flexsearch.js`
in `docs/hugo.toml`) so builds do not depend on a CDN. Do not switch
it back to CDN loading.

## Structure

```text
docs/content/docs/
  getting-started/        Installation, Quickstart, Examples
  general/concepts/       How things work
  general/reference/      File formats, auth, env, annotations, compatibility, troubleshooting
  cli/                    One page per command; cli/pack/ one page per type
  developer-guide/        This section
```

- Sidebar order is the `weight` frontmatter; each section's
  `_index.md` sets the section's position.
- When moving a page, add its old URL to `aliases:` in the frontmatter
  so existing links keep working.
- Put explanations in **General**, exact syntax in **CLI Reference**,
  and link between them instead of duplicating text.

## Links

- Relative body link targets **must** end in `.md` — Hextra's
  render-link hook only resolves targets like `pack.md`; a bare `pack`
  target renders as a raw relative URL and 404s. Section indexes are
  linked as `dir/_index.md`.
- Shortcode attributes (`{{</* card link="…" */>}}`) are raw hrefs,
  relative to the page URL, without `.md`.
- Custom heading anchors use `{#id}` (enabled via
  `markup.goldmark.parser.attribute` in `docs/hugo.toml`).

`go test ./internal/doctest` checks every body link and `#anchor` in
the docs and `README.md`.

## CLI reference {#cli-reference}

Every command page follows the same template:

```text
Synopsis → Arguments → Flags → What happens → Output → Examples → Notes / See also
```

`TestCLIDocs` in `cmd/sandwichcake` keeps the pages in sync with the
code. For every command in the cobra tree it requires:

- a page: `cli/<command>.md`, `cli/<group>/_index.md` for command
  groups, `cli/pack/<type>.md` for pack types; `config` subcommands
  share `cli/config.md`;
- the command path (e.g. `sandwichcake config push`) somewhere on the
  page;
- every local flag as `--name` on the page — for pack subcommands the
  common flags may be documented on `cli/pack/_index.md` instead;
- every global flag on `cli/_index.md`.

When you add a command or flag, update the page in the same change.

## Style

- English, present tense, second person.
- Show real output from the current binary; keep digests abbreviated
  (`sha256:3a1f…`).
- Use `registry.example.com` for example registries.
