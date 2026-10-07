---
title: 'SvelteKit'
description: 'Call Ridu directly from SvelteKit browsers and server loads with one session and generated types.'
product: sdk
eyebrow: 'TypeScript SDK'
order: 146
aliases:
  [
    '@riducms/sveltekit',
    'defineRidu',
    'createRiduHandle',
    'locals.ridu',
    'Vercel'
  ]
navigation:
  section: 'Work with data'
  order: 60
  title: 'SvelteKit'
---

`@riducms/sveltekit` connects a SvelteKit application to Ridu without a proxy. Browsers call Ridu
directly for login, reads, writes, and uploads. Server loads and form actions call Ridu with a
request-scoped client. Both authenticate with the same session token, kept in a cookie on the
application's own domain, so a SvelteKit site on Vercel and a Ridu API on Railway work together
without third-party cookies or a shared parent domain.

| Operation                                | Request path                              |
| ---------------------------------------- | ----------------------------------------- |
| Login, CRUD, search, uploads, and logout | Browser → Ridu                            |
| Server rendering and form actions        | SvelteKit → Ridu                          |
| Private images and downloads             | Browser → Ridu, through a short-lived URL |

## Install and allow the origin {#install}

```bash title="terminal" package-manager="bun"
bun add @riducms/sdk @riducms/sveltekit
```

```bash title="terminal" package-manager="npm"
npm install @riducms/sdk @riducms/sveltekit
```

```bash title="terminal" package-manager="pnpm"
pnpm add @riducms/sdk @riducms/sveltekit
```

```bash title="terminal" package-manager="yarn"
yarn add @riducms/sdk @riducms/sveltekit
```

Browsers call Ridu from the SvelteKit origin, so list it in the CMS's
[`AllowedOrigins`](/docs/cors/), for example `RIDU_ALLOWED_ORIGINS=https://app.example.com`.

The package supports SvelteKit 2.20 and later, and SvelteKit 3.

## Bind the generated client {#define}

```ts title="src/lib/ridu.ts"
import { PUBLIC_RIDU_URL } from '$env/static/public';
import { defineRidu, type InferClient } from '@riducms/sveltekit';
import { createClient } from '$lib/ridu.generated';

export const ridu = defineRidu({
	createClient,
	baseURL: PUBLIC_RIDU_URL,
	authCollection: 'users'
});

export type Client = InferClient<typeof ridu>;
```

`defineRidu` holds configuration and types; it never creates a shared authenticated client.
`authCollection` must be an auth collection, and it types every session's `user`. The Ridu URL is
public configuration because browsers call it.

SvelteKit 3 declares environment variables in `src/env.ts` instead. Mark the URL public there and
import it from `$app/env/public`:

```ts title="src/env.ts"
import { defineEnvVars } from '@sveltejs/kit/env';

export const variables = defineEnvVars({
	RIDU_URL: { public: true }
});
```

Then use `import { RIDU_URL } from '$app/env/public'` and pass `baseURL: RIDU_URL`.

## Add the server hook {#hook}

```ts title="src/hooks.server.ts"
import { createRiduHandle } from '@riducms/sveltekit/server';
import { ridu } from '$lib/ridu';

export const handle = createRiduHandle(ridu);
```

```ts title="src/app.d.ts"
import type { Client } from '$lib/ridu';

declare global {
	namespace App {
		interface Locals {
			ridu: Client;
		}
	}
}

export {};
```

The hook gives each request its own client in `event.locals.ridu`. Compose it with other hooks
through `sequence`, which SvelteKit 3 exports from `@sveltejs/kit/hooks` along with the `Handle`
type. A server-side login, logout, or rejected token updates the session cookie on the
response.

Server requests go through SvelteKit's `event.fetch`. The client sends its token in the
`Authorization` header with `credentials: "omit"`, so the web app's own cookies are never forwarded
to Ridu. To use another fetch, pass `fetch` to `defineRidu` or `createRiduHandle`.

## Load and provide the session {#layout}

```ts title="src/routes/+layout.server.ts"
import { AUTH_DEPENDENCY } from '@riducms/sveltekit';

export const load = async ({ locals, depends }) => {
	depends(AUTH_DEPENDENCY);
	return { session: await locals.ridu.auth.getSession() };
};
```

```svelte title="src/routes/+layout.svelte"
<script lang="ts">
	import { ridu } from '$lib/ridu';

	let { data, children } = $props();

	ridu.provide({ session: () => data.session });
</script>

{@render children()}
```

`getSession()` verifies the token with Ridu once per request and resolves `null` without a session.
An unavailable Ridu rejects instead, so an outage never looks like a logout. The returned snapshot
contains the user and session metadata, never the token.

`provide` creates one client for the mounted application. A server render gets an isolated client
that holds no credential. Later layout snapshots refresh the user, but never replace a newer
sign-in or sign-out in the tab.

## Server loads {#server-loads}

```ts title="src/routes/app/+page.server.ts"
import { redirect } from '@sveltejs/kit';
import { AUTH_DEPENDENCY } from '@riducms/sveltekit';

export const load = async ({ locals, depends }) => {
	depends(AUTH_DEPENDENCY);
	const session = await locals.ridu.auth.getSession();
	if (!session) redirect(303, '/login');

	const bookmarks = await locals.ridu.list('bookmarks', {
		where: { owner: { equals: session.user.id } },
		sort: ['-createdAt']
	});
	return { bookmarks: bookmarks.docs };
};
```

Declare `AUTH_DEPENDENCY` in loads whose result depends on who is signed in. Signing in or out reruns
exactly those loads; ordinary writes never trigger a full reload.

## Components {#components}

```svelte title="src/lib/login-form.svelte"
<script lang="ts">
	import { goto } from '$app/navigation';
	import { ridu } from '$lib/ridu';

	const client = ridu.use();
	let email = $state('');
	let password = $state('');

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		await client.auth.login({ email, password });
		await goto('/app');
	}
</script>

<p>
	Signed in as {client.auth.session?.user.displayName ?? 'nobody'}
</p>
```

`ridu.use()` returns the provided client. Its `auth.session` is reactive, and CRUD methods such as
`client.create`, `client.update` with a revision, and `client.upload` go straight to Ridu. Failures
remain structured `RiduError`s, and the SDK never silently replays a write.

The package also handles the session details applications should not rebuild:

- Login stores the token before its promise resolves, so an immediate navigation is signed in.
- Every request reads the current token instead of one captured when the client was created.
- Logout revokes the session and clears the matching token, even when the server is unreachable.
- Tabs share sign-in and sign-out through `BroadcastChannel`; a tab that missed a message, or whose
  token changed through a form action, catches up when it becomes visible.
- A late response for an earlier token cannot restore a previous account or clear a newer login.
- A token Ridu rejects is removed, and the next server render also clears a stale cookie.
- Rotation is never automatic; the session ends at its configured expiry.

## Private images {#images}

An `<img>` cannot send the session header. Mint short-lived delivery URLs in the load that renders
the images:

```ts title="src/routes/app/+page.server.ts"
const urls = await locals.ridu.getUploadURLs(
	'assets',
	assets.map((asset) => ({ id: asset.id, size: 'thumb' })),
	{ expiresIn: 1800 }
);
```

The URLs point directly at Ridu, never contain the token, and stop working at logout or when read
access ends. See [private uploads in the browser](/docs/authentication/#upload-urls).

## Security trade-off {#security}

The session cookie is readable by scripts on the application's domain, which is what lets the
browser send it to Ridu directly. A cross-site scripting flaw in the application can therefore steal
the session token. Ridu's embedded admin keeps using its HttpOnly cookie. If your application cannot
accept a script-readable token, keep Ridu's cookie behind a same-origin proxy instead.
