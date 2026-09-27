# `@riducms/sveltekit`

SvelteKit integration for a Ridu application's generated client. Browsers call Ridu directly;
server loads and form actions use a request-scoped client. Both authenticate with one session token
kept in a cookie on the application's own domain, so the frontend and Ridu can run on unrelated
domains with third-party cookies blocked.

```ts
// src/lib/ridu.ts
import { PUBLIC_RIDU_URL } from "$env/static/public";
import { defineRidu, type InferClient } from "@riducms/sveltekit";
import { createClient } from "$lib/ridu.generated";

export const ridu = defineRidu({ createClient, baseURL: PUBLIC_RIDU_URL, authCollection: "users" });
export type Client = InferClient<typeof ridu>;
```

```ts
// src/hooks.server.ts
import { createRiduHandle } from "@riducms/sveltekit/server";
import { ridu } from "$lib/ridu";

export const handle = createRiduHandle(ridu);
```

Type `App.Locals.ridu` as `Client`, return `await locals.ridu.auth.getSession()` from the root
layout's server load with `depends(AUTH_DEPENDENCY)`, and call
`ridu.provide({ session: () => data.session })` in the root layout. Components then use
`ridu.use()`; `client.auth.session` is reactive.

The [SvelteKit guide](../../website/src/content/docs/sveltekit.md) covers loads, components,
private images, and the security trade-off: the token cookie is readable by the application's
scripts so the browser can send it to Ridu.

## Ownership

- `defineRidu` holds configuration and types. It never creates a shared authenticated client.
- `createRiduHandle` creates one client per request. `auth.getSession()` verifies once per request;
  a server-side login, logout, or rejected token writes the cookie on the response.
- `provide` creates one browser client per mounted application and an isolated, credential-free
  client for each server render. `BrowserSessionStore` owns the cookie, reactive session, tab
  synchronization, and stale-event rules.
- Transport, errors, and token handling stay in `@riducms/sdk`. This package only supplies
  SvelteKit lifecycle, context, and reactivity.
