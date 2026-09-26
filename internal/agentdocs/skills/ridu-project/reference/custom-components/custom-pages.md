<!-- Generated from website/src/content/docs/custom-components/custom-pages.md by scripts/sync-agent-docs.ts. -->

# Custom admin pages

Add a page for help, reports, an importer, or another task specific to your application.
The page appears inside the admin and requires the user to sign in.

This example adds an Editorial help link that opens `/admin/help`.

## Configuration {#configuration}

| Option                                     | Required                       | What it does                                                              |
| ------------------------------------------ | ------------------------------ | ------------------------------------------------------------------------- |
| `routes[].path`                            | Yes                            | Sets a relative path below the authenticated admin; omit a leading slash. |
| `routes[].component`                       | Yes                            | Supplies the Svelte page component.                                       |
| `routes[].navigation`                      | No                             | Adds the page to navigation; omit it for link-only or redirected routes.  |
| `navigation.label`                         | Yes when navigation is present | Sets the human-readable link text.                                        |
| `navigation.labelKey` / `navigation.group` | No                             | Uses translated link copy or supplies a grouping hint.                    |

Plugin routes must also match the paths declared by their Go descriptor. Application-local routes
exist only in `admin.config.ts`.

## 1. Create the page {#component}

```svelte title="admin/src/components/editorial-help.svelte"
<h1>Editorial help</h1>
<p>
	Save your article as a draft while you work. Publish it when the
	review is complete.
</p>
<p>
	<a href="mailto:editors@example.com">Contact the editorial team</a>
</p>
```

This component does not need props. It can import other components and application code like
any other Svelte component.

## 2. Give it a URL {#register}

```ts title="admin/src/admin.config.ts" focus={7-13}
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import EditorialHelp from './components/editorial-help.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	routes: [
		{
			path: 'help',
			component: EditorialHelp,
			navigation: { label: 'Editorial help' }
		}
	]
});
```

The path is relative to the admin: `help` becomes `/admin/help` with the default admin URL.
The navigation label is the text people click to open it. Omit `navigation` when a page should
only be reachable from your own links.

Keep your other admin settings and add this route to the `routes` array. Keep `bun run dev`
running. Sign in, select Editorial help in the navigation, and reload the page to
confirm the URL works directly.

## Choose a path {#paths}

Paths are literal names such as `help` or `editorial/review`. Do not start them with `/`, add
query strings, or use router patterns such as `:id` and `*`. Choose a path outside Ridu's built-in
pages; names such as `collections`, `globals`, and `account` are reserved. Paths must also be
unique regardless of capitalization.

For a page about one document, use a [document tab](./document-views.md).
For content around an existing screen, use [List and edit views](./custom-views.md).

## Fetch and change data {#data}

For a report or another page that needs data on arrival, follow
[Load data for an admin page](./loading-data.md). A registered Go loader
reads the data, and `withAdminLoader` connects its generated reference to your Svelte page.
Your component receives a ready-to-use `data` prop and a `refresh` function.

Use the [generated SDK](../typescript-sdk.md) for writes or requests triggered later by user
interaction. The page itself runs in the browser; a loader's Go function runs on the server.

Being signed in gives access to the page, not automatic permission to every action it offers.
The API checks permissions whenever the page reads or changes data. Show request failures where
the user can understand and recover from them.

## Link to another admin page {#links}

Use `Link` from `@riducms/admin/routing` for admin destinations. Its `to` value starts at the
admin root: `/help` opens `/admin/help` with the default base path. It preserves router navigation
and unsaved-change prompts. You control the link's content and styling; it can contain a title,
icon, or an entire card. The [list layout example](./list-results.md#component)
shows it with document destinations.

Use an ordinary `<a>` for external websites and `mailto:` links.
