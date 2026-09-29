---
title: 'Rich text blocks'
description: 'Add structured blocks such as callouts to rich text, translate them, read their data, and change their definitions safely.'
product: plugins
eyebrow: 'Rich text'
order: 193
aliases:
  [
    'rich text blocks',
    'Config.Blocks',
    'BlockReferences',
    'callout block',
    'block_recovery_required'
  ]
navigation:
  section: 'Extend Ridu'
  parent: 'rich-text'
  order: 30
  title: 'Custom blocks'
---

A block adds structured content between paragraphs: a callout, image gallery, or call-to-action,
for example. Define its fields in Go just as you would for a collection.

## Add custom blocks {#blocks}

This example adds a Callout block with a title, a translatable message, and a related Post:

```go title="content/articles.go"
callout := field.Block{
	Slug:     "callout",
	TypeName: "ArticleCallout",
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

Add `body` to the collection's `Fields` list. With the development server running, open the editor
and choose Callout from the insert menu. Fill in its fields, then choose Apply to insert it into
the article or Cancel to discard it. You can edit, duplicate, move, or remove the card afterward,
and undo or redo those changes.

Finish or cancel changes in a block drawer before saving the article. Apply updates the editor;
the article's Save button sends the complete document to Go, where validators and hooks run.
Errors appear on the affected card and its fields.

Adding `Blocks` enables the block tools alongside the defaults. If you also provide a `Features`
list, include `richtext.FeatureBlocks`. Ridu only accepts block types listed in `Blocks`. To share
one definition between several fields, register it in the root `Config.Blocks` and select it with
`BlockReferences` instead of `Blocks`; a field uses one or the other.

Blocks can contain groups, arrays, uploads, relationships, and another rich-text field. Their
fields keep the normal defaults, validation, hooks, and permissions.
[Dynamic defaults](/docs/fields/defaults/) run on the server when the parent document is saved;
they do not prefill an unsaved embedded form. A field cannot contain its own definition through an
endless chain of nested blocks.

## Translate blocks {#block-localization}

Call `.Localized()` on the rich-text field to give each locale its own complete article.
Alternatively, localize individual fields inside a block, such as `message` above. In that case,
the surrounding text and block order stay shared: moving or deleting a block affects every locale.
Copying a locale copies those translated values into the same blocks.

Block values belong to the article, so they are saved in its drafts and version history.
Permissions on a child field can also prevent removing the block that contains it.

## Read block data {#block-data}

In the saved JSON, a block's values appear under `fields`:

```json
{
	"type": "block",
	"version": 1,
	"fields": {
		"_key": "stable-key",
		"blockType": "callout",
		"title": "Read first"
	}
}
```

`blockType` identifies the block, and `_key` identifies this particular use of it. Ridu supplies
a missing key when a block is inserted. Do not use either reserved name for your own fields.
Unknown block types and undeclared fields cause validation errors.

Ridu generates Go and TypeScript types for each configured block, including separate types for
creating and updating it. The same definitions appear in OpenAPI. See the
[blocks example](https://github.com/riducms/ridu/tree/main/examples/blocks) for an application that
creates, updates, and renders typed blocks.

A rich-text field is stored as one JSON value. If you need to filter or update content independently
of its article, use a separate collection or a regular field outside the rich text. Large articles
also produce larger reads, writes, and version snapshots.

## Change block definitions safely {#migrations}

Renaming a block, removing it, or changing its fields may affect existing articles and revisions.
Use [Migrations](/docs/migrations/) to update stored content, including the locales and revision
history you intend to keep. Disposable development content can be recreated instead.

If saved content contains a block type that is no longer configured, Ridu reports
`block_recovery_required` and blocks saving. Restore the previous block definition to read and edit
it again, or export the original JSON and write a Go migration to convert it. The admin keeps
unsaved values after a schema reload and offers a recovery export.
