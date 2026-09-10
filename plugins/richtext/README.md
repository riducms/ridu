# Ridu rich-text plugin

`github.com/riducms/ridu/plugins/richtext` provides the rich-text field, portable Lexical document
schema, server validation, reference metadata, and safe Go HTML renderer. Pair it with
`@riducms/plugin-richtext` for the Svelte/Lexical editor.

Register the backend once, then use its field constructor:

```go
ridu.Config{
	Plugins: []ridu.Plugin{richtext.New()},
	Collections: []ridu.Collection{{
		Slug: "posts",
		Fields: field.Fields{
			richtext.Field("content").Required(),
		},
	}},
}
```

`richtext.Field("content")` uses the current defaults. Pass one optional `richtext.Config`
to customize the field; an explicit empty config keeps the same defaults. Supplying more than
one configuration value panics immediately.

In an existing project, add both packages:

```sh
ridu add richtext \
  --go-package github.com/riducms/ridu/plugins/richtext \
  --admin-package @riducms/plugin-richtext
```

Then generate and create the selected adapter's migration. The generated registry imports the admin
pair and rejects incompatible pairing metadata.

See the [Rich text guide](../../website/src/content/docs/rich-text.md) for installation, features,
constraints, verification, SDK values, and server rendering.

## Schema-backed block embeds

Configure a fixed set of ordinary reusable definitions. Omitting `Features` enables the usual
features and blocks when definitions are present:

```go
callout := field.Block{
    Key: "callout", Label: "Callout", TypeName: "ArticleCallout",
    Fields: field.Fields{
        field.Text("title").Required(),
        field.Textarea("message").Localized(),
        field.Relationship("related", "posts"),
    },
}

body := richtext.Field("body", richtext.Config{
    Blocks: []field.Block{callout},
})
```

`Config.Blocks` remains executable Go schema. It is excluded from serialized plugin settings and
resolved through the field-owned `EmbeddedTrees` descriptor; all payload fields use ordinary defaults, validators, hooks,
access, localization, references and transactions. Groups, arrays and finite nested rich-text
fields work through that same boundary. An explicit `Features` list must include `FeatureBlocks`
when definitions are supplied. `FeatureBlocks` by itself is a configuration error.

Ordinary relationship/upload fields in these payloads participate in target admission, population,
reference indexing, and restrict/nullify actions. Standalone `relationship`/`upload` editor nodes
retain their earlier shape/collection-allowlist validation only: they can reference missing targets
and keep dangling IDs after deletion. Nesting an editor inside a block does not change that boundary.

The portable atomic node is:

```json
{"type":"block","version":1,"fields":{"_key":"stable-occurrence","blockType":"callout","title":"Read first"}}
```

Only `type`, `version` and `fields` belong to the node envelope; no editor `children` or top-level
payload properties are accepted. `_key` and `blockType` are reserved inside the schema payload.
The server assigns an omitted key for a new occurrence and preserves valid existing keys.

Generation instantiates `RichTextDocument<Payload>` and `RichTextDocumentInput<Payload>` with
field-specific unions. The portable TypeScript entry is `@riducms/plugin-richtext/document`.
The Go equivalent is `richtext.Document[generated.PostsBodyBlocksBlockPayload]` (with `InputPayload`
or `UpdatePayload` for writes). Its `Node[T].Fields` contains the generated scalar payload codec;
switch on `Fields.Value` to access concrete generated variants. Unknown variants fail typed decoding;
keep raw JSON in export/migration tools. Even plain fields have an empty allowlist: their TS payload
is `never`, and their generated Go payload decoder rejects every block variant.

For typed read/edit/save, convert the envelope while retaining only payload identities:

```go
update, err := richtext.MapDocumentBlocks(*article.Body,
    generated.PostsBodyBlocksBlockPayload.Retain)
// Handle err, then edit the relevant typed occurrence.
callout := update.Root.Children[1].Fields.Value.(*generated.ArticleCalloutUpdate)
title := "Updated title"
callout.Title = &title
// Save update through PostsUpdate.Body with the article's current revision.
```

`Retain` creates fresh update variants without copying redacted, localized or populated values.
`MapDocumentBlocks` copies prose and editor structure, preserving occurrence identity and order.
The [typed article workflow](../../examples/blocks/richtext_workflow_test.go) demonstrates a
populated read, localized children, nested rich text and revision-checked save without maps.

## Read-only rendering

`@riducms/plugin-richtext/render` exports `renderRichTextHTML` without editor, DOM or Svelte imports.
Pass a `RichTextBlockRenderers<Payload, string>` map for application-owned block HTML. Ordinary
text is escaped; renderer HTML is trusted application code. Missing node/block renderers throw by
default. The explicit `fallback(node, error)` option supports an application-owned visible recovery
placeholder.

For Svelte, `@riducms/plugin-richtext/svelte` exports `<RichText value={body} blocks={renderers} />`
and `RichTextBlockComponents<Payload>`. Each component receives its exact `{ block }` variant.
The optional `fallback` and `references` snippets make unsupported nodes and reference presentation
explicit. This read-only entry does not import Lexical or the admin editor.

Go applications use `richtext.RenderDocument(document, blockRenderer, nodeRenderers)`. The block
callback receives the generated payload codec for a normal concrete-type switch. `RenderHTML`
remains the deliberate low-level raw renderer hook. None of these renderers fetches relationships:
pass access-approved populated values, or resolve documented IDs in a bounded application query.

## Experimental content and schema changes

This is a prelaunch replacement of the experimental placeholder, with unchanged version numbering.
There is no automatic conversion or promise to accept arbitrary old payloads. Regenerate disposable
development fixtures through the current API. For content worth retaining, export the raw document
and revision JSON first, declare the intended block schemas, then use the adapter's existing
compiled migration transform to move legacy top-level `blockType`/payload properties into `fields`,
validate every declared variant and materialize stable identities. Inspect retained revisions and
localized values too. Do not simply relabel an undeclared payload as a supported variant.

Unknown stored variants enter the generic `block_recovery_required` boundary: ordinary consumers
receive recovery diagnostics, unredacted unknown payloads are withheld, and ordinary saves are
blocked. Recovery uses explicit raw export and compiled transforms; reads never silently convert
or drop historical blocks. Renaming/removing a variant or changing stored payload schemas uses the
same migration inspection and transform contracts as every Phase 3 structured-content plugin.

Adding another variant to an already enabled block allowlist is additive on all three adapters.
Enabling blocks for the first time on an existing plain rich-text field also changes its serialized
feature settings: the conservative generic planners require an explicit compiled validation
transform for that first opt-in, even if existing prose needs no rewriting. This is a migration
limitation, not an automatic compatibility conversion. Keep the existing document and node versions.
