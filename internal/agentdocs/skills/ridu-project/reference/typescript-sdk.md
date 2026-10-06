<!-- Generated from website/src/content/docs/typescript-sdk.md by scripts/sync-agent-docs.ts. -->

# TypeScript SDK

`@riducms/sdk` is a small Fetch client with ordinary `Promise` methods. `ridu generate` binds it to
your application, so collection names, inputs, filters and results are all checked by TypeScript
against your Go config.

## Install and create a client {#install-and-create}

A Ridu project already includes the generated client at `generated/ridu.generated.ts`. Create one
client and share it:

```ts title="lib/ridu.ts"
import { createClient } from '~/generated/ridu.generated';

export const ridu = createClient({
	baseURL: 'https://cms.example.com',
	auth: { collection: 'users' }
});
```

`~/generated` stands for your project's `generated/` directory; use your own alias or a relative
path. When `ridu dev` is running, a Go config change regenerates the module; commit it with the
change.

A frontend in another repository installs the SDK and imports the generated module from the CMS
project:

```bash title="terminal" package-manager="bun"
bun add @riducms/sdk
```

```bash title="terminal" package-manager="npm"
npm install @riducms/sdk
```

```bash title="terminal" package-manager="pnpm"
pnpm add @riducms/sdk
```

```bash title="terminal" package-manager="yarn"
yarn add @riducms/sdk
```

A browser on another origin needs [CORS](https://riducms.com/docs/cors/) configured on the server first; server-side
code doesn't. A SvelteKit app uses [`@riducms/sveltekit`](./sveltekit.md), which handles sessions
across browser and server renders.

<details class="docs-disclosure" id="commonjs-test-runners">
<summary>Jest and other CommonJS test runners</summary>
<div class="docs-disclosure-body">

`@riducms/sdk` ships as ES modules only. Vite, Metro, webpack, Node's `import` and Vitest load it
directly. Jest skips `node_modules` when transforming, so a test that reaches the client fails with
`SyntaxError: Cannot use import statement outside a module`. Add `@riducms` to the packages Jest
transforms, keeping your preset's entries:

```js title="jest.config.js"
module.exports = {
	preset: 'jest-expo',
	transformIgnorePatterns: [
		'node_modules/(?!(.pnpm|(jest-)?react-native|@react-native(-community)?|expo(nent)?|@expo(nent)?/.*|@riducms/.*))'
	]
};
```

The `.pnpm` entry reaches packages in pnpm's store layout; omit it for npm, Yarn and Bun.

</div>
</details>

## Read and write {#tour}

```ts title="posts.ts"
const { docs } = await ridu.list('posts', {
	where: { status: { equals: 'published' } },
	sort: ['-publishedAt'],
	limit: 10
});

const post = await ridu.create('posts', {
	title: 'Hello, Ridu',
	tags: ['launch']
});

await ridu.update('posts', post.id, { title: 'Hello again' });
await ridu.delete('posts', post.id);
```

The SDK covers everything in [Querying data](./querying.md) and [Writing data](./writing-data.md),
where each example has a TypeScript tab. The guides below cover what is specific to the SDK.

## Types you get {#types}

The generated module exports a type for each collection's documents, inputs and filters, such as
`Posts`, `PostsCreate` and `PostsWhere`, and the client uses them:

- **Results follow your options.** A literal `select` narrows the document type to the selected
  fields; a literal `populate` turns an ID into the related document; `pagination: false` removes
  the totals. Keep reused options literal with `as const` or `satisfies`.
- **Methods follow capabilities.** `upload` only accepts upload collections, `publishChanges` only
  versioned ones, and `restoreDeleted` only collections with trash.
- **Errors are caught at compile time,** such as an unknown field, an operator a field doesn't
  support, or a missing required input.

Two things stay unchecked: sort terms are plain strings, and responses aren't validated against
your document types at runtime. The server remains the authority for both.

## Handle errors {#errors}

A failed request rejects with a `RiduError`. Branch on its `code`, not its message:

```ts title="errors.ts"
import { RiduError } from '@riducms/sdk';

try {
	await ridu.update('posts', postID, { title: '' });
} catch (error) {
	if (!(error instanceof RiduError)) throw error;
	switch (error.code) {
		case 'validation':
			for (const issue of error.issues)
				showIssue(issue.path, issue.message);
			return;
		case 'conflict':
			return reloadAndAskTheAuthor();
		case 'rejected':
			// A hook refused the change; its message is written for people.
			return alert(error.message);
	}
	throw error;
}
```

A `RiduError` has `code`, `status`, `message`, `issues`, `details` and, from the server,
`requestId`. A non-Ridu error page, such as one from a proxy, still becomes a `RiduError` with a
code from its HTTP status. A cancelled request rejects with the runtime's own abort error instead.
[Writing data](./writing-data.md#errors) lists the codes.

## Guides {#guides}

- [Signing in](./typescript-sdk/signing-in.md): users, sessions, tokens and API keys.
- [Uploads](./typescript-sdk/uploads.md): send files and get URLs for private ones.
- [Drafts and publishing](./typescript-sdk/publishing.md): publish, schedule and restore.
- [Live checks](./typescript-sdk/live-validation.md): check a field while someone types.
- [Client options](./typescript-sdk/client-options.md): `fetch`, headers, middleware,
  cancellation, and custom endpoints.

The [SDK reference](https://riducms.com/reference/sdk/) lists every method and type.
