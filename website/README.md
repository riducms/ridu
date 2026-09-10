# Ridu website

The public, consumer-facing home, documentation, guides, and API reference for Ridu. Repository
architecture and maintainer documentation remain as plain Markdown under `docs/`.

## Develop

This package has its own Bun lockfile and sits outside the framework's root Bun workspace.

```sh
bun run dev:website
bun run --cwd website verify
```

Run these commands from the repository root. `dev:website` installs both locked dependency trees
and compiles `@riducms/build` before starting Astro. The website consumes that package’s canonical
UnoCSS utilities through its published export; its output must exist before Astro loads the config.
Only the build package is compiled; the admin and other runtime packages are not required.

`verify` checks formatting and types, runs focused unit tests, builds all static routes, validates
links and anchors, checks the lazy search catalog, and guards the total generated HTML size.

## Production and Cloudflare Pages

From a fresh checkout, run:

```sh
bun run build:website
```

This uses the same preparation as local development and writes the static site to `website/dist`.
It needs no existing `node_modules`, package `dist`, or `NODE_PATH` override.

Configure Cloudflare Pages with:

| Setting                   | Value                   |
| ------------------------- | ----------------------- |
| Root directory            | `/` (repository root)   |
| Build command             | `bun run build:website` |
| Build output directory    | `website/dist`          |
| `BUN_VERSION`             | `1.4.0`                 |
| `NODE_VERSION`            | `24`                    |
| `SKIP_DEPENDENCY_INSTALL` | `true`                  |

Use these settings for production and previews. The build command owns dependency installation
and compilation in order; Cloudflare’s automatic dependency install stays disabled.

Cloudflare supports these explicit version overrides and the dependency-install switch in its
[build image configuration](https://developers.cloudflare.com/pages/configuration/build-image/).
If a build logs a Bun `list-all` HTTP 403, check whether the pinned Bun version subsequently
installs. The upstream [asdf-bun plugin](https://github.com/cometkim/asdf-bun/blob/main/lib/utils.bash)
queries GitHub’s releases API to discover versions, then downloads an explicit version from a
separate release-asset URL. A discovery failure can therefore precede a successful installation.
The status alone does not establish rate limiting. Keep the pin when installation succeeds;
diagnose a later missing `@riducms/build/dist/uno/index.js` through the preparation command above.

`bun run check:website:clean` copies current source files into a temporary checkout with an empty
Bun cache and no installed dependencies or generated package output, runs the production command
without `NODE_PATH`, and validates the emitted routes, assets, and search catalog. It runs in
`make documentation-check`, the local release gate, and the manual release workflow before publishing.

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
