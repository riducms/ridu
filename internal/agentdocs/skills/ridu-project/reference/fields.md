<!-- Generated from website/src/content/docs/fields.md by scripts/sync-agent-docs.ts. -->

# Fields

Fields are the building blocks of a Ridu document. One Go definition drives operation-engine
validation, storage, migrations, generated Go and TypeScript types, REST/OpenAPI contracts, and the
control rendered in the admin.

## Field anatomy {#field-anatomy}

The constructor chooses the value kind, its first argument becomes the document property, and typed
options refine validation and presentation:

```go title="content/posts.go"
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Posts = ridu.Collection{
	Slug: "posts",
	Fields: []field.Definition{
		field.Text("title", field.Required()),
		field.Select(
			"status",
			field.Choices(
				field.Choice{Value: "draft", Label: "Draft"},
				field.Choice{Value: "published", Label: "Published"},
			),
			field.Default("draft"),
		),
	},
}
```

`title` and `status` are public paths in JSON, generated clients, queries, access maps, hooks, and
migrations. Labels and descriptions are author-facing metadata and never rename stored data.

![A clean overview of representative Ridu field controls: text, select, array, relationship, tabs, and rich text.](https://raw.githubusercontent.com/riducms/ridu/main/docs/assets/ridu-admin-field-showcase.png)

### Names are data contracts {#field-names}

A field name starts with a lowercase letter, contains letters/numbers/underscores, and is unique
among siblings. `id`, `createdAt`, `updatedAt`, and `deletedAt` are framework-owned. Nested names
form paths such as `seo.description`; runtime issue paths include row indexes such as
`links.2.label`.

Treat a field rename as an API and data migration. Change `field.Label` when only author copy should
change.

## Choose a built-in field {#built-in-fields}

Start with the value shape consumers should receive, then choose its authoring control.

| Group                  | Field pages                                                                                                                                                                                                                                                                                                        | Value                                                        |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------ |
| Scalar and choice      | [Text](https://riducms.com/docs/fields/text/), [Textarea](https://riducms.com/docs/fields/textarea/), [Email](https://riducms.com/docs/fields/email/), [Code](https://riducms.com/docs/fields/code/), [Date](https://riducms.com/docs/fields/date/), [Number](https://riducms.com/docs/fields/number/), [Checkbox](https://riducms.com/docs/fields/checkbox/), [Select](https://riducms.com/docs/fields/select/), [Radio](https://riducms.com/docs/fields/radio/), [Slug](https://riducms.com/docs/fields/slug/) | Strings, numbers, booleans, and finite choices               |
| Structured             | [JSON](https://riducms.com/docs/fields/json/), [Point](https://riducms.com/docs/fields/point/), [Group](https://riducms.com/docs/fields/group/), [Array](https://riducms.com/docs/fields/array/), [Blocks](https://riducms.com/docs/fields/blocks/), [Tabs](https://riducms.com/docs/fields/tabs/)                                                                                                                                   | JSON values, objects, repeated rows, and discriminated lists |
| Relationship and media | [Relationship](https://riducms.com/docs/fields/relationship/), [Upload](https://riducms.com/docs/fields/upload/)                                                                                                                                                                                                                                         | Stable document references or populated documents            |
| Layout                 | [Row](https://riducms.com/docs/fields/row/), [Collapsible](https://riducms.com/docs/fields/collapsible/), [UI](https://riducms.com/docs/fields/ui/)                                                                                                                                                                                                                         | No stored property; these organise the editor                |
| Computed and plugin    | [Join](https://riducms.com/docs/fields/join/), [Virtual](https://riducms.com/docs/fields/virtual/), [Plugin](https://riducms.com/docs/fields/plugin/)                                                                                                                                                                                                                       | Response-only output or a compiled plugin contract           |

### Scalar fields {#scalar-fields}

Use Text for a short string, Textarea for multi-line plain text, Code for a syntax-aware editor,
and Email for server-validated addresses. Date keeps an explicit string format. Number and Checkbox
use JSON number/boolean values. Select and Radio generate unions from configured values; Select can
also be multiple. Slug adds deterministic source generation, required/unique/indexed behavior, and
manual overrides to ordinary text storage.

### Slugs {#slugs}

```go
field.Text("title", field.Required()),
field.Slug("slug", "title"),
```

Slugs are required, unique, indexed, non-localized strings. Generated values follow the source
until an author supplies a manual value. Read [Slug](https://riducms.com/docs/fields/slug/) for ASCII
normalization and migration implications.

## Field options {#field-options}

Constructors accept narrow option interfaces, making many invalid combinations a Go compile error.
Compatibility that depends on the concrete kind—for example `Language` on anything except Code—is
reported by `ridu.Resolve` with an exact config path.

| Option                                                         | Purpose                                                                                           |
| -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `Label`, `Description`, `Placeholder` and translation variants | Author copy without changing data paths                                                           |
| `Required`                                                     | Reject missing, null, and kind-specific empty values                                              |
| `Default`, `DefaultChoices`                                    | Write supported create defaults                                                                   |
| `Unique`, `Index`                                              | Declare scalar/reference storage invariants or query indexes                                      |
| `Localized`                                                    | Store an independent content value per configured locale                                          |
| `ReadOnly`, `Hidden`, `Sidebar`, `Columns`, `Tab`              | Change admin presentation, never authorization                                                    |
| `ShowWhen`, `ShowWhenCondition`                                | Reactively display controls from typed sibling/document predicates                                |
| [`AdminComponent`](#custom-components)                         | Select an exact renderer from a statically paired plugin                                          |
| Kind-specific options                                          | Length/range, choices, relationship targets, nested fields, rows, blocks, joins, and editor hints |

`ShowWhen` is presentation only. Hidden values remain in the form and still pass through
validation, access, hooks, and writes. Use application validation/hooks for a cross-field invariant
and field access for security.

### Custom components {#custom-components}

Use `field.AdminComponent` when the stored value and server behavior of a built-in field are right,
but its authoring control is not. The selected Svelte component replaces the field editor without
changing storage, validation, access, hooks, migrations, REST, OpenAPI, or generated types:

```go title="content/posts.go"
field.Text(
	"headline",
	field.Required(),
	field.AdminComponent(
		"editorial-tools",
		"Headline",
		json.RawMessage(`{"showCharacterCount":true}`),
	),
)
```

Register the exact renderer in the statically paired admin package:

```ts title="admin/src/index.ts"
import { ADMIN_PLUGIN_API_VERSION, defineAdminPlugin, defineFieldPlugin } from '@riducms/plugin';

import HeadlineField from './headline-field.svelte';

const headlineField = defineFieldPlugin({
	type: 'text',
	key: 'editorial-tools',
	componentKey: 'Headline',
	component: HeadlineField,
	canRender: (field) =>
		field.admin.component?.plugin === 'editorial-tools' &&
		field.admin.component.component === 'Headline'
});

export const editorialToolsAdminPlugin = defineAdminPlugin({
	apiVersion: ADMIN_PLUGIN_API_VERSION,
	key: 'editorial-tools',
	pairingVersion: 1,
	fields: [headlineField]
});
```

The component receives `FieldComponentProps`: the resolved schema field, the narrow form controller,
admin translations, and an optional authoring host for document lookup, the reference browser, and
plugin requests. Read and write through `form.get(field.path)` and `form.set(field.path, value)`,
register the path, respect `field.admin.readOnly`, and render `form.issuesFor(field.path)`. Component
config is public deterministic manifest metadata, so never place secrets or executable policy in it.
The paired backend plugin and admin package must use the same plugin key, admin API version, and
pairing version; generation or startup rejects a missing or incompatible pair.

Use a [UI field](https://riducms.com/docs/fields/ui/) for presentation that stores no value. Use a
[Plugin field](https://riducms.com/docs/fields/plugin/) and the complete [custom-field guide](https://riducms.com/guides/custom-fields/)
when the value shape, validation, generated types, or query behavior is itself new.

Ridu's component surface is deliberately smaller than Payload's current field-component API:

| Authoring need                         | Ridu contract                                                                                                    |
| -------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| Replace the edit control               | `field.AdminComponent` plus an exact `FieldPlugin` renderer                                                      |
| Add presentation-only editor UI        | `field.UI` with an optional paired renderer                                                                      |
| Customize array or block row headings  | `field.RowLabelComponent` plus a registered row-label component                                                  |
| Customize a collection list cell       | Admin plugin `listCells`, scoped to an exact collection and field                                                |
| Field-specific filter or version diff  | No public custom renderer yet                                                                                    |
| Separate label/description/error slots | No independent slots; the replacement field component renders these through its schema metadata and form issues  |
| Before/after-input component slots     | No independent slots; compose the additional UI inside the replacement field component                           |
| Server-rendered admin components       | Not supported; admin extensions are statically bundled Svelte components and production has no JavaScript server |

Custom components are presentation, not authorization. Ridu evaluates visibility and field access
before rendering, but authoritative validation and access remain in Go and apply to every transport.

### Defaults and validation {#validation-and-defaults}

Built-in kinds validate through the operation engine for Local API, REST, SDK, admin, tasks, and
plugin transports. Defaults run on create, not update. Core fields do not accept an
arbitrary `Validate` callback; normalize input in `BeforeValidate`, enforce cross-field business
rules in hooks, or create a typed [Plugin field](https://riducms.com/docs/fields/plugin/) with a server validator.

## Read sibling values {#sibling-data}

A sibling is another field under the same parent. At the collection root, every other root field is
a sibling. Inside a Group, Array row, or Block, only fields in that object or row are siblings.

Use the API that matches the behavior you are implementing:

| Where you need the value   | Read it from                  | Scope                                                                        |
| -------------------------- | ----------------------------- | ---------------------------------------------------------------------------- |
| Virtual resolver           | `ctx.Document.Values["name"]` | The current root document                                                    |
| Field-access rule          | `ctx.SiblingData["name"]`     | The nearest parent object or repeated row                                    |
| Admin visibility condition | `field.Sibling("name", ...)`  | The nearest parent object or repeated row                                    |
| Collection or field hook   | `ctx.Data["name"]`            | Submitted root data; persisted update values are under `ctx.Original.Values` |

Values use Ridu's typed [`store.Value`](https://riducms.com/reference/store/value/) vocabulary. For example,
`ctx.SiblingData["membersOnly"].BooleanValue()` returns the boolean and whether the stored value is
actually a boolean; `StringValue()`, `NumberValue()`, `ObjectValue()`, and `Values()` handle the
other shapes.

See [Conditional fields](https://riducms.com/docs/fields/conditional-fields/) for `field.Sibling` paths, operators,
and compound presentation rules; [Virtual fields](https://riducms.com/docs/fields/virtual/#sibling-data) for deriving
response values; and [field access](./access-control.md#sibling-data) for row-scoped
authorization. Reading a sibling does not itself enforce a rule: conditions are presentation,
while access and hooks run on the server.

## Nested fields {#nested-fields}

Use [Group](https://riducms.com/docs/fields/group/) for one known object, [Array](https://riducms.com/docs/fields/array/) for ordered rows
with one shape, and [Blocks](https://riducms.com/docs/fields/blocks/) for an ordered union. Named
[Tabs](https://riducms.com/docs/fields/tabs/) also create an object; unnamed tabs affect only presentation.

```go
field.Group("seo", field.Fields(
	field.Text("title"),
	field.Textarea("description"),
)),
field.Array("links", field.Fields(
	field.Text("label", field.Required()),
	field.Text("url", field.Required()),
), field.MaxRows(12)),
```

Nested fields generate typed contracts, validate precise paths, support migrations and queries, and
render structured controls. Prefer them over JSON when the shape is known.

## Authoring layout {#authoring-layout}

Rows, collapsibles, unnamed tabs, and UI fields organise authors without nesting stored values.
Named tabs and Groups do create a stored boundary. `ReadOnly` and `Hidden` are also presentation,
not authorization.

```go
field.Row(
	field.Text("firstName", field.Columns(6)),
	field.Text("lastName", field.Columns(6)),
),
field.Collapsible("advanced", true,
	field.JSON("providerMetadata"),
),
```

The stored paths remain `firstName`, `lastName`, and `providerMetadata`.

## Relationships {#relationships}

[Relationship](https://riducms.com/docs/fields/relationship/) stores one/many references to one or several collection
targets. [Upload](https://riducms.com/docs/fields/upload/) is the focused reference to one upload-enabled collection.
Option filters narrow author choices and are revalidated on the server, but target read access is
still required. Population replaces stored references with access-checked target documents only
when requested.

Use [Join](https://riducms.com/docs/fields/join/) for an inverse view rather than keeping two ID lists synchronized.
See [Relationships, joins, and population](https://riducms.com/docs/relationships/) for cardinality, delete behavior,
atomic join mutation, and read limits.

## Upload and rich-text fields {#plugin-fields}

An Upload field references a media document; it does not accept raw bytes. Create the media through
the admin, multipart REST, or generated SDK upload methods. The official
[Rich-text plugin](./rich-text.md) uses a Plugin field with a portable versioned document,
Go validation, generated types, and a statically paired Svelte/Lexical editor.

## Joins and computed fields {#joins-and-virtual-fields}

Join and [Virtual](https://riducms.com/docs/fields/virtual/) values appear in generated output but never mutation
input. A Join reads inverse relationships through target access. A Virtual calls a matching trusted
`Computed` resolver and validates its result against `ValueString`, `ValueNumber`, `ValueBoolean`,
or `ValueJSON`. Both run only when selected output needs them.

## Localization {#localized-fields}

`Localized` requires application content locales. Put it on one scalar/reference when only that
value varies, or on Group/Array/Blocks when the complete structure varies per locale. Container
localization prevents fallback from mixing descendant values from different locale objects.

Admin-interface translations for labels, descriptions, choices, blocks, tabs, and row names use
their `*Translations` metadata and are independent from stored content localization. See
[Content localization](./localization.md).

## Field access and hooks {#field-access-and-hooks}

`Collection.FieldAccess` maps canonical paths to executable read/write decisions;
`Collection.FieldHooks` maps paths to lifecycle hooks. Presentation never substitutes for either.
Filtered collection access remains in the atomic store query, while field read rules redact denied
values after a document is admitted.

Use `BeforeValidate` to normalize, `BeforeChange` for transaction-bound derived stored values, and
`AfterChange` only when an external effect can safely be retried. Continue with
[Access control](./access-control.md) and [Hooks](./hooks.md).

## Evolve a field safely {#schema-evolution}

Adding an optional field is usually additive. Renames, kind/cardinality changes, removed choices or
block types, narrowed relationship targets, new required constraints, and removed fields can need a
data transform or coordinated migration. Run `ridu migrate create`, review the artifact, verify it
against an isolated target, and rehearse production data before applying it. Never use development
sync as a substitute for a deployment plan.

## Modelling patterns {#modelling-patterns}

| Need                                                    | Prefer                                                   |
| ------------------------------------------------------- | -------------------------------------------------------- |
| Short or multi-line plain text                          | Text or Textarea                                         |
| Formatted portable document                             | Official Rich text plugin                                |
| One known object / repeated one-shape rows / mixed rows | Group / Array / Blocks                                   |
| Fixed values / managed records                          | Select / Relationship                                    |
| Media reference                                         | Upload to an upload-enabled collection                   |
| Editor organisation without API nesting                 | Row, Collapsible, unnamed Tabs, Columns                  |
| Derived response value                                  | Root Virtual plus `Computed` resolver                    |
| Reusable new value kind                                 | Compiled Plugin field with exact generated/admin pairing |

Prefer the narrowest shape that represents the domain. See the individual field pages for behavior
and the [`field` API reference](https://riducms.com/reference/field/) for signatures.
