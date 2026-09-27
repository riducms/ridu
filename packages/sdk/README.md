# `@riducms/sdk`

The Fetch runtime behind each generated Ridu TypeScript client. It provides
`Promise<T>` methods, structured `RiduError` failures, middleware, authentication,
collection/global operations, uploads, versions, and plugin transports. Application-specific
document and input types come from `ridu generate`, not from this package alone.

It works in browsers, Bun, and Node server tooling. See the
[TypeScript SDK guide](../../website/src/content/docs/typescript-sdk.md) for setup and examples.

Generated projects should import their application-specific factory rather than constructing a
generic schema by hand:

```ts
import { createClient } from "./generated/ridu.generated";

const ridu = createClient({ baseURL: "https://cms.example.com" });
const posts = await ridu.list("posts", {
	where: { status: { equals: "published" } },
	sort: ["-createdAt"],
});
```

Pass `includeAccess: true` when an authoring surface needs collection and per-document
capabilities alongside the page. The opt-in response is a `CollectionPageEnvelope`; ordinary list
calls retain the smaller `PageEnvelope`. Capabilities describe available controls, while the server
still authorizes each mutation.

Authentication lives under `ridu.auth`. Same-origin browser clients use Ridu's HttpOnly cookie by
default. A frontend on another domain, a server renderer, or a script passes a token store and the
client sends `Authorization: Session <token>` on every request:

```ts
import { memoryTokenStore } from "@riducms/sdk";

const ridu = createClient({
	baseURL: "https://cms.example.com",
	auth: { collection: "users", token: memoryTokenStore() },
});
await ridu.auth.login({ email, password });
const session = await ridu.auth.getSession(); // null without a session; rejects on an outage
```

SvelteKit applications use `@riducms/sveltekit`, which supplies a cookie-backed store, request-scoped
server clients, and reactive session state. `getUploadURLs` mints short-lived URLs for private
uploads displayed with `<img>`. Long-running service clients should use an expiring API key.
`RiduError` carries stable codes, HTTP status, path-aware issues, and request context; do not reduce
failures to an untyped string.
