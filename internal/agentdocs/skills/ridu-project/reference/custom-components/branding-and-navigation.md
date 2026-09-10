<!-- Generated from website/src/content/docs/custom-components/branding-and-navigation.md by scripts/sync-agent-docs.ts. -->

# Branding and navigation

You can customize the parts of the admin that surround your content: the logo, navigation,
login screen, account screens, and header.

This example uses a text logo, adds an email link below the navigation links, and shows a short
message above the login form.

## Configuration {#configuration}

| Registration   | Required options                                   | Positions or surfaces                                                               |
| -------------- | -------------------------------------------------- | ----------------------------------------------------------------------------------- |
| `branding[]`   | `key`, `surface`, `component`                      | `loginLogo`, `navigationLogo`, or `accountAvatar`; one component owns each surface. |
| `navigation[]` | `key`, `position`, `component`                     | `before`, `beforeLinks`, `afterLinks`, `after`, or one `replace`.                   |
| `login[]`      | `key`, `component`; optional `position`            | `before`, `after` (default), or one `replace`.                                      |
| `account[]`    | `key`, `surface`, `component`; optional `position` | `profile` or `security`, composed before/after or replaced once per surface.        |
| `shell[]`      | `key`, `position`, `component`                     | `header`, `actions`, or `settingsMenu`.                                             |
| `logoutButton` | `key`, `component`                                 | Replaces the account menu's sign-out control.                                       |

Every key must be unique in its contribution list. Replacement conflicts fail during admin config
validation instead of silently choosing one component.

## 1. Create the components {#components}

```svelte title="admin/src/components/brand-logo.svelte"
<script lang="ts">
	import type { AdminBrandComponentProps } from '@riducms/plugin';

	let { surface }: AdminBrandComponentProps = $props();
</script>

<!-- Reuse the logo at a larger size on the sign-in screen. -->
<span class="font-semibold" class:text-xl={surface === 'loginLogo'}>
	Acme Studio
</span>
```

```svelte title="admin/src/components/support-link.svelte"
<script lang="ts">
	import type { AdminNavigationComponentProps } from '@riducms/plugin';

	let { user }: AdminNavigationComponentProps = $props();
</script>

{#if user}
	<a href="mailto:editors@example.com">Contact the editorial team</a>
{/if}
```

```svelte title="admin/src/components/login-message.svelte" focus={8-9}
<script lang="ts">
	import type { AdminLoginComponentProps } from '@riducms/plugin';

	let { defaultView }: AdminLoginComponentProps = $props();
</script>

<p>Use your work email to sign in to Acme Studio.</p>
<!-- Keep Ridu's sign-in form and authentication behavior. -->
{@render defaultView()}
```

Ridu tells the logo where it is being displayed through `surface`; this example uses larger
text on the login screen. The navigation link reads `user` to show it after sign-in. The login
component renders the built-in form with `defaultView`, adding a message above it.

You can use your application's fonts, images, and other Svelte components in all three.

## 2. Choose where they appear {#register}

```ts title="admin/src/admin.config.ts"
import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import BrandLogo from './components/brand-logo.svelte';
import SupportLink from './components/support-link.svelte';
import LoginMessage from './components/login-message.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	branding: [
		{ key: 'login-logo', surface: 'loginLogo', component: BrandLogo },
		{
			key: 'nav-logo',
			surface: 'navigationLogo',
			component: BrandLogo
		}
	],
	navigation: [
		{ key: 'support', component: SupportLink, position: 'afterLinks' }
	],
	login: [
		{
			key: 'work-email',
			component: LoginMessage,
			position: 'replace'
		}
	]
});
```

Merge these entries into your existing admin config and keep `bun run dev` running.
On the login screen you should see Acme Studio and the work-email message. After signing in,
the navigation should show the same brand and the contact link.

`loginLogo` and `navigationLogo` select the two logo locations. `accountAvatar` is available for
a custom account image. Register at most one component for each location.

## Place content around navigation and account screens {#positions}

Navigation supports `before`, `beforeLinks`, `afterLinks`, `after`, and `replace`. Use
`beforeLinks` or `afterLinks` for content beside the collection and page links. Use
[Custom pages](./custom-pages.md) to add a new admin page and its navigation link.

This example uses `replace` for login because the component includes the built-in form. Use
`before` or `after` for a component that only adds content and does not render `defaultView`. Account components use those positions
and choose either `surface: 'profile'` or `surface: 'security'`. A replacement receives the
built-in content as `defaultView`; render it with `{@render defaultView()}` to keep the working
login or account form. Only one replacement for each target is allowed.

## Header, settings, and logout {#other-locations}

Use `shell` entries to add a component at `header`, `actions`, or `settingsMenu`. The
`logoutButton` option replaces the logout control; that component receives `host.logout()`
to sign the user out.

See the [admin configuration reference](https://riducms.com/reference/plugin/admin-config/) for these
options and their component prop types. For shared settings across several of your components,
see [Shared settings](./providers.md).
