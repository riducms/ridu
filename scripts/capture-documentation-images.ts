import { copyFile, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { chromium, type Browser, type Locator, type Page } from "@playwright/test";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const playgroundRoot = resolve(repositoryRoot, "playground/documentation");
const captureRoot = resolve(repositoryRoot, "playground/.ridu/docs-capture");
const outputDirectory = resolve(repositoryRoot, "docs/assets");
const fieldOutputDirectory = resolve(outputDirectory, "fields");
const temporaryDirectory = resolve(captureRoot, ".screenshots");

const sqliteOnly = process.argv.includes("--sqlite-only");
const keepProjects = process.argv.includes("--keep-projects");
const reuseProjects = process.argv.includes("--reuse-projects");

interface CaptureField {
	slug: string;
	collection: string;
	documentId: string;
	route: string;
	selector: string;
	state: string;
	output: string;
	documentationOwner: string;
}

interface CaptureManifest {
	version: number;
	fields: CaptureField[];
}

interface Variant {
	name: string;
	template: "starter" | "blank";
	database: "sqlite" | "postgres";
	module: string;
}

interface RunningApp {
	process: Bun.Subprocess<"ignore", "pipe", "pipe">;
	logs: Promise<string>;
	apiURL: string;
	adminURL: string;
}

const variants: Variant[] = [
	{
		name: "starter-sqlite",
		template: "starter",
		database: "sqlite",
		module: "example.com/ridu-documentation-starter-sqlite",
	},
	{
		name: "starter-postgres",
		template: "starter",
		database: "postgres",
		module: "example.com/ridu-documentation-starter-postgres",
	},
	{
		name: "blank-sqlite",
		template: "blank",
		database: "sqlite",
		module: "example.com/ridu-documentation",
	},
	{
		name: "blank-postgres",
		template: "blank",
		database: "postgres",
		module: "example.com/ridu-documentation-blank-postgres",
	},
];

const selectedVariants = sqliteOnly
	? variants.filter((variant) => variant.database === "sqlite")
	: variants;

async function run(
	command: string,
	args: string[],
	options: { cwd?: string; env?: Record<string, string | undefined> } = {}
) {
	const child = Bun.spawn([command, ...args], {
		cwd: options.cwd ?? repositoryRoot,
		env: { ...Bun.env, ...options.env },
		stdin: "ignore",
		stdout: "inherit",
		stderr: "inherit",
	});
	const exitCode = await child.exited;
	if (exitCode !== 0) {
		throw new Error(`${command} ${args.join(" ")} exited with ${exitCode}`);
	}
}

async function generateVariant(variant: Variant) {
	const target = resolve(captureRoot, variant.name);
	await rm(target, { recursive: true, force: true });
	await run("go", [
		"run",
		"./internal/dogfood/new",
		"--target",
		target,
		"--module",
		variant.module,
		"--scope",
		"@ridu-docs",
		"--template",
		variant.template,
		"--database",
		variant.database,
		"--agent",
		"none",
	]);
	return target;
}

async function overlayFieldGallery(projectRoot: string) {
	await mkdir(resolve(projectRoot, "cmd/seed"), { recursive: true });
	await copyFile(
		resolve(playgroundRoot, "content/config.go"),
		resolve(projectRoot, "content/config.go")
	);
	await copyFile(
		resolve(playgroundRoot, "content/field-guide.go"),
		resolve(projectRoot, "content/field-guide.go")
	);
	await copyFile(
		resolve(playgroundRoot, "cmd/seed/main.go"),
		resolve(projectRoot, "cmd/seed/main.go")
	);

	const serverPath = resolve(projectRoot, "cmd/server/main.go");
	let server = await readFile(serverPath, "utf8");
	server = server.replace(
		/\n\t\tridu\.WithUploadStorage\(func\(context\.Context\) \(storage\.Backend, error\) \{\n\t\t\treturn localstorage\.New\(filepath\.Join\("\.ridu", "documentation-uploads"\)\)\n\t\t\}\),/g,
		""
	);
	server = server.replace(
		'"github.com/riducms/ridu/adapters/sqlite"\n',
		'"github.com/riducms/ridu/adapters/sqlite"\n\tlocalstorage "github.com/riducms/ridu/adapters/storage/local"\n'
	);
	server = server.replace(
		'"github.com/riducms/ridu/store"\n',
		'"github.com/riducms/ridu/storage"\n\t"github.com/riducms/ridu/store"\n'
	);
	server = server.replace(
		"\t\t}),\n\t\tridu.WithAddress",
		"\t\t}),\n\t\tridu.WithUploadStorage(func(context.Context) (storage.Backend, error) {\n" +
			'\t\t\treturn localstorage.New(filepath.Join(".ridu", "documentation-uploads"))\n' +
			"\t\t}),\n\t\tridu.WithAddress"
	);
	await writeFile(serverPath, server);
	await run("gofmt", [
		"-w",
		resolve(projectRoot, "content/config.go"),
		resolve(projectRoot, "content/field-guide.go"),
		resolve(projectRoot, "cmd/seed/main.go"),
		serverPath,
	]);
	await run(resolve(projectRoot, ".ridu/bin/ridu"), ["generate"], {
		cwd: projectRoot,
		env: { GOWORK: "off" },
	});
}

async function collectOutput(stream: ReadableStream<Uint8Array> | null) {
	if (stream === null) return "";
	return new Response(stream).text();
}

async function waitForURL(url: string, app: RunningApp, timeout = 180_000) {
	const deadline = Date.now() + timeout;
	let lastError = "not ready";
	while (Date.now() < deadline) {
		if (app.process.exitCode !== null) {
			throw new Error(
				`documentation app exited early (${app.process.exitCode})\n${await app.logs}`
			);
		}
		try {
			const response = await fetch(url);
			if (response.ok) return;
			lastError = `HTTP ${response.status}`;
		} catch (cause) {
			lastError = cause instanceof Error ? cause.message : String(cause);
		}
		await Bun.sleep(250);
	}
	throw new Error(`timed out waiting for ${url}: ${lastError}`);
}

function startSQLiteApp(projectRoot: string, apiPort: number, adminPort: number): RunningApp {
	const databasePath = resolve(projectRoot, ".ridu/documentation.sqlite");
	const child = Bun.spawn(
		[
			resolve(projectRoot, ".ridu/bin/ridu"),
			"dev",
			"--no-docker",
			"--database-path",
			databasePath,
			"--address",
			`127.0.0.1:${apiPort}`,
			"--admin-port",
			String(adminPort),
		],
		{
			cwd: projectRoot,
			env: { ...Bun.env, GOWORK: "off" },
			stdin: "ignore",
			stdout: "pipe",
			stderr: "pipe",
		}
	);
	const logs = Promise.all([collectOutput(child.stdout), collectOutput(child.stderr)]).then(
		([stdout, stderr]) => `${stdout}\n${stderr}`
	);
	return {
		process: child,
		logs,
		apiURL: `http://127.0.0.1:${apiPort}`,
		adminURL: `http://127.0.0.1:${adminPort}`,
	};
}

function startPostgresApp(projectRoot: string, apiPort: number, adminPort: number): RunningApp {
	const child = Bun.spawn(
		[
			resolve(projectRoot, ".ridu/bin/ridu"),
			"dev",
			"--address",
			`127.0.0.1:${apiPort}`,
			"--admin-port",
			String(adminPort),
		],
		{
			cwd: projectRoot,
			env: { ...Bun.env, GOWORK: "off" },
			stdin: "ignore",
			stdout: "pipe",
			stderr: "pipe",
		}
	);
	const logs = Promise.all([collectOutput(child.stdout), collectOutput(child.stderr)]).then(
		([stdout, stderr]) => `${stdout}\n${stderr}`
	);
	return {
		process: child,
		logs,
		apiURL: `http://127.0.0.1:${apiPort}`,
		adminURL: `http://127.0.0.1:${adminPort}`,
	};
}

async function stopApp(app: RunningApp) {
	if (app.process.exitCode === null) {
		app.process.kill("SIGINT");
		await Promise.race([app.process.exited, Bun.sleep(10_000)]);
	}
	if (app.process.exitCode === null) app.process.kill("SIGKILL");
}

async function resetPostgresProject(projectRoot: string) {
	await run("docker", ["compose", "down", "--volumes", "--remove-orphans"], {
		cwd: projectRoot,
	});
}

async function seedFieldGallery(projectRoot: string) {
	await run("go", ["run", "./cmd/seed"], {
		cwd: projectRoot,
		env: {
			GOWORK: "off",
			RIDU_DOCS_DATABASE_PATH: resolve(projectRoot, ".ridu/documentation.sqlite"),
			RIDU_DOCS_UPLOAD_ROOT: resolve(projectRoot, ".ridu/documentation-uploads"),
		},
	});
}

async function createPage(browser: Browser, baseURL: string) {
	const context = await browser.newContext({
		baseURL,
		colorScheme: "dark",
		deviceScaleFactor: 1,
		locale: "en-GB",
		viewport: { width: 1440, height: 900 },
	});
	const page = await context.newPage();
	await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
	await page.addInitScript(() => {
		localStorage.setItem("ridu-theme", "dark");
		localStorage.setItem("ridu-sidebar-open", "true");
	});
	return page;
}

async function logIn(page: Page, email: string, password: string) {
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill(email);
	await page.getByRole("textbox", { name: "Password", exact: true }).fill(password);
	await page.getByRole("button", { name: "Sign in" }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor();
}

async function firstDocumentID(page: Page, collection: string) {
	const response = await page.request.get(
		`/api/collections/${encodeURIComponent(collection)}?limit=1&locale=en`
	);
	if (!response.ok()) {
		throw new Error(`list ${collection} for documentation capture: ${response.status()}`);
	}
	const body = (await response.json()) as { docs?: Array<{ id?: string }> };
	const id = body.docs?.[0]?.id;
	if (id === undefined || id === "") {
		throw new Error(`documentation capture project has no ${collection} document`);
	}
	return id;
}

async function settle(page: Page) {
	await page.waitForLoadState("networkidle");
	await page.waitForTimeout(200);
	await page.evaluate(async () => await document.fonts.ready);
}

async function normalizeScreenshot(source: string, output: string) {
	await mkdir(dirname(output), { recursive: true });
	await run("magick", [
		source,
		"-bordercolor",
		"#111012",
		"-border",
		"24x24",
		"-strip",
		"-define",
		"png:exclude-chunk=date,time",
		output,
	]);
}

async function capturePage(page: Page, filename: string) {
	await settle(page);
	const raw = resolve(temporaryDirectory, `${filename}.raw.png`);
	await page.screenshot({ path: raw, fullPage: false, animations: "disabled" });
	await normalizeScreenshot(raw, resolve(outputDirectory, filename));
	console.log(`captured ${filename}`);
}

async function captureLocator(page: Page, locator: Locator, field: CaptureField) {
	await locator.scrollIntoViewIfNeeded();
	await locator.waitFor({ state: "visible" });
	await settle(page);
	const raw = resolve(temporaryDirectory, `${field.slug}.raw.png`);
	if (field.slug === "select") {
		const controlRaw = resolve(temporaryDirectory, `${field.slug}.control.raw.png`);
		const contentRaw = resolve(temporaryDirectory, `${field.slug}.content.raw.png`);
		await locator.screenshot({ path: controlRaw, animations: "disabled" });
		await locator.locator('[data-slot="select-trigger"]').click();
		const content = page.locator('[data-slot="select-content"]:visible');
		await content.waitFor();
		await content.screenshot({ path: contentRaw, animations: "disabled" });
		await run("magick", ["-background", "#111012", controlRaw, contentRaw, "-append", raw]);
		await page.keyboard.press("Escape");
	} else if (field.slug === "date") {
		const controlRaw = resolve(temporaryDirectory, `${field.slug}.control.raw.png`);
		const contentRaw = resolve(temporaryDirectory, `${field.slug}.content.raw.png`);
		await locator.screenshot({ path: controlRaw, animations: "disabled" });
		await locator.getByRole("button", { name: "Open calendar" }).click();
		const content = page.locator('[data-slot="popover-content"]:visible');
		await content.waitFor();
		await content.screenshot({ path: contentRaw, animations: "disabled" });
		await run("magick", ["-background", "#111012", controlRaw, contentRaw, "-append", raw]);
		await page.keyboard.press("Escape");
	} else {
		await locator.screenshot({ path: raw, animations: "disabled" });
	}
	await normalizeScreenshot(raw, resolve(repositoryRoot, field.output));
	console.log(`captured field ${field.slug} (${field.state})`);
}

async function captureFieldGallery(page: Page, manifest: CaptureManifest) {
	let currentRoute = "";
	for (const field of manifest.fields) {
		const route = field.route;
		if (route !== currentRoute) {
			await page.goto(route);
			await page.getByRole("main").waitFor();
			currentRoute = route;
		}
		await captureLocator(page, page.locator(field.selector).first(), field);
	}
}

async function captureFieldApplication(
	browser: Browser,
	app: RunningApp,
	manifest: CaptureManifest
) {
	const page = await createPage(browser, app.adminURL);
	await logIn(page, "docs@riducms.test", "ridu-documentation");

	await page.goto("/admin/collections/articles/docs-article?locale=fr");
	await page.getByLabel("Content locale", { exact: true }).waitFor();
	await capturePage(page, "ridu-admin-localization.png");

	await page.goto("/admin/collections/media");
	await page.getByRole("heading", { name: "Media", exact: true }).waitFor();
	const mediaID = await firstDocumentID(page, "media");
	await page.goto(`/admin/collections/media/${mediaID}?locale=en`);
	await page.getByRole("region", { name: "Asset preview" }).waitFor();
	await capturePage(page, "ridu-admin-upload.png");

	await page.goto("/admin/collections/articles/docs-article?locale=en");
	await page.getByRole("button", { name: "Live preview", exact: true }).click();
	await page.getByRole("region", { name: "Live preview" }).waitFor();
	await page.getByText("Connected", { exact: true }).waitFor();
	await capturePage(page, "ridu-admin-live-preview.png");

	await captureFieldGallery(page, manifest);
	await page.context().close();
}

async function createStarterAccount(page: Page) {
	await page.goto("/admin/");
	await page.getByRole("heading", { name: /Welcome to/ }).waitFor();
	await page.getByLabel("Email", { exact: true }).fill("editor@riducms.test");
	await page.locator("#ridu-first-user-password").fill("ridu-documentation");
	await page.locator("#ridu-first-user-password-confirmation").fill("ridu-documentation");
	await page.getByRole("button", { name: "Create account" }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor();
}

async function exerciseStarterApplication(
	browser: Browser,
	app: RunningApp,
	captureAssets: boolean
) {
	const page = await createPage(browser, app.adminURL);
	await createStarterAccount(page);
	if (captureAssets) {
		await page.goto("/admin/");
		await page.getByRole("heading", { name: "Collections", exact: true }).waitFor();
		await capturePage(page, "ridu-admin-dashboard.png");
	}

	await page.goto("/admin/collections/posts/create");
	await page.getByLabel("Title", { exact: true }).fill("Hello from Ridu");
	await page.getByLabel("Status", { exact: true }).click();
	await page.getByRole("option", { name: "Published", exact: true }).click();
	await page.getByLabel("Author", { exact: true }).click();
	await page.getByRole("option", { name: /editor@riducms\.test/ }).click();
	const contentEditor = page.getByRole("textbox", { name: "Content" });
	await contentEditor.click();
	await contentEditor.pressSequentially("A first post authored in the clean Ridu starter.");
	if (!(await contentEditor.textContent())?.includes("A first post authored")) {
		throw new Error("starter rich-text editor did not accept the documentation content");
	}
	await page
		.getByRole("button", { name: /^(Save|Save draft|Publish|Publish changes)$/ })
		.first()
		.click();
	await page.waitForURL(/\/admin\/collections\/posts\/[^/]+/);
	await page.getByText(/successfully created\./).waitFor();
	if (captureAssets) {
		await capturePage(page, "ridu-admin-post-editor.png");

		const richText = page.getByRole("textbox", { name: "Content" });
		await richText.scrollIntoViewIfNeeded();
		const raw = resolve(temporaryDirectory, "rich-text.raw.png");
		await richText.locator("xpath=..").screenshot({ path: raw, animations: "disabled" });
		await normalizeScreenshot(raw, resolve(outputDirectory, "ridu-admin-rich-text.png"));

		await page.goto("/admin/collections/posts");
		await page.getByRole("heading", { name: "Posts", exact: true }).waitFor();
		await capturePage(page, "ridu-admin-posts-list.png");
	}
	await page.context().close();
}

async function smokeBlankApplication(browser: Browser, app: RunningApp) {
	const page = await createPage(browser, app.adminURL);
	await createStarterAccount(page);
	const schemaResponse = await page.request.get("/api/schema");
	if (!schemaResponse.ok()) {
		throw new Error(`blank-project schema smoke check: ${schemaResponse.status()}`);
	}
	const schema = (await schemaResponse.json()) as {
		schema?: { collections?: Array<{ slug?: string }> };
	};
	if (!schema.schema?.collections?.some((collection) => collection.slug === "users")) {
		throw new Error("blank-project schema smoke check did not expose users");
	}
	await page.context().close();
}

async function verifyGeneratedSDK(projectRoot: string, app: RunningApp) {
	const scriptPath = resolve(projectRoot, ".ridu/documentation-sdk-read.ts");
	await writeFile(
		scriptPath,
		`import { createClient } from "../generated/ridu.generated";

let sessionCookie = "";
const client = createClient({
	baseURL: process.env.RIDU_URL!,
	middleware: [async (request, next) => {
		const headers = new Headers(request.headers);
		if (sessionCookie) headers.set("Cookie", sessionCookie);
		const response = await next(new Request(request, { headers }));
		const setCookie = response.headers.get("set-cookie");
		const match = setCookie?.match(/(?:^|,\\s*)(ridu_session=[^;,\\s]+)/);
		if (match?.[1]) sessionCookie = match[1];
		return response;
	}],
});

await client.login("users", {
	email: process.env.RIDU_EMAIL!,
	password: process.env.RIDU_PASSWORD!,
});
const page = await client.list("posts", {
	where: { status: { equals: "published" } },
	select: { title: true, status: true },
});
if (!page.docs.some((post) => post.title === "Hello from Ridu")) {
	throw new Error("generated SDK did not return the Quickstart post");
}
console.log("generated SDK read verified");
`
	);
	try {
		await run("bun", [scriptPath], {
			cwd: projectRoot,
			env: {
				RIDU_URL: app.apiURL,
				RIDU_EMAIL: "editor@riducms.test",
				RIDU_PASSWORD: "ridu-documentation",
			},
		});
	} finally {
		await rm(scriptPath, { force: true });
	}
}

async function createFieldOverview() {
	const representatives = ["text", "select", "array", "relationship", "tabs", "plugin"];
	const inputs = representatives.map((slug) => resolve(fieldOutputDirectory, `${slug}.png`));
	await run("magick", [
		"-background",
		"#111012",
		"-gravity",
		"center",
		...inputs,
		"-append",
		"-strip",
		"-define",
		"png:exclude-chunk=date,time",
		resolve(outputDirectory, "ridu-admin-field-showcase.png"),
	]);
}

async function verifyGeneratedVariant(projectRoot: string) {
	await run(resolve(projectRoot, ".ridu/bin/ridu"), ["generate", "--check"], {
		cwd: projectRoot,
		env: { GOWORK: "off" },
	});
}

function startPreviewApplication() {
	return Bun.serve({
		hostname: "127.0.0.1",
		port: 18082,
		fetch(request) {
			const title = new URL(request.url).searchParams.get("title") ?? "Ridu preview";
			return new Response(
				`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="color-scheme" content="dark"><style>
:root{font-family:ui-sans-serif,system-ui;background:#111012;color:#f5f1f5}body{margin:0;padding:48px}
main{max-width:720px;margin:auto}.eyebrow{font:11px ui-monospace,monospace;letter-spacing:.14em;text-transform:uppercase;color:#d8a8c2}
h1{font:42px/1.05 ui-serif,Georgia,serif;margin:18px 0}p{color:#c7bec5;font-size:17px;line-height:1.7}
</style></head><body><main><div class="eyebrow">Live content preview</div><h1>${Bun.escapeHTML(title)}</h1><p>Edits from the Ridu admin arrive here without saving or rebuilding the application.</p></main>
<script>
const channel=new URL(location.href).searchParams.get('__ridu_preview');
const announce=()=>parent.postMessage({type:'ridu-live-preview',ready:true,channel},'*');
announce();
setInterval(announce,250);
addEventListener('message',(event)=>{const message=event.data;if(message?.type!=='ridu-live-preview'||message.channel!==channel)return;const next=message.data?.title;if(typeof next==='string')document.querySelector('h1').textContent=next;});
</script></body></html>`,
				{ headers: { "Content-Type": "text/html; charset=utf-8" } }
			);
		},
	});
}

await mkdir(captureRoot, { recursive: true });
await mkdir(outputDirectory, { recursive: true });
await mkdir(fieldOutputDirectory, { recursive: true });
await mkdir(temporaryDirectory, { recursive: true });

const manifest = JSON.parse(
	await readFile(resolve(playgroundRoot, "capture-manifest.json"), "utf8")
) as CaptureManifest;
if (manifest.version !== 1 || manifest.fields.length !== 24) {
	throw new Error("documentation capture manifest must contain version 1 and all 24 fields");
}

const generatedProjects = new Map<string, string>();
for (const variant of selectedVariants) {
	const target = reuseProjects
		? resolve(captureRoot, variant.name)
		: await generateVariant(variant);
	generatedProjects.set(variant.name, target);
}

const blankSQLite = generatedProjects.get("blank-sqlite");
const starterSQLite = generatedProjects.get("starter-sqlite");
if (blankSQLite === undefined || starterSQLite === undefined) {
	throw new Error("the capture workflow requires the starter and blank SQLite variants");
}
await overlayFieldGallery(blankSQLite);
for (const projectRoot of generatedProjects.values()) await verifyGeneratedVariant(projectRoot);

await rm(resolve(blankSQLite, ".ridu/documentation.sqlite"), { force: true });
await rm(resolve(blankSQLite, ".ridu/documentation-uploads"), { recursive: true, force: true });
await rm(resolve(starterSQLite, ".ridu/documentation.sqlite"), { force: true });

const browser = await chromium.launch();
const previewApplication = startPreviewApplication();
try {
	const fieldApp = startSQLiteApp(blankSQLite, 18101, 18102);
	try {
		await waitForURL(`${fieldApp.apiURL}/readyz`, fieldApp);
		await waitForURL(`${fieldApp.adminURL}/admin/`, fieldApp);
		await seedFieldGallery(blankSQLite);
		await captureFieldApplication(browser, fieldApp, manifest);
	} finally {
		await stopApp(fieldApp);
	}

	const starterApp = startSQLiteApp(starterSQLite, 18111, 18112);
	try {
		await waitForURL(`${starterApp.apiURL}/readyz`, starterApp);
		await waitForURL(`${starterApp.adminURL}/admin/`, starterApp);
		await exerciseStarterApplication(browser, starterApp, true);
		await verifyGeneratedSDK(starterSQLite, starterApp);
	} finally {
		await stopApp(starterApp);
	}

	if (!sqliteOnly) {
		const starterPostgres = generatedProjects.get("starter-postgres");
		const blankPostgres = generatedProjects.get("blank-postgres");
		if (starterPostgres === undefined || blankPostgres === undefined) {
			throw new Error("the complete capture workflow requires both PostgreSQL variants");
		}

		await resetPostgresProject(starterPostgres);
		const starterPostgresApp = startPostgresApp(starterPostgres, 18121, 18122);
		try {
			await waitForURL(`${starterPostgresApp.apiURL}/readyz`, starterPostgresApp);
			await waitForURL(`${starterPostgresApp.adminURL}/admin/`, starterPostgresApp);
			await exerciseStarterApplication(browser, starterPostgresApp, false);
			await verifyGeneratedSDK(starterPostgres, starterPostgresApp);
		} finally {
			await stopApp(starterPostgresApp);
			await resetPostgresProject(starterPostgres);
		}

		await resetPostgresProject(blankPostgres);
		const blankPostgresApp = startPostgresApp(blankPostgres, 18131, 18132);
		try {
			await waitForURL(`${blankPostgresApp.apiURL}/readyz`, blankPostgresApp);
			await waitForURL(`${blankPostgresApp.adminURL}/admin/`, blankPostgresApp);
			await smokeBlankApplication(browser, blankPostgresApp);
		} finally {
			await stopApp(blankPostgresApp);
			await resetPostgresProject(blankPostgres);
		}
	}
} finally {
	previewApplication.stop(true);
	await browser.close();
}

await createFieldOverview();
await rm(temporaryDirectory, { recursive: true, force: true });
if (!keepProjects) {
	for (const projectRoot of generatedProjects.values()) {
		await rm(projectRoot, { recursive: true, force: true });
	}
}

console.log(`captured clean documentation images in ${outputDirectory}`);
