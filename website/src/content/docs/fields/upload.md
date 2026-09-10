---
title: 'Upload field'
description: 'Let authors select an image, file, or gallery from a media collection.'
product: core
eyebrow: 'Relationship and media fields'
order: 78
aliases: ['field.Upload', 'media relationship field', 'file picker']
relatedSymbolIds: ['go:github.com/riducms/ridu/field#Upload']
navigation:
  section: 'Model content'
  parent: fields
  order: 180
  title: 'Upload'
---

Use `field.Upload` to add an image or file picker to a document. For example, a post can select
a hero image from your media collection. The field stores the media document’s ID; the file
itself lives in your configured storage.

## In the admin {#admin-behavior}

![A populated Cover upload field in the Ridu admin showing the selected field-guide image.](../../../../../docs/assets/fields/upload.png)

_Authors select a file from the media collection._

## Add an image or file picker {#example}

First, define a collection that accepts uploads:

```go title="content/media.go"
var Media = ridu.Collection{
	Slug:   "media",
	Upload: true,
	Fields: field.Fields{
		field.Text("alt").Required(),
	},
}
```

```go title="content/posts.go"
field.Upload("heroImage", "media").
	Required().
	OnDelete(field.ReferenceDeleteRestrict)
```

Pass that collection’s slug to `field.Upload`. The collection must exist and have `Upload: true`.
Use `field.Uploads("gallery", "media")` to let authors select several files.

## Configuration {#configuration}

| Constructor or method                                                                  | What it controls                                                               |
| -------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| `field.Upload(name, collection)`                                                       | Stores one ID from an upload-enabled collection.                               |
| `field.Uploads(name, collection)`                                                      | Stores an ordered list of upload document IDs.                                 |
| `.Required()`                                                                          | Requires one selected file, or a non-empty list for `Uploads`.                 |
| `.FilterOptionRules(rules...)`                                                         | Narrows picker choices and server admission with finite query predicates.      |
| `.OnDelete(field.ReferenceDeleteRestrict)` / `.OnDelete(field.ReferenceDeleteNullify)` | Rejects a hard delete while referenced, or clears/removes matching references. |
| `.Index()` / `.Unique()`                                                               | Available on a singular Upload reference.                                      |
| `.Localized()`                                                                         | Stores the selected reference or list separately for each locale.              |
| `.Validate(...)`, `.LiveValidate(...)`, `.Access(...)`, `.Hooks(...)`                  | Adds application rules and lifecycle behavior.                                 |

## Filter the available files {#options}

The picker shows files the current user can read. Use `FilterOptionRules` to narrow the choices
by MIME type or another field value. The server checks these rules again when saving. See
[filtering relationship choices](/docs/fields/relationship/#option-filters) for an example.

Upload fields also support `Required`, `Localized`, delete behavior, conditions, and admin
settings.

## Read file details and upload new files

Reads return the selected media ID. Request population to include details such as filename,
MIME type, image dimensions, generated sizes, and `alt` text. Access rules still apply to the
media document and its fields.

To add a new file, use the admin, the SDK’s `upload` or `uploadFromURL` method, or the multipart
REST endpoint. Then select the resulting media document in this field.

## Common mistakes {#troubleshooting}

- Passing `"media"` to `field.Upload` does not enable uploads on that collection; set `Upload: true`.
- Never submit or expose raw storage object keys as if they were media references.
- Required uploads cannot use nullify-on-delete. Decide retention and permanent deletion behavior
  before authors build references.
- Database and object storage must be backed up and restored as one recovery point.

See [`field.Upload`](/reference/field/upload/) and [Uploads and media](/docs/uploads/) for file
uploading, image sizes, delivery, and storage configuration.
