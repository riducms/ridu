<!-- Generated from website/src/content/docs/custom-components.md by scripts/sync-agent-docs.ts. -->

# Custom components

Use your own Svelte components to make the admin fit the people who use it. Add a character
counter to a title field, show a reading-time badge in a table, or build a page with instructions
for your editors.

For these changes, create a component and import it in `admin/src/admin.config.ts`. You can use
components from your application directly. You do not need to build a plugin.

## Choose what to customize {#choose}

| I want to…                                       | Start here                                                                  |
| ------------------------------------------------ | --------------------------------------------------------------------------- |
| Change an input or add information beside it     | [Field components](./custom-components/field-components.md)               |
| Change the heading of an array or block row      | [Row labels](./custom-components/row-labels.md)                           |
| Change how a value appears in a collection table | [Table cells](./custom-components/list-cells.md)                          |
| Add a panel to the dashboard                     | [Dashboard](./custom-components/dashboard.md)                             |
| Add a page with its own URL and navigation link  | [Custom pages](./custom-components/custom-pages.md)                       |
| Add a tab beside Edit and API                    | [Document tabs](./custom-components/document-views.md)                    |
| Add a button beside the document actions         | [Document buttons](./custom-components/document-actions.md)               |
| Add content around a list or document editor     | [List and edit views](./custom-components/custom-views.md)                |
| Change the logo, navigation, or login screen     | [Branding and navigation](./custom-components/branding-and-navigation.md) |
| Share settings between my components             | [Shared settings](./custom-components/providers.md)                       |

## Add your first component {#first-component}

This example adds a welcome message above the dashboard. Start with an existing Ridu application
from the [Quickstart](./quickstart.md).

Create `admin/src/components/welcome-panel.svelte`:

```svelte title="admin/src/components/welcome-panel.svelte"
<script lang="ts">
	import type { AdminDashboardPanelProps } from '@riducms/plugin';

	let { user }: AdminDashboardPanelProps = $props();
</script>

<h2>Welcome to Acme Studio</h2>
{#if typeof user?.email === 'string'}
	<p>Signed in as {user.email}.</p>
{/if}
<p>
	Start with a draft. Ask the editorial team for a review before
	publishing.
</p>
```

The script receives the signed-in `user` from Ridu. The message also shows their email when it
is available. `$props()` is how a Svelte 5 component reads the values passed to it.

Import it in `admin/src/admin.config.ts` and add it to `dashboard`. If this file already contains
other settings, keep them and add the new import and dashboard entry.

```ts title="admin/src/admin.config.ts" focus={8-10}
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import WelcomePanel from './components/welcome-panel.svelte';

export default defineAdmin({
	// Include admin components from the project's installed plugins.
	plugins: generatedAdminPlugins,
	dashboard: [
		{ key: 'welcome', component: WelcomePanel, position: 'before' }
	]
});
```

Keep `plugins: generatedAdminPlugins`: that line loads any plugins your application uses, such
as rich text. `key: 'welcome'` names this panel; choose a different key for each panel you add.
`position: 'before'` puts it above the built-in dashboard.

The commands in these guides use Bun. If your project uses another package manager, use its
equivalent scripts. From the project root, start `bun run dev` if it is not already running,
then sign in at `/admin`. You should see the welcome message above the usual dashboard.

Keep that command running while you work through these guides. It runs `ridu dev`, which
regenerates the schema when you change Go fields and reloads the admin when you edit Svelte
components or their registration. There is no separate generation step for this development loop.

Run `bun run check` when you are ready to validate your work before committing or building.
It checks your components and registrations, along with the rest of your application; you do
not need to run it before trying each change in the admin.

## Which registration helper should I use? {#registration-helpers}

A registration helper connects a component to the part of the admin that will render it.
The helpers below do different jobs; you do not need all of them for a customization.

| What you are building                                             | Helper                                                            | Where you use it                                          |
| ----------------------------------------------------------------- | ----------------------------------------------------------------- | --------------------------------------------------------- |
| Your application's admin configuration                            | [defineAdmin](https://riducms.com/reference/plugin/define-admin/)                    | Export it from `admin/src/admin.config.ts`.               |
| A different input for an existing text, number, or checkbox field | [defineFieldEditor](https://riducms.com/reference/plugin/define-field-editor/)       | Add its result to `defineAdmin`'s `fields` map.           |
| A summary in an array or block row's heading                      | [defineRowLabel](https://riducms.com/reference/plugin/define-row-label/)             | Add its result to `defineAdmin`'s `rowLabels` map.        |
| An admin package shipped with a Go plugin                         | [defineAdminPlugin](https://riducms.com/reference/plugin/define-admin-plugin/)       | Export it from the plugin's JavaScript package.           |
| The editor for a new Go field type                                | [definePluginField](https://riducms.com/reference/plugin/define-plugin-field/)       | Add its result to `defineAdminPlugin`'s `fields` map.     |
| An alternative editor supplied by a plugin                        | [defineFieldComponent](https://riducms.com/reference/plugin/define-field-component/) | Add its result to `defineAdminPlugin`'s `components` map. |

For most application changes, start with `defineAdmin` and the tutorial for the component you
want to add. Use the plugin helpers when you are packaging UI alongside Go behavior. The
[field plugin guide](./custom-fields.md) shows that complete workflow.

Each reference page explains the helper's options, what it returns, and how it connects to Go.
The field props references show what your Svelte component receives:
[FieldEditorProps](https://riducms.com/reference/plugin/field-editor-props/) for an application input and
[PluginFieldProps](https://riducms.com/reference/plugin/plugin-field-props/) for a plugin editor.

## Where your code runs {#where-code-runs}

Admin components run in the browser. They use Svelte 5, and their TypeScript is bundled with the
admin when you build the application. Use the [generated SDK](./typescript-sdk.md) when a
component needs to read or change server data. The API checks the signed-in user's permissions.
See [Admin performance](https://riducms.com/docs/performance/admin/) before adding a large browser dependency or doing
network work from a component that repeats for every row or table cell.

A component may receive **props**: values Ridu passes to it, such as the current document or
field. Each guide shows the props its example needs. You can use your own styling and import
reusable controls such as `Input` and `Button` from `@riducms/ui`.

## Coming from Payload {#from-payload}

If you have used [Payload’s custom components](https://payloadcms.com/docs/custom-components/overview),
the workflow is familiar: write a component and tell the admin where to use it. In Ridu you
write Svelte instead of React and import the component directly in `admin.config.ts`.
For a custom input, register the component under a name such as `app:titleCounter`, then use that
name in the Go field's `Admin.Editor` setting. The [field component tutorial](./custom-components/field-components.md)
shows both files.

Ridu's admin components run in the browser, so they cannot call the Go local API directly.
A new field type with its own server validation or data format needs a
[field plugin](./custom-fields.md). Start with a field component when you only need to
change how an existing field is edited.

For the full list of configuration options, see the
[admin configuration reference](https://riducms.com/reference/plugin/).
