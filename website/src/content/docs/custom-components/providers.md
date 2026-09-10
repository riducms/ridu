---
title: 'Share settings between components'
description: 'Share settings or Svelte context between your admin components.'
product: admin
order: 160
eyebrow: 'Custom components'
aliases:
  ['custom provider', 'Svelte context', 'shared component settings']
navigation:
  section: 'Admin & workflows'
  parent: custom-components
  order: 100
  title: 'Shared settings'
---

When several components need the same settings or state, you can share them through Svelte
context. A **provider** is a component that sets up that context and renders the admin inside it.
Other components can then read the shared values without passing them through every component
in between.

You do not need a provider to register a field, dashboard panel, or custom page. Start with the
relevant component guide unless you have something to share between components.

This example shares a support email address with a navigation link.

## Configuration {#configuration}

| Option                           | Required                  | What it does                                                                   |
| -------------------------------- | ------------------------- | ------------------------------------------------------------------------------ |
| `providers[].key`                | Yes                       | Gives this provider a unique registration name.                                |
| `providers[].component`          | Yes                       | Supplies a Svelte component that sets context and renders `defaultView`.       |
| `AdminProviderProps.defaultView` | Yes to continue rendering | Contains the next provider or the admin application.                           |
| `AdminProviderProps.i18n`        | No                        | Gives the provider the current interface translations and formatting settings. |

Providers run in registration order: installed plugins first, then application entries.

## 1. Define the shared setting {#context}

Svelte's `createContext` returns two functions: one reads the value and the other provides it.
Put them in a module both components can import:

```ts title="admin/src/support-context.ts"
import { createContext } from 'svelte';

// Both functions share the same typed Svelte context.
export const [getSupport, setSupport] = createContext<{
	email: string;
}>();
```

## 2. Set the support email {#provider}

```svelte title="admin/src/components/support-provider.svelte" focus={6-7,10}
<script lang="ts">
	import type { AdminProviderProps } from '@riducms/plugin';
	import { setSupport } from '../support-context';

	let { defaultView }: AdminProviderProps = $props();
	// Set context during setup, before child components read it.
	setSupport({ email: 'editors@example.com' });
</script>

{@render defaultView()}
```

Call `setSupport` while the component is being created. The `defaultView` snippet is the admin
inside your provider. You must render it or the admin will not appear.

## 3. Read it in another component {#consumer}

```svelte title="admin/src/components/context-support-link.svelte" focus={6-7,11}
<script lang="ts">
	import type { AdminNavigationComponentProps } from '@riducms/plugin';
	import { getSupport } from '../support-context';

	let { user }: AdminNavigationComponentProps = $props();
	// Read the value supplied by SupportProvider above this component.
	const support = getSupport();
</script>

{#if user}
	<a href={`mailto:${support.email}`}>Contact the editorial team</a>
{/if}
```

Call `getSupport` during the component's setup. It returns the value from the surrounding
provider. This example shares a fixed setting; use Svelte's normal reactive state when the
shared value needs to change.

## 4. Register both components {#register}

```ts title="admin/src/admin.config.ts" focus={8-15}
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import SupportProvider from './components/support-provider.svelte';
import ContextSupportLink from './components/context-support-link.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	providers: [{ key: 'support', component: SupportProvider }],
	navigation: [
		{
			key: 'support-link',
			component: ContextSupportLink,
			position: 'afterLinks'
		}
	]
});
```

Keep `bun run dev` running, then sign in. The contact link should appear below the
navigation links and use the email address supplied by the provider.

Providers wrap in registration order, with the first provider on the outside. Providers from
installed plugins wrap before your application providers. Register a provider that another
provider depends on earlier in the list.

Provider components receive `i18n` as well as `defaultView`. They wrap the admin before a user
necessarily signs in, so do not assume a user or saved document is available in their setup.
