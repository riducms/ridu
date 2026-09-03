<!-- Generated from website/src/content/docs/rich-text.md by scripts/sync-agent-docs.ts. -->

# Rich text

The rich-text plugin pairs a Go field and validator with a Svelte 5 Lexical editor. It stores
portable, versioned JSON rather than browser editor state. Choose the enabled features in Go config.

## Use it in a new project {#new-project}

The `starter` template already installs and registers rich text. Open `content/posts.go`, use
`richtext.Field("content")`, then run `ridu dev`. Do not add a second registration.

If you chose the `blank` template, follow the existing-project installation below.

## Add it to an existing project {#existing-project}

Add the Go and admin packages together:

```bash title="terminal"
npm run ridu -- add richtext \
  --go-package github.com/riducms/ridu/plugins/richtext \
  --admin-package @riducms/plugin-richtext
```

Register the backend once and use `richtext.Field` like a built-in field:

```go title="content/config.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
)

func Config() ridu.Config {
	return ridu.Config{
		Name:        "Acme Editorial",
		Plugins:     []ridu.Plugin{richtext.New()},
		Collections: []ridu.Collection{Posts},
	}
}

var Posts = ridu.Collection{
	Slug: "posts",
	Fields: []field.Definition{
		field.Text("title", field.Required()),
		richtext.Field("content", field.Required()),
	},
}
```

Start the development loop and verify the editor locally:

```bash title="terminal"
npm run dev
```

Create a document containing formatting and a link, reload it, and read the same value through the
generated SDK. If uploads or relationships are enabled, verify those targets too.

Before deployment, create and verify the immutable migration:

```bash title="terminal"
npm run ridu -- migrate create --name add-rich-text
npm run ridu -- migrate plan
npm run ridu -- migrate verify
npm run ridu -- check
```

Apply the reviewed artifact with `migrate up` during deployment, then require a clean
`migrate status`. Supply the selected adapter's URL/path and production safety flags as described
in its guide.

## Default authoring features {#defaults}

`richtext.Field` enables the recommended set:

- links with in-place editing and safe URL validation;
- ordered, unordered, and check lists;
- code blocks and horizontal rules;
- access-aware upload cards with per-placement captions;
- relationship cards; and
- headings, quotes, alignment, indentation, line breaks, and inline bold, italic, underline,
  strike-through, subscript, superscript, and code as editor baseline nodes.

Authors use a slash/insert menu, keyboard navigation, floating selection toolbar, and the admin's
shared relationship/upload browser. Upload and relationship cards store only a stable collection
slug and document ID; selecting a card can inspect, replace, or remove the placement without
deleting the referenced document.

![The official Ridu rich-text field in a Post editor with portable Lexical content and the surrounding schema-driven form.](https://raw.githubusercontent.com/riducms/ridu/main/docs/assets/ridu-admin-rich-text.png)

_Saving persists the portable document value described below, not DOM or browser editor state._

An upload node can add a placement-specific caption without changing the asset's reusable `alt`
field:

```json
{
	"type": "upload",
	"version": 1,
	"relationTo": "media",
	"id": "asset_123",
	"caption": "A crop used only in this article"
}
```

Reference pickers and hydrated cards respect target collection access. The server also validates
the collection slug and ID when saving; an editor preview is never the authorization boundary.

## Choose a feature set {#features}

`FieldWithConfig` accepts an allowlist. `Features: nil` inherits the defaults; any non-nil slice
replaces them, including an empty slice:

```go
richtext.FieldWithConfig("content", richtext.Config{
	Features: []richtext.Feature{
		richtext.FeatureLinks,
		richtext.FeatureLists,
		richtext.FeatureCode,
		richtext.FeatureHorizontalRule,
		richtext.FeatureUploads,
		richtext.FeatureRelationships,
	},
	UploadCollections:       []string{"media"},
	RelationshipCollections: []string{"posts", "pages"},
}, field.Required())
```

An empty upload/relationship collection list allows every compatible collection; a non-empty list
restricts both admin choices and server validation.

`FeatureBlocks` does not provide custom block registration or a complete authoring workflow. Custom
blocks still need a versioned node schema, editor integration, and renderer.

## Portable document contract {#document}

Every value has exactly one document version and one root:

```json
{
	"version": 1,
	"root": {
		"type": "root",
		"children": [
			{
				"type": "paragraph",
				"children": [{ "type": "text", "text": "Hello, Ridu", "format": 1 }]
			}
		]
	}
}
```

Ridu rejects an unsupported document version, disabled or unknown node, malformed child/reference,
missing text/link data, a tree deeper than 64, or more than 10,000 nodes with path-aware validation
issues. Changing `DocumentVersion` requires a content migration; the server never silently upgrades
stored JSON during a read.

Required, localized, read-only, and layout options work as they do for other fields. Test the exact
combination of localization, versions, and live preview that your application uses.

## Render safe HTML in Go {#render-html}

`RenderHTML` escapes text and renders paragraphs, headings, quotes, formatting, links, lists,
check-list state, code, line breaks, alignment/indent metadata, and horizontal rules. It accepts
only relative URLs or `http`, `https`, `mailto`, and `tel` links.

Provide upload, relationship, and block renderers by node type. Inspect the collection and ID or
`blockType`:

```go
html, err := richtext.RenderHTML(value, map[string]func(store.Values) (string, error){
	"upload": func(node store.Values) (string, error) {
		return renderUploadReference(ctx, node)
	},
	"relationship": func(node store.Values) (string, error) {
		return renderRelationshipReference(ctx, node)
	},
	"block": func(node store.Values) (string, error) {
		return renderApplicationBlock(ctx, node)
	},
})
if err != nil {
	return err
}
```

Resolve referenced content through the local API so access and redaction still apply. Do not trust
stored reference data as an HTML URL or concatenate unescaped application fields.

## Frontend rendering boundary {#frontend}

Ridu supplies safe Go HTML rendering, not a generic read-only Svelte component. A frontend can
render the JSON, call an endpoint that uses `RenderHTML`, or provide a custom renderer.

Not included: tables, embeds, custom block authoring, Markdown shortcuts, HTML/Markdown import and
export, plain-text rendering, custom-node migration hooks, Yjs collaboration, and a read-only Svelte
renderer.

See the [`richtext` Go reference](https://riducms.com/reference/richtext/), the
[`@riducms/plugin-richtext` reference](https://riducms.com/reference/plugin-richtext/), [Uploads](./uploads.md),
and [Build a custom field](https://riducms.com/guides/custom-fields/).
