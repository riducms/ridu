# Ridu website

The public, consumer-facing home, documentation, guides, and API reference for Ridu. Repository
architecture and maintainer documentation remain as plain Markdown under `docs/`.

## Develop

This package has its own Bun lockfile and sits outside the framework's root Bun workspace.

```sh
bun install --cwd website
bun run --cwd website dev
bun run --cwd website verify
```

`verify` checks formatting and types, runs focused unit tests, builds all static routes, validates
links and anchors, checks the lazy search catalog, and guards the total generated HTML size.

## Source map

- `src/content/docs/` and `src/content/guides/` contain ordinary consumer documentation as
  schema-validated Markdown.
- `src/config/documentation.ts` derives routes, navigation, pagination, outlines, and search records
  from those collections. The interactive Project Structure guide is merged into the same model.
- `src/reference/authoring/` contains the reviewed module registry, stable-ID editorial overlays,
  CLI records, and route lock. `src/reference/generated/catalog.json` is deterministically extracted
  from the public Go, TypeScript, and Svelte sources; `bun run reference:check` rejects drift.
- `src/features/project-structure/` owns the interactive guide's data, components, controllers,
  validation, and bespoke diagram CSS.
- `src/components/` is grouped by owning surface: `chrome`, `code`, `docs`, and `reference`.
- `src/styles/` is limited to tokens, base/accessibility rules, generated code chrome, and Markdown
  prose. Ordinary page and component layout uses website-local UnoCSS utilities.
- `src/pages/search-index.json.ts` emits the search catalog once. The site header fetches it lazily
  on first use instead of embedding hundreds of links in every page.

## Author documentation

Each Markdown document declares its product, title, description, ordering, and navigation group in
frontmatter. Stable section anchors use an explicit suffix:

```md
## Access decisions {#access-decisions}
```

Labelled code fences use ordinary metadata:

````md
```go title="cms/config.go"
var Config = ridu.Config{}
```
````

Plain Markdown is the default. Keep API signatures and other structured contracts out of prose,
and introduce MDX only when a document genuinely needs an embedded interactive component.
