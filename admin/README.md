# `@riducms/admin`

The framework-owned Svelte 5 authoring application embedded by Ridu servers. Generated projects
provide a thin entry that mounts this package with their exact generated SDK client and statically
registered plugin/language imports; applications do not fork or regenerate the admin source.

```ts
import { mountAdmin } from "@riducms/admin";
import { createClient, type RiduConfig } from "../generated/ridu.generated";
import { adminPlugins } from "./plugins";

const target = document.getElementById("app");
if (!target) throw new Error("Ridu admin requires #app.");

mountAdmin<RiduConfig>({
	target,
	clientFactory: () => createClient({ baseURL: window.location.origin }),
	plugins: adminPlugins,
});
```

Use the generated project entry unless you are building a custom Ridu admin host. Keep
`@riducms/admin`, `@riducms/sdk`, `@riducms/plugin`, `@riducms/ui`, and paired plugin packages on the
same Ridu release. Production uses the built static assets embedded in the Go binary; this package
does not introduce a JavaScript server runtime.

Start with [The Ridu admin](../website/src/content/docs/admin.md), then use the focused author task
guides for [browsing](../website/src/content/docs/browsing-content.md),
[editing](../website/src/content/docs/editing-documents.md),
[bulk/trash actions](../website/src/content/docs/bulk-and-trash.md), and
[document locks](../website/src/content/docs/document-locks.md).
