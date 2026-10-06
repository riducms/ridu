<!-- Generated from website/src/content/docs/custom-components/loading-data.md by scripts/sync-agent-docs.ts. -->

# Load data for an admin page

Use a **loader** when a custom page needs server data before it can show useful content.
You write the read in Go, then connect it to a Svelte component. Ridu supplies the result as
the component's `data` prop; you do not fetch the same data again when the component mounts.

This example adds a Post report showing how many posts match a title search. It assumes an
existing application with `users` and `posts` collections, as in the [Quickstart](../quickstart.md).

If you only want cards instead of the built-in collection table, use
[custom list results](./list-results.md). That component already receives
the current page of documents and does not need a loader.

## 1. Write the read in Go {#loader}

```go title="content/admin_data.go" focus={18,29,35}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
)

type PostSummaryInput struct {
	Search string `json:"q"`
}

type PostSummaryData struct {
	Total  int    `json:"total"`
	Search string `json:"search"`
	Locale string `json:"locale"`
}

var PostSummary = ridu.NewAdminLoader("post-summary", loadPostSummary)

func loadPostSummary(
	ctx ridu.AdminLoadContext,
	input PostSummaryInput,
) (PostSummaryData, error) {
	// We need the count, not a page full of post content.
	options := ridu.ListOptions{Limit: 1}
	if input.Search != "" {
		options.Where = query.Contains("title", input.Search)
	}
	posts, err := ctx.List("posts", options)
	if err != nil {
		return PostSummaryData{}, err
	}
	return PostSummaryData{
		Total: *posts.Total, Search: input.Search,
		Locale: string(ctx.Locale()),
	}, nil
}
```

[`NewAdminLoader`](https://riducms.com/reference/ridu/new-admin-loader/) gives the function the key
`post-summary`. You will use that same key in TypeScript. Choose a unique key of at most
80 characters: start with a lowercase letter, then use lowercase letters, digits, and
single hyphens between words.

The `json:"q"` tag means the URL's `?q=Welcome` becomes `input.Search`.
Without `q`, the search is empty and the function counts all readable posts.
`posts.Total` points to the number of matches, not the size of the one-document page. It is
nil only when a list sets `SkipTotal`, so this counted read can dereference it.

[`ctx.List`](https://riducms.com/reference/core/admin-load-context-list-method/) reads as the signed-in user
in the current content locale, using the application's configured fallback locales.
Loader reads cannot switch to exact-locale or all-locales mode. Collection permissions and
field access rules still apply.
See [Queries](../go-packages/query.md) for building more filters.

## 2. Register the loader {#register-go}

Add `PostSummary` to `Admin.Loaders` in your existing Go config:

```go title="content/config.go" focus={10}
package content

import "github.com/riducms/ridu"

func Config() ridu.Config {
	return ridu.Config{
		Name: "Editorial",
		Admin: ridu.AdminConfig{
			User:    "users",
			Loaders: []ridu.AdminLoaderDefinition{PostSummary},
		},
		Collections: []ridu.Collection{Users, Posts},
	}
}
```

Keep your other collections, plugins, and admin settings. Add to an existing `Loaders` slice
rather than replacing loaders you already use.

With `bun run dev` running, Go changes regenerate your application contracts.
If you are not running the development server, run:

```sh title="terminal"
ridu generate
```

The generated `generated/ridu.generated.ts` now exports `adminLoaders['post-summary']`
and `AdminLoaderData['post-summary']`. Do not copy the Go result shape into a handwritten
TypeScript interface: the generated type changes when you change the Go struct.

## 3. Render the result {#component}

```svelte title="admin/src/components/post-summary.svelte" focus={10-14,17-23,33-35}
<script lang="ts">
	import type { AdminLoaderProps } from '@riducms/plugin';
	import { Link } from '@riducms/admin/routing';
	import { Button } from '@riducms/ui';
	import {
		adminLoaders,
		type AdminLoaderData
	} from '../../../generated/ridu.generated';

	let {
		data,
		refresh,
		refreshing
	}: AdminLoaderProps<AdminLoaderData['post-summary']> = $props();

	function filterLink(q: string) {
		const query = new URLSearchParams(
			adminLoaders['post-summary'].query({ q })
		);
		// Keep the selected content language when changing the search.
		if (data.locale) query.set('locale', data.locale);
		return `?${query}`;
	}
</script>

<section class="space-y-4 p-6" aria-label="Post summary">
	<h2 class="text-xl font-semibold">Post summary</h2>
	<p>{data.total} posts match {data.search || 'all titles'}.</p>
	<nav aria-label="Filter post summary" class="flex gap-4">
		<Link to={filterLink('')}>All posts</Link>
		<Link to={filterLink('Welcome')}>Welcome posts</Link>
	</nav>
	<Button variant="outline" disabled={refreshing} onclick={refresh}>
		{refreshing ? 'Refreshing…' : 'Refresh count'}
	</Button>
</section>
```

There is no mount-time request or initial loading flag here. `data` is supplied by Ridu.
[`AdminLoaderProps`](https://riducms.com/reference/plugin/admin-loader-props/) also gives you `refresh`
and `refreshing`: the button runs the same Go function again while leaving the previous
count visible.

The two links change this page's query string. `query({ q })` encodes only the loader's
input. `filterLink` also keeps the content locale returned by Go, so filtering does not
switch languages. `Link` from `@riducms/admin/routing` performs admin navigation without
a full page reload. Back and Forward restore the matching results.

## 4. Add the page {#register-page}

```ts title="admin/src/admin.config.ts" focus={12-14}
import { defineAdmin } from '@riducms/plugin/admin';
import { withAdminLoader } from '@riducms/plugin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import { adminLoaders } from '../../generated/ridu.generated';
import PostSummary from './components/post-summary.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	routes: [
		{
			path: 'post-report',
			navigation: { label: 'Post report' },
			...withAdminLoader(adminLoaders['post-summary'], PostSummary)
		}
	]
});
```

[`withAdminLoader`](https://riducms.com/reference/plugin/with-admin-loader/) connects the generated loader
reference and the component. The `...` spreads that pair into the route registration.
TypeScript checks that the component's `data` type matches the Go function's result.

Sign in and choose **Post report** in the sidebar. You should see a count. With a post titled
“Welcome to the site”, **Welcome posts** narrows the count; **All posts** clears the search.
Reload either URL to get the same result. If there are no matching posts, the count is zero.

## Put the same data on the dashboard {#dashboard}

After creating the loader and component above, use a dashboard registration instead of the
custom route:

```ts title="admin/src/admin.config.ts" source="admin/src/post-dashboard.config.ts" focus={12-14}
import { defineAdmin } from '@riducms/plugin/admin';
import { withAdminLoader } from '@riducms/plugin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import { adminLoaders } from '../../generated/ridu.generated';
import PostSummary from './components/post-summary.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	dashboardPanels: [
		{
			key: 'post-summary',
			position: 'before',
			...withAdminLoader(adminLoaders['post-summary'], PostSummary)
		}
	]
});
```

Use this as your `admin/src/admin.config.ts`, or add its dashboard entry to your current config.
`before` keeps the normal dashboard below the panel. Use `replace` to make your component
the entire dashboard. See [Dashboard](./dashboard.md).

A [whole-screen replacement](./custom-views.md#replace) uses the same
`withAdminLoader` pairing in a `coreViews` entry.

## Refresh, errors, and changing URLs {#refresh}

- Use `refresh()` after an action when the displayed data needs updating. It rereads the
  current URL; it does not take new filter arguments.
- Put filters in the URL. Path or query changes run the selected loader again.
- Return an error from Go when the read fails. Ridu shows an error with a retry control.
  A failed refresh keeps the last successful data visible.
- Use the [generated SDK](../typescript-sdk.md) or your own authenticated Go endpoint
  for writes. Loaders are for reads: do not send emails, create documents, or acquire locks
  inside one.

Keep using ordinary SDK requests for work that starts after a button click or opening a dialog.
A loader is not required for every request.

## Choose a supported screen {#screens}

Loaders work on dashboard panels, custom pages, and replacement views for collection lists,
document creation, document editing, globals, and not-found pages. Plugins can use these same
registrations; see [Add a loader to a plugin](../plugins.md#admin-loaders).

They do not attach to individual fields, table cells, document tabs, account/login components,
or shell slots. Those components use their existing props and browser requests.

A replacement owns the data it asks for. Rendering `defaultView()` inside it still starts
the built-in screen's own requests; a custom loader does not provide data to that built-in form.

## Keep the result small and safe {#result}

Return only what the component needs. Use structs with explicit JSON tags, strings, booleans,
numbers, pointers, arrays, and slices. A `time.Time` output becomes a string. Maps,
`any`, recursive types, and custom JSON encoders are not supported. Generation reports
unsupported types. Query inputs are scalar fields; use a pointer when you need to distinguish
a missing parameter from a zero value.

Everything you return is sent to the browser. Do not include credentials or server secrets.
For external services, use `ctx.Context` for cancellation and enforce your application's
access rules; Ridu cannot apply collection permissions to a third-party service for you.

In a built admin, Go prepares the selected page's data while the browser downloads its code.
The first load shows the themed background until the page can appear; navigation keeps the
previous page visible while the next one loads. Svelte still renders in the browser—there is
no server-side JavaScript runtime.

Preparation is an optimization, not a reason to return an entire database. If the complete
initial snapshot exceeds 2 MiB, or preparation is unavailable, Ridu uses its browser loading
path instead. Keep large tables paginated and load interaction-only data when it is needed.
