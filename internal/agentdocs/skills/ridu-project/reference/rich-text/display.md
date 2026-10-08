<!-- Generated from website/src/content/docs/rich-text/display.md by scripts/sync-agent-docs.ts. -->

# Display rich text

A rich-text field saves JSON. Your frontend decides how it looks: render it with Go on the server,
or with JavaScript or Svelte in your application. To let your app's users write it too, see
[Use the editor in your app](./editor.md).

## Understand the saved JSON {#document}

The saved field has a `version` and a `root` containing its text and other content. For example:

```json
{
	"version": 1,
	"root": {
		"type": "root",
		"children": [
			{
				"type": "paragraph",
				"children": [
					{ "type": "text", "text": "Hello, Ridu", "format": 1 }
				]
			}
		]
	}
}
```

Each object inside `children` is called a **node**. A paragraph contains text nodes; a list contains
list items. Ridu checks these objects when saving and reports the path of invalid content, such as
a link with no URL or a block type you have not configured.

Documents may contain at most 10,000 nodes and nest at most 64 levels deep. Unsupported document
versions are rejected. Changing the document version requires a content migration.

## Display rich text with Go {#render-html}

Use `richtext.RenderDocument` with a generated Go document type. It renders the built-in formatting
and calls your function for custom blocks, so you can choose their HTML. The
[Go rendering example](https://github.com/riducms/ridu/blob/main/examples/blocks/render/article.go)
shows how to handle each generated block type.

If you are working with a raw `store.Value`, use `richtext.RenderHTML`. Supply functions for media,
relationships, and custom blocks:

```go
html, err := richtext.RenderHTML(
	value,
	map[string]func(store.Values) (string, error){
		"upload": func(node store.Values) (string, error) {
			return renderUploadReference(ctx, node)
		},
		"relationship": func(node store.Values) (string, error) {
			return renderRelationshipReference(ctx, node)
		},
		"block": func(node store.Values) (string, error) {
			return renderApplicationBlock(ctx, node)
		},
	},
)
if err != nil {
	return err
}
```

Built-in text is escaped, and links must be relative or use `http`, `https`, `mailto`, or `tel`.
Your rendering functions must escape their own field values. Fetch related content through the
local API so the user's permissions still apply; do not treat a stored document ID as a URL.

## Display rich text on your frontend {#frontend}

Fetch the document through your generated SDK, then pass its rich-text field to a renderer.
For JavaScript or TypeScript, use `renderRichTextHTML`:

```ts
import { renderRichTextHTML } from '@riducms/sdk/richtext';

const html = renderRichTextHTML(article.content);
```

Here, `article.content` is a saved rich-text value. Check it is present before rendering an optional
field. Install only `@riducms/sdk` in a content consumer: it includes the document types and this
renderer without Svelte, Lexical, or a browser.

For Svelte, import `RichText` from `@riducms/plugin-richtext/svelte` and render
`<RichText value={article.content} />`. To display custom blocks, pass a `blocks` object that maps
each block's key to a Svelte component. Each component receives the fields for its block type.
The [Svelte rendering example](https://github.com/riducms/ridu/blob/main/examples/blocks/render/Article.svelte)
shows a complete mapping.

The TypeScript equivalent maps keys to functions that return HTML. Use
`RichTextBlockRenderers` to check that every block has a function, or `RichTextBlockComponents`
for Svelte components. Missing renderers cause an error unless you provide a visible fallback.
Renderers do not fetch related data; load it before rendering.
