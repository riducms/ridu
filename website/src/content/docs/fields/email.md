---
title: 'Email field'
description: 'Store and validate an email-shaped string with a purpose-built admin control.'
product: core
eyebrow: 'Scalar and choice fields'
order: 63
aliases: ['field.Email', 'email address field']
relatedSymbolIds: ['go:github.com/riducms/ridu/field#Email']
navigation:
  section: 'Model content'
  parent: fields
  group: 'Scalar & choice'
  order: 30
  title: 'Email'
---

Use `field.Email` when a document property is an email address. It retains a string value contract
but adds server-side email-shape validation and an email control in the admin.

## In the admin {#admin-behavior}

![A populated Contact email field in the Ridu admin.](../../../../../docs/assets/fields/email.png)

_The email input and server share shape validation; the stored value remains a string._

## Smallest working example {#example}

```go title="content/contacts.go"
field.Email("contactEmail", field.Required())
```

The operation engine validates requests from every transport, not only values entered in the
admin. Invalid input produces a `422 validation` response with an issue at `contactEmail`.

## Options and behavior {#options}

```go title="content/teams.go"
field.Email(
	"billingEmail",
	field.Required(),
	field.Unique(),
	field.Description("Invoices and payment notices are sent here."),
)
```

Email supports common string-field presentation, default, localization, uniqueness, and indexing
options. Use `Unique` when one address must identify at most one document in that collection. An
email field does not send mail, verify ownership, or make a collection authenticate users; those
are separate application and [authentication](/docs/authentication/) concerns.

Email values can be selected, sorted, and queried with the string filter vocabulary. A localized
email is valid when each locale genuinely needs a different address; translating the label uses
`LabelTranslations` instead.

## Common mistakes {#troubleshooting}

- Syntactic validation does not confirm that an inbox exists or belongs to the actor. Use a
  verification workflow for that claim.
- Normalize addresses according to your product policy before relying on uniqueness. Ridu does not
  invent provider-specific equivalence rules such as stripping dots or `+` suffixes.
- A collection with an `email` field is not automatically an auth collection. Set `Auth: true` and
  configure its access and account workflows.

See [`field.Email`](/reference/field/email/) and [Authentication](/docs/authentication/).
