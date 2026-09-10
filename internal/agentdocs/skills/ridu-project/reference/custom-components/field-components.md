<!-- Generated from website/src/content/docs/custom-components/field-components.md by scripts/sync-agent-docs.ts. -->

# Custom field components

A custom field component changes the control people use to edit a field. For example, a text
field can show a character counter, a color preview, or a button that fills in a suggested value.
The field still stores text, and its Go validation rules still apply when someone saves.

This tutorial adds a character counter below an article's title. Start with an existing Ridu
application and its `posts` collection.

## Configuration {#configuration}

| Option or prop                                       | Required                   | What it does                                                                                |
| ---------------------------------------------------- | -------------------------- | ------------------------------------------------------------------------------------------- |
| `defineFieldEditor({ type, component })`             | Yes                        | Registers a Svelte editor for one supported built-in field type.                            |
| `decodeConfig(value)`                                | When Go supplies settings  | Validates unknown JSON synchronously and returns typed component settings.                  |
| `fields['app:name']`                                 | Yes                        | Gives the local editor the same key selected in Go.                                         |
| `field.Admin.Editor`                                 | Yes on each selected field | Uses `field.Component("app:name")`, with optional JSON-safe settings.                       |
| `FieldEditorProps.field.value` / `.set(value)`       | Supplied by Ridu           | Reads and updates this field's unsaved form value.                                          |
| `field.inputProps`, `field.readOnly`, `field.issues` | Supplied by Ridu           | Connects labels/errors, enforces read-only UI, and exposes validation feedback.             |
| `field.liveValidation` / `field.stale`               | Supplied by Ridu           | Reports server-check state and whether an asynchronous result still belongs to this editor. |

Supported types are text, textarea, email, date, code, number, checkbox, text list, and number list.

## 1. Create the component {#component}

Create `admin/src/components/title-field.svelte`:

```svelte title="admin/src/components/title-field.svelte" focus={10-17}
<script lang="ts">
	import type { FieldEditorProps } from '@riducms/plugin/editor';
	import Field from '@riducms/plugin/editor/field';
	import { Input } from '@riducms/ui';

	// Read and update the current form value through Ridu.
	let { field }: FieldEditorProps<'text'> = $props();
</script>

<!-- Field connects the input to its label and validation messages. -->
<Field {field}>
	<Input
		{...field.inputProps}
		value={field.value ?? ''}
		readonly={field.readOnly}
		oninput={(event) => field.set(event.currentTarget.value)}
	/>
	<p>{(field.value ?? '').length} characters</p>
</Field>
```

Ridu passes the current field to your component as a prop. `field.value` contains what the user
has typed, including changes they have not saved. `field.set(...)` updates that value in the
form; the normal Save button saves the document.

`Field` adds the label, description, and validation messages. Spreading `field.inputProps` onto
the input connects it to that label and those messages. `readonly={field.readOnly}` makes the
input respect the field's current editing permissions.

The character count reads the same value as the input, so it updates as the user types. There is
no second copy of the title to keep in sync.

## 2. Register the component {#register}

In `admin/src/admin.config.ts`, import the component and add it to `fields`:

```ts title="admin/src/admin.config.ts" focus={8-14}
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import { defineFieldEditor } from '@riducms/plugin/editor';
import TitleField from './components/title-field.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	fields: {
		// Use this name in the Go field's Editor option.
		'app:titleCounter': defineFieldEditor({
			type: 'text',
			component: TitleField
		})
	}
});
```

`app:titleCounter` is the name you will use in Go. The `app:` prefix means this component belongs
to your application. The rest of the name starts with a letter and can contain letters, numbers,
and underscores.

`type: 'text'` tells Ridu which kind of field this component edits. It matches
`FieldEditorProps<"text">` in the component.

[`defineFieldEditor`](https://riducms.com/reference/plugin/define-field-editor/) returns the registration stored in
the `fields` map. [`defineAdmin`](https://riducms.com/reference/plugin/define-admin/) collects that registration with
the rest of your admin settings. Neither helper renders the component here; Ridu renders it when
an editor opens a Go field that selects its name.

The helper reference lists every option, including how to pass per-field settings with
`decodeConfig`. [FieldEditorProps](https://riducms.com/reference/plugin/field-editor-props/) documents the value,
validation messages, form reads, and other props your component receives.

## 3. Choose it for a field {#select}

Set `Admin.Editor` to `field.Component("app:titleCounter")`. For example:

```go title="content/posts.go" focus={15-19}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Posts = ridu.Collection{
	Slug: "posts",
	Admin: ridu.CollectionAdmin{
		UseAsTitle:     "title",
		DefaultColumns: []string{"title", "readingMinutes"},
	},
	Fields: field.Fields{
		field.Text("title").
			Required().
			Admin(field.Admin{
				Editor: field.Component("app:titleCounter"),
			}),
		field.Number("readingMinutes"),
	},
}
```

If you are adding this collection for the first time, include `content.Posts` in your Go config's
`Collections` list, as shown in the [Quickstart](../quickstart.md). If Posts already exists, keep
its other fields and options and configure `Admin.Editor` on its title.

Add `Editor` to the field's existing `field.Admin` settings if it already has any. Calling
`.Admin(...)` a second time replaces all of those settings, including its description and width.

## 4. Try it {#try-it}

Keep the development server running. If it is not already running, start it from the project root:

```sh title="terminal"
bun run dev
```

This runs `ridu dev`, which regenerates the schema when your Go fields change and reloads the
admin when you edit components or their registration. You do not need to run `ridu generate`
separately or restart the development server for these changes.

Open Posts and create or edit a document. Type a title: the character count should update
immediately. Save, then reload the page; the title and count should still match. Clearing a
required title and saving should display the usual validation message.

Run `bun run check` when you are ready to validate your work before committing or building.
It is a separate check, not a prerequisite for trying the component.

The same component can be selected on text fields in groups, arrays, and blocks. It follows its
row when the row is reordered.

## Show server feedback while editing {#live-validation}

Add [`.LiveValidate(...)`](../fields/live-validation.md) to the Go field to enable server
checks while editing. Updating through `field.set(...)` starts the check; the `Field` wrapper
displays returned issues, and the surrounding admin handles checking and retry messages.

Your component can read `field.liveValidation.status` when it needs to react to a pending or
failed check. Ridu handles the requests and cancellation. Keep `.Validate(...)` registered when
the rule must also prevent an invalid save.

## Other field types {#field-types}

Application field components currently support text, textarea, email, date, code, number, and
checkbox fields. Match the Go field, the registration's `type`, and `FieldEditorProps`.
A number component reads and writes numbers; a checkbox component reads and writes booleans.
The other supported types use strings. Use `field.set(null)` to clear a value.

For controls without an HTML read-only mode, such as checkboxes, use
`disabled={field.readOnly}`. Ridu also checks whether editing is allowed when `field.set` is called.

To change an array row's heading, use [Row labels](./row-labels.md).
Selects, relationships, uploads, JSON, and rich text cannot use a component registered this way.
Their custom editors need a [plugin](../custom-fields.md).

## Read another field or wait for an API response {#other-values}

The component can also receive `form`. For example, `form.get('subtitle')` reads the current
subtitle, including unsaved edits. This interface reads other fields; use `field.set` to change
the field your component edits.

For a suggestion button that waits for an API response, keep the `field` object you started
with and check `field.stale` before applying the result. It becomes stale if the user closes the
editor, removes the row, changes document or locale, or saves or resets the form. Discard that
result instead of writing it into the new form.

For reusable settings supplied from Go, see [field.Component](https://riducms.com/reference/field/component/) and
[defineFieldEditor](https://riducms.com/reference/plugin/define-field-editor/). Settings are checked with a
`decodeConfig` function before they reach your component.
