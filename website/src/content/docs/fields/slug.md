---
title: 'Slug field'
description: 'Generate a URL-friendly name from a title, and let authors override it.'
product: core
eyebrow: 'Basic fields'
order: 70
aliases: ['field.Slug', 'URL slug', 'NormalizeSlug']
relatedSymbolIds:
  [
    'go:github.com/riducms/ridu/field#Slug',
    'go:github.com/riducms/ridu/field#NormalizeSlug'
  ]
navigation:
  section: 'Model content'
  parent: fields
  order: 100
  title: 'Slug'
---

Use `field.Slug` for a URL-safe identifier derived from another string field. It uses text
storage and generated `string` types, but is always required, collection-wide unique, and
indexed.

## In the admin {#admin-behavior}

![A generated Slug field in the Ridu admin, populated from the document title.](../../../../../docs/assets/fields/slug.png)

_The value follows source edits until an author overrides it; **Generate slug** restores source-driven updates._

## Generate a slug from a title {#example}

```go title="content/posts.go"
field.Text("title").Required(),
field.Slug("slug", "title"),
```

When `slug` is omitted on create, Ridu derives it from `title`. Generated slugs follow later source
edits. Once an author submits a different slug, that manual value remains stable. The
admin's **Generate slug** action returns it to source-derived behavior.

The source can be a direct string or a string beneath non-repeated groups, for example
`seo.pageTitle`. It cannot traverse an array or blocks list.

## Configuration {#configuration}

| Constructor or method                     | What it controls                                                                    |
| ----------------------------------------- | ----------------------------------------------------------------------------------- |
| `field.Slug(name, sourcePath)`            | Creates a required, unique, indexed text field generated from a string source path. |
| `sourcePath`                              | Names a direct string or a string inside non-repeated groups, such as `seo.title`.  |
| `.Label(...)` / `.LabelTranslations(...)` | Changes author-facing copy without changing the stored key.                         |
| `.Admin(...)`                             | Sets description, width, visibility, and other editor presentation.                 |
| `.Access(...)` / `.RestrictAccess(...)`   | Replaces or narrows field create/read/update access.                                |
| `.Validate(...)` / `.LiveValidate(...)`   | Adds save or live rules after slug normalization.                                   |
| `field.NormalizeSlug(text)`               | Applies the same lowercase, separator, and cleanup rules in application code.       |

## How titles become slugs {#normalization}

Ridu converts the title in the same way in the server and admin: ASCII letters become lowercase,
digits and underscores remain, whitespace and hyphen runs become one hyphen, and other characters
are removed. Requests that bypass the admin are normalized too. Use `field.NormalizeSlug` when
application code needs the same result.

```go title="slug_test.go"
got := field.NormalizeSlug("  Hello, Ridu!  ")
// got == "hello-ridu"
```

## Localization and changing URLs {#constraints}

Slug fields cannot declare a default or `Localized`. Their source chain cannot be localized either.
For locale-specific URLs, model separate explicit fields and routing rules. Labels, descriptions,
conditions, and compatible string presentation options still work.

A slug rename can break inbound URLs even when the database migration succeeds. Preserve redirects
in the consuming application and decide whether old values must remain reserved.

## Common mistakes {#troubleshooting}

- Do not duplicate `Required`, `Unique`, or `Index`; `Slug` already applies them.
- A title that becomes empty after conversion is invalid; require
  a usable source or let the author enter a manual value.
- Do not expect Unicode transliteration. Normalization is ASCII and deterministic.

See [`field.Slug`](/reference/field/slug/) and
[`field.NormalizeSlug`](/reference/field/normalize-slug/).
