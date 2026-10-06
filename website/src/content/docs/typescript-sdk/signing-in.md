---
title: 'Signing in'
description: 'Sign users up and in with the TypeScript SDK, keep sessions in cookies or token stores, and authenticate services with API keys.'
product: sdk
eyebrow: 'TypeScript SDK'
order: 146
aliases:
  [
    'login',
    'logout',
    'session',
    'token',
    'createUser',
    'sign up',
    'api key',
    'memoryTokenStore'
  ]
navigation:
  section: 'Work with data'
  parent: typescript-sdk
  order: 10
  title: 'Signing in'
---

Authentication lives under `ridu.auth`. Give the client the auth collection your users belong to,
and every call uses it:

```ts title="lib/ridu.ts"
export const ridu = createClient({
	baseURL: 'https://cms.example.com',
	auth: { collection: 'users' }
});
```

Without `auth.collection`, pass `{ collection }` to each auth call. How sessions, password rules and
lockout work on the server is covered in [Authentication](/docs/authentication/).

## Sign up {#sign-up}

```ts
const user = await ridu.auth.createUser({
	data: { email, name },
	password
});
```

The password stays outside `data`. Whether anyone may sign up is decided by the collection's
`Create` access rule.

## Sign in and out {#sign-in}

```ts
const session = await ridu.auth.login({ email, password });
console.log(session.user.name);

const current = await ridu.auth.getSession(); // null when signed out
await ridu.auth.logout();
```

`getSession` checks the session with the server. It resolves `null` when nobody is signed in, and
rejects when the server can't be reached, so an outage never looks like a sign-out. `logoutAll` ends
every session of the current user.

## Cookies or tokens {#transports}

By default the SDK uses Ridu's HttpOnly session cookie, which suits a browser on the same site as
the server. A browser on another site, a server render or a script uses a token instead. Give the
client a token store, and the SDK keeps the token in it and sends `Authorization: Session <token>`
with every request:

```ts title="scripts/import.ts"
import { memoryTokenStore } from '@riducms/sdk';

const ridu = createClient({
	baseURL: process.env.RIDU_URL!,
	auth: { collection: 'users', token: memoryTokenStore() }
});
await ridu.auth.login({ email, password });
```

A store is any object with the current `token`, plus `issued` and `cleared` callbacks, so you can
keep the token wherever suits your app:

```ts title="lib/token-store.ts"
let token: string | null = loadSavedToken();

export const tokenStore = {
	get token() {
		return token;
	},
	issued(value: string) {
		token = value;
		saveToken(value);
	},
	cleared(value: string) {
		// Ignore a late response about a token already replaced.
		if (token === value) token = null;
	}
};
```

The SDK reads the store on every request and clears a token the server rejects. In SvelteKit,
[`@riducms/sveltekit`](/docs/sveltekit/) supplies a store that keeps the token in a cookie on your
app's own domain.

## Services and API keys {#api-keys}

A server-to-server client sends an API key as a Bearer token:

```ts title="service-client.ts"
const service = createClient({
	baseURL: 'https://cms.example.com',
	headers: async () => ({
		Authorization: `Bearer ${await loadAPIKey()}`
	})
});
```

A signed-in user creates a key with `ridu.auth.createAPIKey`, and only that call returns its
secret. `apiKeys` lists the user's keys without secrets; `revokeAPIKey` revokes one.

## Account tasks {#account}

| Method                                  | Does                                                     |
| --------------------------------------- | -------------------------------------------------------- |
| `changePassword`                        | changes the password and ends every session              |
| `sessions`, `revokeSession`             | lists the user's sessions, and ends one                  |
| `rotate`                                | replaces the session token without extending the session |
| `requestPasswordReset`, `resetPassword` | sends a reset message, and sets a new password from it   |
| `requestVerification`, `verifyEmail`    | sends a verification message, and confirms the address   |
| `forceUnlock`                           | unlocks an account locked after failed sign-ins          |

Reset, verification and lockout methods work when the auth collection enables them.

## Preview tokens {#preview}

`preview` and `previewGlobal` read a draft with a short-lived preview token, which
`createPreviewToken` issues and `revokePreviewToken` revokes. Pair them with `connectLivePreview` to
update a preview frame as an author types; see [Live preview](/guides/live-preview/).
