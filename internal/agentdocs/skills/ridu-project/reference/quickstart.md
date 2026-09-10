<!-- Generated from website/src/content/docs/quickstart.md by scripts/sync-agent-docs.ts. -->

# Quickstart

This guide takes you from an empty directory to a running CMS and a typed SDK read. The **Starter**
template includes an authenticated `users` collection and a small `posts` collection.

You need [Go 1.25 or newer](https://go.dev/doc/install), a compatible Node.js version, and one package manager:
npm, Bun, pnpm, or Yarn. Use Node.js 24 or newer for a new project. The generated admin also supports
Node.js 20.19+ on the 20.x line and 22.12+ on the 22.x line; see
[Releases and compatibility](https://riducms.com/docs/releases/#supported-matrix). You do not need a global `ridu` command.

## 1. Choose a database and create the project {#choose-a-database}

Both paths produce the same Ridu application. Choose **SQLite** when you want the fewest moving
parts on one machine. Choose **PostgreSQL** when the database will be a separate service or the
application may run on more than one host.

<div data-doc-choice="database">
  <div role="tablist" aria-label="Quickstart database">
    <button type="button" role="tab" data-doc-choice-value="sqlite">SQLite</button>
    <button type="button" role="tab" data-doc-choice-value="postgres">PostgreSQL</button>
  </div>
  <div id="sqlite-path" role="tabpanel" data-doc-choice-panel="sqlite">
    <p>Choose <strong>SQLite</strong> in the wizard for the shortest path to a local Ridu project. It needs no database service or Docker setup. Development data lives in <code>.ridu/development.sqlite</code> and is created when <code>dev</code> starts.</p>
    <p>SQLite is intended for a local file on one host. Before deploying, set an absolute <code>RIDU_SQLITE_PATH</code> and review the <a href="/docs/sqlite/#existing-project">SQLite production guidance</a>.</p>
  </div>
  <div id="postgresql-path" role="tabpanel" data-doc-choice-panel="postgres">
    <p>Choose <strong>PostgreSQL</strong> in the wizard for a networked database that can run independently of the application and support multiple app hosts. The generated project includes a development service on port <code>54329</code>; <code>dev</code> starts it with Docker or OrbStack.</p>
    <p>If you already operate PostgreSQL, set <code>DATABASE_URL</code> and run <code>dev --no-docker</code>. Production connections must use verified TLS; the <a href="/docs/postgres/#existing-project">PostgreSQL guide</a> shows the complete existing-service setup.</p>
  </div>
</div>

Start the initializer with your package manager. The positional `my-app` names the project
directory; the wizard asks you to choose the **Starter** template, the database above, and optional
coding-agent guidance. The remaining commands install the workspace and start development.

```bash title="terminal" package-manager="npm"
npm create ridu@latest my-app
cd my-app
npm install
npm run dev
```

```bash title="terminal" package-manager="bun"
bun create ridu@latest my-app
cd my-app
bun install
bun run dev
```

```bash title="terminal" package-manager="pnpm"
pnpm create ridu@latest my-app
cd my-app
pnpm install
pnpm run dev
```

```bash title="terminal" package-manager="yarn"
yarn create ridu my-app
cd my-app
yarn install
yarn run dev
```

The initializer writes the initial migration with the generated contracts. After installing
dependencies, the project is ready for its first production build.

<details class="docs-disclosure">
<summary>Non-interactive scaffolding and custom package identities</summary>
<div class="docs-disclosure-body">

Use flags for non-interactive setup or to set the Go module and npm scope. This example selects
SQLite; change `--database sqlite` to `--database postgres` for the PostgreSQL path.

```bash title="terminal" package-manager="npm"
npm create ridu@latest -- \
	--template starter \
	--database sqlite \
	--module github.com/acme/content \
	--scope @acme \
	--no-agent \
	content
```

```bash title="terminal" package-manager="bun"
bun create ridu@latest \
	--template starter \
	--database sqlite \
	--module github.com/acme/content \
	--scope @acme \
	--no-agent \
	content
```

```bash title="terminal" package-manager="pnpm"
pnpm create ridu@latest \
	--template starter \
	--database sqlite \
	--module github.com/acme/content \
	--scope @acme \
	--no-agent \
	content
```

```bash title="terminal" package-manager="yarn"
yarn create ridu \
	--template starter \
	--database sqlite \
	--module github.com/acme/content \
	--scope @acme \
	--no-agent \
	content
```

A new named target directory must not already exist. Use `.` to create the project in your current
directory; Ridu warns first and stops if any project paths conflict with existing files. Replace `--no-agent` with
`--agent codex|claude|cursor|all` when automation should install agent guidance. See
[Installation](./installation.md#create-project) for every scaffold option and recovery behavior.

</div>
</details>

## 2. Open the admin {#open-the-admin}

Keep the selected tab's `dev` command running. Wait for it to print healthy API and admin URLs, then
open `http://localhost:8080/admin`. On an empty database Ridu shows the setup screen instead of a
login form. Create the first user with an email address and a password.

The setup operation is available only while the configured auth collection is empty. After it
succeeds, the same URL shows the login screen.

![The Ridu admin dashboard showing the Users and Posts collections.](https://raw.githubusercontent.com/riducms/ridu/main/docs/assets/ridu-admin-dashboard.png)

## 3. Create your first post {#create-a-post}

Choose **Posts** in the sidebar, select **Create new**, and enter:

| Field  | Value             |
| ------ | ----------------- |
| Title  | `Hello from Ridu` |
| Status | `published`       |
| Author | Your new user     |

Save the document. The list view should now contain **Hello from Ridu**.

## 4. Read it through the generated SDK {#read-with-the-sdk}

Open `generated/ridu.generated.ts`, then create `scripts/read-posts.ts`:

```ts title="scripts/read-posts.ts"
import { createClient } from '../generated/ridu.generated';

const baseURL = process.env.RIDU_URL ?? 'http://localhost:8080';
const email = process.env.RIDU_EMAIL;
const password = process.env.RIDU_PASSWORD;

if (!email || !password) {
	throw new Error(
		'Set RIDU_EMAIL and RIDU_PASSWORD to the user created in the admin.'
	);
}

let sessionCookie = '';
const ridu = createClient({
	baseURL,
	middleware: [
		async (request, next) => {
			const headers = new Headers(request.headers);
			if (sessionCookie) headers.set('Cookie', sessionCookie);

			const response = await next(new Request(request, { headers }));
			const setCookie = response.headers.get('set-cookie');
			const match = setCookie?.match(
				/(?:^|,\s*)(ridu_session=[^;,\s]+)/
			);
			const session = match?.[1];
			if (session) sessionCookie = session;
			return response;
		}
	]
});

await ridu.login('users', { email, password });
const page = await ridu.list('posts', {
	where: { status: { equals: 'published' } },
	select: { title: true, status: true },
	sort: ['-createdAt']
});

console.log(page.docs);
```

Leave the selected tab's `dev` command running and execute the script in a second terminal:

```bash title="terminal" package-manager="npm"
RIDU_EMAIL='you@example.com' \
RIDU_PASSWORD='your-password' \
npm exec tsx -- scripts/read-posts.ts
```

```bash title="terminal" package-manager="bun"
RIDU_EMAIL='you@example.com' \
RIDU_PASSWORD='your-password' \
bun scripts/read-posts.ts
```

```bash title="terminal" package-manager="pnpm"
RIDU_EMAIL='you@example.com' \
RIDU_PASSWORD='your-password' \
pnpm exec tsx scripts/read-posts.ts
```

```bash title="terminal" package-manager="yarn"
RIDU_EMAIL='you@example.com' \
RIDU_PASSWORD='your-password' \
yarn tsx scripts/read-posts.ts
```

The output includes the post you just created. The collection slug, filter operators, selected
fields, and returned document shape are inferred from the generated contract; mistyping `posts`,
`status`, or `published` is a TypeScript error.

The middleware keeps the opaque `ridu_session` cookie in memory for this process and forwards it
after login. Do not log or persist it. The typed login result does not expose the raw session token.
Browser applications need no cookie middleware; call `login` once and the SDK's default
`credentials: "include"` sends the `HttpOnly` cookie. Long-running service clients should use an
expiring API key rather than a user's password or browser session.

## 5. Change the model {#change-the-model}

Add a summary to `content/posts.go`:

```go title="content/posts.go" add={11-15}
var Posts = ridu.Collection{
	Slug: "posts",
	Access: ridu.CollectionAccess{
		Create: authenticatedOnly,
		Read:   authenticatedOnly,
		Update: authenticatedOnly,
		Delete: authenticatedOnly,
	},
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Textarea("summary").
			MaxLength(240).
			Admin(field.Admin{
				Description: "A short introduction used by post cards.",
			}),
		field.Select("status", "draft", "published").Default("draft"),
		field.Relationship("author", "users"),
		richtext.Field("content"),
	},
}
```

Save the file while `dev` is running. Ridu regenerates the contracts, applies the additive
development change, restarts the server, and refreshes the admin. Reopen the post; the **Summary**
control should now be available.

Run this before committing:

```bash title="terminal"
ridu doctor
ridu migrate create --name add-post-summary
ridu generate --check
ridu check
```

Review the new migration, then commit it with `generated/` and your Go config. `.ridu/` remains
disposable local state. Development sync is not a deployment plan.

## If something does not start {#troubleshooting}

Run the `doctor` command from the package-manager tabs above first. If the project-local launcher is
not installed yet, repeat the matching `install` command from step 1 and retry. If scaffolding
stopped during Go setup, follow the exact recovery commands printed by the CLI. For an occupied
database port, either stop the conflicting local service or follow the existing-service command in
the [PostgreSQL guide](./postgres.md#existing-project).

Next, learn [how a Ridu project is organised](https://riducms.com/guides/project-structure/), choose the right
[field](./fields.md), or read the [TypeScript SDK guide](./typescript-sdk.md). See
[Installation](./installation.md) for other templates, existing services, and recovery.
