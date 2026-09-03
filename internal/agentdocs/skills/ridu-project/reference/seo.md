<!-- Generated from website/src/content/docs/seo.md by scripts/sync-agent-docs.ts. -->

# SEO

The SEO plugin adds metadata fields to selected collections and globals, with an overview, title and
description guidance, an optional upload image, and a search-result preview.

The fields use the resource's normal access, validation, localization, drafts, versions, hooks,
REST routes, and generated types.

## Use it in a new project {#new-project}

Generated projects include `@riducms/plugin-seo` in the root and admin workspaces. Add `seo.New` to
your Go config, select the resources, and configure any generator callbacks.

## Add it to an existing project {#existing-project}

Install the package in both the root and admin workspaces:

```bash title="terminal" package-manager="bun"
bun add @riducms/plugin-seo
bun add --cwd admin @riducms/plugin-seo
go mod tidy
```

```bash title="terminal" package-manager="npm"
npm install @riducms/plugin-seo
npm install --workspace admin @riducms/plugin-seo
go mod tidy
```

```bash title="terminal" package-manager="pnpm"
pnpm add --workspace-root @riducms/plugin-seo
pnpm --dir admin add @riducms/plugin-seo
go mod tidy
```

```bash title="terminal" package-manager="yarn"
yarn add --ignore-workspace-root-check @riducms/plugin-seo
yarn --cwd admin add @riducms/plugin-seo
go mod tidy
```

Then add `seo.New` below. The plugin requires resource options, so configure it in Go rather than
with a zero-argument `ridu plugin add` entry.

## Configure the paired plugin {#configure}

Configure the compiled Go plugin and select the resources that receive metadata:

```go title="content/config.go"
package content

import (
	"fmt"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/plugins/seo"
	"github.com/riducms/ridu/schema"
)

func Config() ridu.Config {
	return ridu.Config{
		Name: "Acme Editorial",
		Plugins: []ridu.Plugin{
			seo.New(seo.Config{
				Collections:       []schema.CollectionSlug{"pages", "posts"},
				Globals:           []schema.CollectionSlug{"site-settings"},
				UploadsCollection: "media",
				TabbedUI:          true,
				GenerateTitle: func(ctx seo.GenerateContext) (string, error) {
					title, _ := ctx.Document["title"].(string)
					return title + " | Acme", nil
				},
				GenerateURL: func(ctx seo.GenerateContext) (string, error) {
					if ctx.Collection == nil {
						return "https://example.com/", nil
					}
					slug, _ := ctx.Document["slug"].(string)
					return fmt.Sprintf("https://example.com/%s", slug), nil
				},
			}),
		},
	}
}
```

```sh title="terminal"
npm install
npm run dev
```

Open one configured collection or global, edit the SEO title, description, and image, save, and read
the generated `meta` value through the SDK.

Before deployment, create and verify the immutable migration:

```sh title="terminal"
npm run ridu -- migrate create --name add-seo
npm run ridu -- migrate plan
npm run ridu -- migrate verify
npm run ridu -- check
```

Apply the reviewed artifact with `migrate up` during deployment, then require a clean
`migrate status`. Use the selected adapter's connection flags and safety admission for those
commands.

## Default field shape {#default-fields}

The plugin injects one `meta` group in this order:

| Path               | Field behavior                                                                    |
| ------------------ | --------------------------------------------------------------------------------- |
| `meta.overview`    | Presentation-only summary of title, description, and image checks.                |
| `meta.title`       | Text with 50–60 character guidance and optional generation.                       |
| `meta.description` | Textarea with 100–150 character guidance and optional generation.                 |
| `meta.image`       | Optional upload reference when `UploadsCollection` is configured.                 |
| `meta.preview`     | Presentation-only result preview using the current unsaved title and description. |

The character ranges are authoring guidance, not validation. Add `field.MinLength` or
`field.MaxLength` through a field override when the server must reject values outside a range.

Injected metadata fields become localized when application content localization is enabled. The
admin shows exact-language values, reports missing fallback values, and passes the selected locale
to generation callbacks.

`UploadsCollection` must name an upload-enabled collection. Authors can select an existing asset,
inspect the selected upload, replace it, remove it, or ask the configured image generator for an
asset ID. Upload access and normal document validation still apply.

## Place metadata in tabs {#tabs}

Set `TabbedUI: true` to place metadata in a separate tab. Ridu preserves an existing leading tab group
and appends an SEO tab. If fields are not already tabbed, it groups ordinary content under the
resource label, preserves authored tabs, and adds SEO last. An auth collection's email field stays
outside the tabs so authentication semantics do not change.

Without `TabbedUI`, the `meta` group is appended to the selected resource's existing fields.

## Generate from the unsaved draft {#generation}

Each callback receives a `GenerateContext` containing:

- the current unsaved document snapshot;
- the document ID, when editing an existing document;
- the selected content locale;
- a detached collection or global definition; and
- the authenticated plugin endpoint context, including the access-controlled local API.

`GenerateTitle`, `GenerateDescription`, and `GenerateImage` require create access for a new
collection document, update access for an existing document, or update access for a global.
`GenerateURL` requires create access for a new document and read access for an existing collection
document or global. Every generation call requires an authenticated actor.

Generated values are proposed form edits. The author can inspect or change them, and normal save or
publish validation persists them. A generator never writes around the form or operation engine.

The admin cancels superseded calls and discards a response when the draft, resource, ID, or locale
has changed. While a URL request is pending or has failed, the preview clears the old URL instead of
presenting it as current.

> [!IMPORTANT]
> Generator callbacks run on the server. Keep credentials and executable behavior there; never put
> them in field or renderer configuration sent to the admin.

## Replace or directly place fields {#field-overrides}

`Config.Fields` receives a copy of the ordered defaults and returns the complete contents of the
injected `meta` group. Use it to add validation, change labels, insert fields, or remove a default:

```go
Fields: func(defaults []field.Definition) ([]field.Definition, error) {
	return []field.Definition{
		defaults[0],
		seo.MetaTitle(true, field.Required(), field.MaxLength(70)),
		seo.MetaDescription(true, field.MaxLength(180)),
		defaults[3],
		defaults[4],
	}, nil
},
```

The callback must return at least one field. Its output is resolved and validated like application
config, so duplicate names, incompatible options, invalid renderer metadata, and bad upload targets
fail before startup.

`Overview`, `MetaTitle`, `MetaDescription`, `MetaImage`, and `Preview` are also public constructors
for direct placement. Their `WithConfig` variants map overview and preview components to custom
paths or customize image presentation. Direct title, description, and image constructors are
localized by default; use injected fields or an ordinary field plus the lower-level
admin-component contract when an application has no localization configuration.

`Collections` and `Globals` select automatic injection. Direct fields can live on another configured
resource: the authenticated generation endpoint resolves that resource and applies its ordinary
access rules even when it is not in an injection selector.

## Endpoint and failure behavior {#endpoints}

The plugin declares four `POST` endpoints under `/api/plugins/seo/`:

| Endpoint               | Result                                            |
| ---------------------- | ------------------------------------------------- |
| `generate-title`       | `{ "result": "..." }` from `GenerateTitle`.       |
| `generate-description` | `{ "result": "..." }` from `GenerateDescription`. |
| `generate-image`       | An upload document ID from `GenerateImage`.       |
| `generate-url`         | A preview URL from `GenerateURL`.                 |

All four routes remain present when a callback is omitted and return an empty result. Requests are
limited to 1 MiB, reject unknown or trailing JSON, require exactly one collection or global, and
return the normal structured Ridu error envelope. Use the SDK's `requestPlugin` method rather than
building a URL manually; it retains base URL, credentials, headers, middleware, cancellation, and
`RiduError` behavior.

| Authoring state                        | Result                                                                     |
| -------------------------------------- | -------------------------------------------------------------------------- |
| Callback is not configured             | Auto-generation is not offered; the endpoint returns an empty result.      |
| Actor lacks resource access            | The endpoint returns `access_denied`; existing field values remain intact. |
| Callback fails                         | The admin reports a retryable generation error and does not change value.  |
| Draft changes during generation        | The old request is cancelled or its response is discarded.                 |
| Referenced image is unreadable/deleted | The field reports the unavailable reference without inventing metadata.    |

## Scope and boundaries {#boundaries}

The plugin owns search metadata authoring. It does not inject canonical links, robots directives,
Open Graph variants, Twitter cards, JSON-LD, redirects, sitemaps, or frontend `<head>` rendering.
Read the stored metadata in your application frontend and render the exact tags your delivery
surface needs.

See the [`seo` Go reference](https://riducms.com/reference/seo/),
[`@riducms/plugin-seo` reference](https://riducms.com/reference/plugin-seo/), [Plugin system](./plugins.md),
[Localization](./localization.md), [Uploads](./uploads.md), and
[TypeScript SDK](./typescript-sdk.md).
