---
title: 'Code field'
description: 'Edit source code, templates, or queries with syntax highlighting.'
product: core
eyebrow: 'Basic fields'
order: 64
aliases: ['field.Code', 'source code field', 'code editor']
relatedSymbolIds:
  [
    'go:github.com/riducms/ridu/field#Code',
    'go:github.com/riducms/ridu/field#Admin'
  ]
navigation:
  section: 'Model content'
  parent: fields
  order: 40
  title: 'Code'
---

Use `field.Code` for source text, templates, queries, or configuration that benefits from syntax
highlighting. It stores a string; Ridu does not execute, compile, or trust the value.

## In the admin {#admin-behavior}

![A populated TypeScript Source code field in the Ridu admin.](../../../../../docs/assets/fields/code.png)

_Language changes highlighting only; stored source remains an unexecuted string._

## Choose a highlighting language {#example}

```go title="content/snippets.go"
field.Code("source").
	Required().
	Admin(field.Admin{CodeLanguage: "typescript"})
```

`Admin.CodeLanguage` is an editor hint. It does not validate that the source parses and does not transform
the returned string.

## Configuration {#configuration}

| Constructor or method                             | What it controls                                                      |
| ------------------------------------------------- | --------------------------------------------------------------------- |
| `field.Code(name)`                                | Creates a stored string shown in a code editor.                       |
| `.Required()`                                     | Rejects missing, null, and empty source.                              |
| `.MinLength(n)` / `.MaxLength(n)`                 | Sets inclusive source-length bounds.                                  |
| `.Default(value)` / `.DefaultFrom(callback)`      | Supplies fixed or request-aware initial source.                       |
| `.Localized()`                                    | Stores separate source for each configured content locale.            |
| `.Admin(field.Admin{CodeLanguage: ...})`          | Chooses syntax highlighting; it does not parse or execute the source. |
| `.Validate(callback)` / `.LiveValidate(callback)` | Adds application-specific save or live checks.                        |

## Limit length and customize the editor {#options}

Code accepts string length constraints, a string default, localization, indexing/uniqueness, and
the common admin options. Only Code accepts `field.Admin.CodeLanguage`; applying it to Text or Textarea is a
configuration error reported by `ridu.Resolve`.

```go title="content/templates.go"
field.Code("template").MaxLength(50_000).Admin(field.Admin{
	CodeLanguage: "html",
	Description:  "Validated and rendered by the application.",
})
```

Queries use the string operators. Localization can be useful for locale-specific
templates, but be explicit about fallback behavior when a missing translation would change
execution or presentation.

## Security and troubleshooting {#troubleshooting}

- Treat code values as untrusted content. Never pass them to `eval`, a shell, a template engine, or
  a database without an application-owned parser, allowlist, and resource bounds.
- Syntax highlighting is not syntax validation. Add a hook or plugin validator when valid source
  is required by your application.
- Use [JSON](/docs/fields/json/) when machines need structured configuration rather than source
  text, and Rich text when authors need formatted prose.

See [`field.Code`](/reference/field/code/) and [`field.Admin.CodeLanguage`](/reference/field/admin/).
