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

Browser clients use the default cookie credentials. Long-running service clients should use an
expiring API key. `RiduError` carries stable codes, HTTP status, path-aware issues, and request
context; do not reduce failures to an untyped string.
