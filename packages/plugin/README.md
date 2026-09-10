# `@riducms/plugin`

Build Svelte components that work inside Ridu's admin. Ridu provides the document form,
validation messages, access state and tools for related documents. Your component supplies the UI.
The admin is built into static files and served by the Go application; these components run in
the browser, without a JavaScript server in production.

## Choose the API for your job

| Job | Import | Register/select it |
| --- | --- | --- |
| Add a field type with its own Go rules and structured value | `defineAdminPlugin`, `definePluginField` from `@riducms/plugin/authoring/v1` | Plugin `fields` map; matching field-type key declared in Go |
| Supply an alternative advanced field editor in a paired plugin | `defineFieldComponent` from `@riducms/plugin/authoring/v1` | Plugin `components` map; Go `.Admin(field.Admin{Editor: field.PluginComponent(owner, name, config)})` |
| Change how one application's text/number/checkbox-style field looks | `defineFieldEditor` from `@riducms/plugin/editor` | `defineAdmin({ fields })`; Go `.Admin(field.Admin{Editor: field.Component("app:name", config)})` |
| Add application routes, dashboard panels or other admin UI | `defineAdmin` from `@riducms/plugin/admin` | `admin/src/admin.config.ts` |
| Customize an application's array/block row headings | `defineRowLabel` from `@riducms/plugin/admin` | `defineAdmin({ rowLabels })`; Go `.Admin(field.Admin{RowLabel: field.Component("app:name", config)})` |
| Use the shared visual controls | `@riducms/ui` | Compose controls in your component |

Public component/host types are exported from `@riducms/plugin`. The versioned authoring import
also exports the plugin field types. Do not import private `admin/src` implementations.

## A plugin has a server half and a browser half

The **Go half** declares the plugin's field types, settings, validation and any server endpoints.
The **Svelte/TypeScript half** displays those fields and implements browser interactions.
Ridu generates the glue that checks both halves agree and chooses the correct component.

There are three separate names:

- **Plugin key**, such as `editorial-tools`: identifies the installed Go/admin pair.
- **Field-type key**, such as `review-note`: identifies a kind of value supplied by that plugin.
  One plugin can provide several types; field-type keys must be unique across installed plugins.
- **Field name**, such as `review`: identifies where a value lives in a document. Several fields
  can use the same `review-note` type, including fields inside repeatable rows.

Use `ridu plugin new` to start a paired package and `ridu plugin add` to install it into an app.
The app retains `generatedAdminPlugins` in `defineAdmin({ plugins: generatedAdminPlugins })`.
This keeps the generated Go/admin checks. Authors using explicit imports still supply an ordinary
plugin array, such as `plugins: [outlineAdminPlugin, seoAdminPlugin]`, alongside the corresponding
Go registrations and generated checks.

`pairingVersion` is a positive integer you maintain in both halves. Increase it when the Go and
admin packages can no longer work together. It is separate from your package release version.
The `/authoring/v1` import supplies Ridu's `apiVersion`; do not stamp it onto a declaration yourself.
Changing that number does not make incompatible code compatible.

## Example: a note with a “Use note as title” button

This editor stores `{ text: string }`. Its Go field settings contain `{ "copyTo": "title" }`,
meaning the button should copy into the containing document's Title field.

First define checked data/settings in `value.ts`. A decoder is simply a function that checks
unknown data, returns the shape your component expects, or throws a useful error.

```ts
export interface NoteValue {
  text: string;
}
export interface NoteConfig {
  copyTo: string;
}

export function decodeNote(raw: unknown): NoteValue {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw) ||
      !("text" in raw) || typeof raw.text !== "string" || Object.keys(raw).length !== 1) {
    throw new Error("A note must be an object containing only a text string.");
  }
  return { text: raw.text };
}

export function decodeNoteConfig(raw: unknown): NoteConfig {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw) ||
      !("copyTo" in raw) || typeof raw.copyTo !== "string" || !raw.copyTo.trim() ||
      Object.keys(raw).length !== 1) {
    throw new Error("Note settings must contain a nonempty copyTo field path.");
  }
  return { copyTo: raw.copyTo };
}
```

Then write `note-field.svelte`:

```svelte
<script lang="ts">
  import type { PluginFieldProps } from "@riducms/plugin/authoring/v1";
  import type { NoteValue, NoteConfig } from "./value";

  let { field, config, form }: PluginFieldProps<NoteValue, NoteConfig> = $props();
  // Remember this document's Title field while this note editor is open.
  // svelte-ignore state_referenced_locally
  const title = form.bind(config.copyTo);
</script>

<label for={field.schema.id}>{field.schema.admin.label}</label>
<textarea
  id={field.schema.id}
  name={field.schema.path}
  required={field.schema.required}
  readonly={field.readOnly}
  aria-invalid={field.issues.length > 0}
  aria-describedby={`${field.schema.id}-issues`}
  value={field.value?.text ?? ""}
  oninput={(event) => field.set({ text: event.currentTarget.value })}
></textarea>
<div id={`${field.schema.id}-issues`} aria-live="polite">
  {#each field.issues as issue}<p>{issue.message}</p>{/each}
</div>
<button
  type="button"
  disabled={field.readOnly || title.readOnly || !field.value?.text}
  onclick={() => title.set(field.value?.text ?? null)}
>
  Use note as title
</button>
```

Register it in `index.ts`:

```ts
import { defineAdminPlugin, definePluginField } from "@riducms/plugin/authoring/v1";
import NoteField from "./note-field.svelte";
import { decodeNote, decodeNoteConfig } from "./value";

export const editorialAdminPlugin = defineAdminPlugin({
  key: "editorial-tools",
  pairingVersion: 1,
  fields: {
    "review-note": definePluginField({
      component: NoteField,
      decodeValue: decodeNote,
      decodeConfig: decodeNoteConfig,
    }),
  },
});
```

The Go descriptor must declare the same plugin/field-type keys, admin package/export and pairing
version, plus its value types and validation. The application must actually contain a `title`
field. The config decoder above checks the setting's shape; it does not prove that path exists.
`form.bind` checks the current form when the component starts. See the
[plugin guide](https://riducms.com/docs/plugins/) for the Go registration side.

The flow is:

1. Ridu opens the document and supplies its form to your component.
2. Typing calls `field.set({ text: ... })`, changing the note in the **unsaved form**.
3. Clicking the button copies the note's current text into Title in that same form.
4. This is a one-time copy. Further typing does not update Title until another click.
5. Saving the document sends the form through Ridu's normal server permissions and validation.
   If validation fails, Ridu supplies the returned issues to the field.

Nothing in this button calls AI or saves to the database. A note inside `reviews.0.note` still
copies to the document-root `title`, because that is what its config says.

## What `form.bind` means

A **binding** is a connection to one particular field in the open form. For example:

```ts
const note = form.bind("reviews.0.note");
```

This selects the first review **at the time of the call**. If that review later moves to the
third position, `note.set(...)` still edits the same review. Keeping the text `"reviews.0.note"`
and looking it up again later would select whoever occupies the first position then.

Create bindings during component setup or before starting delayed work, and keep the returned
object in your callback. Ridu stops that connection from being used after its target is removed,
the source editor unmounts, or the document, content locale, schema or saved/reset form values
are replaced. Reading/writing then throws. `binding.stale` remains safe to inspect, and
`binding.readOnly` becomes true. Neither a timeout nor a late response may reuse that old connection
to edit a new row/document. A new mounted editor gets new bindings.

A title binding also respects the note editor's editability: the button cannot update Title while
the source note is read-only, even if Title itself is editable. The server independently checks
permissions on save.

The operation engine assigns missing `_key` IDs to every declared array and block row, including
rows created through the API. Keys are nonempty and unique within each list. Keep returned keys
when editing or reordering existing rows; document duplication assigns fresh identities.

`form.bind(path)` returns `PluginFieldBinding<unknown>`, because TypeScript cannot infer a value
shape from an arbitrary string path. Check values you read and write the shape the target field
expects. This is different from your own `field`, which has your registered decoder types.

## Values, config and validation

| API | Meaning |
| --- | --- |
| `field.value` | Latest checked form value, including unsaved edits. Objects/arrays are copies. |
| `field.rawValue` | Copied data before decoding, useful for recovery UI when old data is unsupported. |
| `field.set(next)` | Replace this value in the normal unsaved form. Use `null` to clear; undefined is rejected. |
| `field.issues` | Current form/server validation issues for the field and its children. |
| `form.get(path)` | Copy a value at its current document-root path; does not remember a row's identity. |
| `form.snapshot()` | Copy current form values once. It is neither live state nor a server fetch. |

Changing an object returned by a read does not edit the form. Call `set` with a replacement value.
Ridu copies values supplied to `set` too, so mutating your original object afterward does not
silently change saved or unsaved form data.

`PluginFieldProps<Value, Config, Type, Input>` uses `Input = Value` by default. When the server
accepts a different write shape from its returned data, supply `decodeInput` too. Until a save
returns normalized data, `field.value` can contain either checked `Value` or checked `Input`.
The host tries the output decoder first, then the supplied input decoder. Do not assume it has
invented server-generated properties for an unsaved edit. Null/undefined reads are empty states
handled separately from the decoders.

Decoders must be synchronous and safe to run repeatedly. Value data must be plain JSON-shaped
data: no class instances, functions, cycles or non-finite numbers. Config arrives as serialized
Go settings and must be checked before it becomes typed component data. For a selected component,
omit the Go configuration argument when there are no settings; no decoder or `config` prop is needed.
Every supplied object, including `{}`, requires `decodeConfig`, and a declared decoder requires an
object. Plugin-owned value configuration keeps its descriptor defaults and may use an empty
object without a decoder.

Let registration helpers infer the result. Their types check component props against decoder
results; generated TypeScript checks the declared saved/write types against the Go descriptor.
Type annotations do not execute validation, and `any`/assertions can bypass static checks. A
poorly written decoder can still accept invalid data. Go validation and authorization remain the
final checks on saving; UI customization does not change storage, filtering or operation rules.

## Related documents and server requests

`authoring` supplies Ridu's tools to a field component:

- `collections`: available collection definitions, not the documents themselves.
- `locale`: content locale, separate from the admin interface language.
- `documentRevision`: an admin change counter for refreshing UI, not a saved `_revision` number.
- `findDocument(collection, id, signal?)`: fetch a saved document in the current locale. It does
  not read the current form's unsaved changes.
- `referenceBrowser`: the related-document picker/editor. Render it and update your field in
  `onCommit(ids)`. Return false to keep it open; otherwise it closes after acceptance. Saving a
  related document inside the picker is a separate server operation.
- `requestPlugin<Result>(path, body, signal?)`: POST to this renderer's paired Go plugin endpoint,
  using a relative path such as `generate-title`. The response generic does not validate data;
  request `unknown` and decode it when needed. This does not update your form automatically.

Async authoring calls check that the field is still usable when they complete. An expired field
rejects the result. This cannot undo server work already dispatched, and it does not choose
between two requests made while the same field stays open. Your component must handle errors,
cancel unnecessary requests and prevent an older response overwriting a newer result.

## Ordinary fields inside a structured plugin value

A plugin such as rich text can contain cards with ordinary Ridu fields inside them. The Go field
must declare those embedded trees, cases and variants. Here, **payload** means the ordinary field
data inside one such item, not the whole plugin value or the Payload CMS product.

To edit an existing item's fields directly in the parent form, render:

```svelte
{#if selectedIdentity !== undefined && authoring.schemaForm !== undefined}
  {@render authoring.schemaForm({ treeKey: "widgets", identity: selectedIdentity })}
{/if}
```

The plugin provides `selectedIdentity` from its own selection UI. `treeKey` matches the Go-declared
tree, and `identity` identifies the existing item. Ridu finds its current position and renders its
ordinary fields. Edits immediately enter the parent form; this does not save the document.
Removed/malformed items show recovery UI rather than another item's fields.

For **Apply/Cancel**, use a temporary embedded draft:

1. Call `beginSchemaDraft({ treeKey, identity })` for an existing item, or
   `beginSchemaDraft({ treeKey, caseTag, variantSlug })` to prepare a new item with field defaults.
2. Render `schemaDraftEditor({ draft, title, onApply, onCancel })` as a Svelte snippet.
3. `onApply(payload)` receives checked field data. Update your plugin/editor state and serialize
   it through `field.set`; Ridu does not insert the item into your value for you. Close the drawer
   in your Apply/Cancel callbacks. Apply/Cancel releases the draft.
4. Call `discard()` if you abandon a draft outside the drawer. Closing the field also cleans it up.

These drafts are temporary forms, not saved document versions. They reuse Ridu's form controller
and cannot save independently. Pending insertions or dirty drafts block outer Save until the user
applies/cancels them. Apply checks declared field rules; executable Go validators run on parent save.

Drafts follow the same item through reorder. Removal, changed source data, document/locale/schema
or relevant access changes expire them; reopen instead of applying old data.
`schemaIssues({ treeKey, identity })` supplies current issues for an item's badge.
`copySchemaPayload({ treeKey, caseTag, variantSlug }, payload)` copies ordinary field data with
fresh IDs for schema-declared nested rows/items. Your plugin still manages the copied outer item.
The host interprets only declared schema structure, not arbitrary JSON with similar property names.

See the [Outline fixture](../../tests/contracts/admin_app/outline-field.svelte) for a compact
embedded-form example and the [rich-text package](../plugin-richtext/) for an editor integration.

## Other admin UI

Both paired plugins and `defineAdmin` can register `routes`, `dashboard`, `login`, `account`,
`navigation`, `logoutButton`, `views`, `branding`, `shell`, `providers`, `listCells`,
`documentActions` and `documentViews`.

Replacement login/account/navigation/core-view components receive a `defaultView` snippet.
Render `{@render defaultView()}` to keep the normal screen inside your wrapper. Providers must
render their `defaultView` to include the nested admin. Host methods perform login/logout,
refreshes and notifications; application-specific operations use the generated SDK.

Collection/global view replacements may target one resource or act as a fallback. An exact
resource match wins over its fallback; duplicate targets and exclusive replacements fail.
Table cells and document action/view props contain document data, not a field-form binding.
Use their hosts to refresh after your operation; `refresh()` fetches data, it does not save edits.

Paired plugins register row headings with `defineRowLabelPlugin({ key, componentKey, component })`
and Go `.Admin(field.Admin{RowLabel: field.PluginComponent(key, componentKey, config)})`. The heading
receives a copied, deeply frozen `row` and a 1-based visual `rowNumber`. Its config is unknown: validate it inside the component. Application-local
headings use `defineRowLabel`, adding a config decoder only when settings are supplied, and select
`.Admin(field.Admin{RowLabel: field.Component("app:name", config)})` in Go. Use
`field.Admin{RowLabelPath: path}` for array headings based on one child field.

Use `defineAdminMessages` for interface messages. Catalog keys have no prefix; refer to them as
`plugin.editorial-tools:messageName` for that plugin or `app:messageName` for the application.

## Local field editors and visual wrappers

Local editors support string, number, boolean, text-list, and number-list value shapes.
`FieldEditorProps<"text-list">` uses `string[]`; `FieldEditorProps<"number-list">` uses `number[]`.
Lists remain one field occurrence and arrays are copied on reads and writes. The host rejects
mixed types, non-finite numbers, and null elements at runtime. A built-in numeric control can retain
unfinished input in its form draft; a custom binding rejects such values instead of claiming they
are `number[]`.

```ts
const editor = defineFieldEditor({ type: "text-list", component: SellingPoints });
// SellingPoints.svelte receives FieldEditorProps<"text-list"> and can call:
// field.set([...(field.value ?? []), "Solid oak"]);
```

Application-local editors support text, textarea, email, date, code, number and checkbox fields.
They receive `FieldEditorProps<Type, Config>` and their own `field.set`, plus document reads and
related-document browsing/lookup. They cannot bind another field for writing or call plugin
endpoints, and do not expose embedded draft forms. Advanced `AdminComponent` renderers stay on
the paired-plugin API. Packaged distribution of application-local components is still deferred.

`Field` from `@riducms/plugin/editor/field` is an optional wrapper for a local field editor's label,
description and issues. `FieldFrame` from `@riducms/ui` is the underlying visual component, also
usable by plugin editors. Neither owns form values or registration. Your input still supplies its
value, read-only state and change handler. Importing only editor types does not import this UI.

## Checks and further reading

Run your plugin's TypeScript/Svelte checks, then the application's `ridu check` and production
`ridu build`. Ridu checks actual registrations against the resolved Go schema, including field
selection, decoder settings, duplicate registrations and Go/admin compatibility. These checks
compile with the application's production Vite configuration without starting a browser or dev
server. They do not prove every future field value or arbitrary callback is correct: test those
interactions as well.

For custom UI, use the documented editor contracts and run the checks above in the consuming
application so its Svelte and TypeScript configuration checks the complete integration.

- [Plugin fields and editors](https://riducms.com/docs/fields/plugin/)
- [Building plugins](https://riducms.com/docs/plugins/)
- [Application-local field components](https://riducms.com/docs/custom-components/field-components/)
