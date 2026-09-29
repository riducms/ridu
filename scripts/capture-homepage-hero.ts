// Captures the homepage hero: the real Ridu admin editing a journal post, with
// a designed frontend in its live-preview panel. Like the documentation
// captures, it runs a generated application's production binary; nothing in
// the admin is mocked. `--reuse-project` skips regenerating the application.
import { copyFile, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { chromium } from "@playwright/test";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const heroRoot = resolve(repositoryRoot, "playground/homepage-hero");
const captureRoot = resolve(repositoryRoot, "playground/.ridu/homepage-hero");
const projectRoot = resolve(captureRoot, "app");
const fontRoot = resolve(repositoryRoot, "website/node_modules");
const output = resolve(repositoryRoot, "website/src/assets/home/ridu-live-preview.png");
const reuseProject = process.argv.includes("--reuse-project");
const apiPort = 18140;
const previewPort = 18090;
const apiURL = `http://127.0.0.1:${apiPort}`;
const databasePath = resolve(projectRoot, ".ridu/hero.sqlite");
const uploadRoot = resolve(projectRoot, ".ridu/hero-uploads");

async function run(
	command: string,
	args: string[],
	cwd = repositoryRoot,
	env: Record<string, string> = {}
) {
	const child = Bun.spawn([command, ...args], {
		cwd,
		env: { ...Bun.env, GOWORK: "off", ...env },
		stdin: "ignore",
		stdout: "inherit",
		stderr: "inherit",
	});
	if ((await child.exited) !== 0) throw new Error(`${command} ${args.join(" ")} failed`);
}

async function generateProject() {
	await rm(projectRoot, { recursive: true, force: true });
	await run("go", [
		"run",
		"./internal/dogfood/new",
		"--target",
		projectRoot,
		"--module",
		"example.com/ridu-homepage-hero",
		"--scope",
		"@ridu-hero",
		"--template",
		"blank",
		"--database",
		"sqlite",
		"--agent",
		"none",
	]);
}

async function overlayJournal() {
	await mkdir(resolve(projectRoot, "cmd/seed"), { recursive: true });
	await rm(resolve(projectRoot, "content/posts.go"), { force: true });
	for (const path of ["content/config.go", "content/journal.go", "cmd/seed/main.go"]) {
		await copyFile(resolve(heroRoot, path), resolve(projectRoot, path));
	}
	// Give the generated server local upload storage, as the documentation capture does.
	const serverPath = resolve(projectRoot, "cmd/server/main.go");
	let server = await readFile(serverPath, "utf8");
	const addressOption = "\t\tridu.WithAddress(serverAddress()),";
	if (!server.includes(addressOption)) {
		throw new Error("generated server no longer has the expected upload-storage insertion point");
	}
	if (!server.includes("ridu.WithUploadStorage(")) {
		server = server
			.replace(
				'"github.com/riducms/ridu/adapters/sqlite"\n',
				'"github.com/riducms/ridu/adapters/sqlite"\n\tlocalstorage "github.com/riducms/ridu/adapters/storage/local"\n'
			)
			.replace(
				'"github.com/riducms/ridu/store"\n',
				'"github.com/riducms/ridu/storage"\n\t"github.com/riducms/ridu/store"\n'
			)
			.replace(
				addressOption,
				"\t\tridu.WithUploadStorage(func(context.Context) (storage.Backend, error) {\n" +
					'\t\t\treturn localstorage.New(filepath.Join(".ridu", "hero-uploads"))\n' +
					"\t\t}),\n" +
					addressOption
			);
		await writeFile(serverPath, server);
	}
	await run("gofmt", ["-w", "content", "cmd"], projectRoot);
	const ridu = resolve(projectRoot, ".ridu/bin/ridu");
	await run(ridu, ["generate"], projectRoot);
	await rm(resolve(projectRoot, "migrations"), { recursive: true, force: true });
	await mkdir(resolve(projectRoot, "migrations"), { recursive: true });
	await run(ridu, ["migrate", "create", "--name", "homepage-hero"], projectRoot);
	await run(ridu, ["build"], projectRoot);
}

async function prepareDatabase() {
	await rm(databasePath, { force: true });
	await rm(uploadRoot, { recursive: true, force: true });
	const ridu = resolve(projectRoot, ".ridu/bin/ridu");
	await run(ridu, ["migrate", "up", "--database-path", databasePath], projectRoot);
	await run("go", ["run", "./cmd/seed"], projectRoot, {
		RIDU_HERO_DATABASE_PATH: databasePath,
		RIDU_HERO_UPLOAD_ROOT: uploadRoot,
	});
}

function startApplication() {
	return Bun.spawn([resolve(projectRoot, "dist/app")], {
		cwd: projectRoot,
		env: { ...Bun.env, RIDU_SQLITE_PATH: databasePath, RIDU_ADDRESS: `127.0.0.1:${apiPort}` },
		stdin: "ignore",
		stdout: "ignore",
		stderr: "inherit",
	});
}

async function waitForReady(application: Bun.Subprocess) {
	const deadline = Date.now() + 60_000;
	while (Date.now() < deadline) {
		if (application.exitCode !== null) throw new Error("the hero application exited early");
		try {
			if ((await fetch(`${apiURL}/readyz`)).ok) return;
		} catch {
			// Not listening yet.
		}
		await Bun.sleep(250);
	}
	throw new Error("timed out waiting for the hero application");
}

// ---- The journal frontend shown inside the live-preview panel ----

interface RichNode {
	type?: string;
	tag?: string;
	text?: string;
	format?: number;
	children?: RichNode[];
}

// renderRichText is also sent to the browser, so it must stay self-contained.
function renderRichText(document: { root?: RichNode } | undefined): string {
	const escape = (value: string) =>
		value.replace(/[&<>"]/g, (character) => `&#${character.charCodeAt(0)};`);
	const inline = (nodes: RichNode[] = []) =>
		nodes
			.map((node) => {
				let html = escape(node.text ?? "");
				if ((node.format ?? 0) & 1) html = `<strong>${html}</strong>`;
				if ((node.format ?? 0) & 2) html = `<em>${html}</em>`;
				return html;
			})
			.join("");
	return (document?.root?.children ?? [])
		.map((node) => {
			if (node.type === "heading")
				return `<h2 class="eyebrow section">${inline(node.children)}</h2>`;
			if (node.type === "quote") return `<blockquote>${inline(node.children)}</blockquote>`;
			if (node.type === "paragraph") return `<p>${inline(node.children)}</p>`;
			return "";
		})
		.join("");
}

// renderTitle sets the part before a colon in a heavier weight.
function renderTitle(title: string): string {
	const escape = (value: string) =>
		value.replace(/[&<>"]/g, (character) => `&#${character.charCodeAt(0)};`);
	const split = title.indexOf(":");
	if (split < 0) return escape(title);
	return `<strong>${escape(title.slice(0, split + 1))}</strong>${escape(title.slice(split + 1))}`;
}

const journalStyles = `
@font-face{font-family:Geist;src:url(/fonts/geist.woff2) format('woff2');font-weight:100 900}
@font-face{font-family:Martian;src:url(/fonts/martian.woff2) format('woff2');font-weight:100 900}
:root{--bg:#0d0b10;--ink:#f4eff5;--soft:#c8bfcb;--faint:#83798a;--rose:#f27eb2;--rule:rgba(244,239,245,.075)}
*{box-sizing:border-box}
html{background:var(--bg)}
body{margin:0;min-height:100vh;color:var(--ink);font-family:Geist,system-ui,sans-serif;-webkit-font-smoothing:antialiased;
	background:radial-gradient(900px 520px at 18% -8%,rgba(242,126,178,.10),transparent 62%),radial-gradient(700px 480px at 100% 30%,rgba(150,120,255,.06),transparent 60%),var(--bg)}
.page{position:relative;width:min(720px,calc(100% - 64px));margin:0 auto;padding:0 28px 120px}
.page::before,.page::after{content:"";position:absolute;top:0;bottom:0;width:1px;
	background:repeating-linear-gradient(to bottom,var(--rule) 0 3px,transparent 3px 9px)}
.page::before{left:0}.page::after{right:0}
header{display:flex;align-items:center;justify-content:space-between;padding-top:30px}
.brand{display:flex;align-items:center;gap:10px;color:var(--ink);text-decoration:none;font-size:14px;font-weight:500;letter-spacing:.01em}
nav{display:flex;gap:26px}
nav a,.eyebrow{font:500 10px/1 Martian,ui-monospace,monospace;letter-spacing:.18em;text-transform:uppercase}
nav a{color:var(--faint);text-decoration:none}nav a[aria-current]{color:var(--rose)}
.date{margin:92px 0 0;color:var(--rose)}
h1{margin:22px 0 0;font-size:clamp(34px,5.4vw,54px);line-height:1.04;font-weight:300;letter-spacing:-.018em;text-wrap:balance}
h1 strong{font-weight:620}
figure{margin:48px calc(-1 * min(64px, (100vw - 100%) / 2 - 14px)) 0;border-radius:14px;overflow:hidden;box-shadow:0 30px 80px rgba(0,0,0,.45),0 0 0 1px rgba(255,255,255,.04)}
figure img{display:block;width:100%;aspect-ratio:2.05;object-fit:cover}
.section{margin:64px 0 0;color:var(--rose);font-weight:500}
p{margin:22px 0 0;font:17px/1.78 Georgia,'Times New Roman',serif;color:var(--soft)}
`;

async function renderJournal(request: Request, id: string) {
	const url = new URL(request.url);
	const token = url.searchParams.get("__ridu_preview_token");
	if (!token) return new Response("Missing preview capability", { status: 401 });
	// Read the saved draft with the scoped capability, as a real frontend would.
	const postResponse = await fetch(
		`${apiURL}/api/preview/collections/posts/${encodeURIComponent(id)}`,
		{
			headers: { Authorization: `Bearer ${token}` },
		}
	);
	if (!postResponse.ok)
		return new Response(await postResponse.text(), { status: postResponse.status });
	const postBody = await postResponse.json();
	const post = postBody.doc ?? postBody;
	let banner = "";
	if (typeof post.banner === "string" && post.banner !== "") {
		const mediaResponse = await fetch(
			`${apiURL}/api/collections/media/${encodeURIComponent(post.banner)}`
		);
		const mediaBody = await mediaResponse.json();
		const media = mediaBody.doc ?? mediaBody;
		if (typeof media.url === "string") banner = new URL(media.url, apiURL).href;
	}
	const published = new Date(post.createdAt).toLocaleDateString("en-US", {
		year: "numeric",
		month: "long",
		day: "numeric",
	});
	const html = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="dark"><title>Field Notes</title><style>${journalStyles}</style></head>
<body><div class="page">
<header>
	<a class="brand" href="#"><svg width="26" height="26" viewBox="0 0 32 32" aria-hidden="true"><circle cx="16" cy="16" r="12" fill="none" stroke="#f4eff5" stroke-opacity=".75" stroke-width="1.4"/><path d="M16 4a12 12 0 0 1 0 24" fill="none" stroke="#f27eb2" stroke-width="1.4"/><circle cx="16" cy="16" r="3.2" fill="#f27eb2"/></svg>Field Notes</a>
	<nav><a href="#">Work</a><a href="#" aria-current="page">Journal</a><a href="#">Studio</a><a href="#">Contact</a></nav>
</header>
<p class="eyebrow date">${published}</p>
<h1 data-title>${renderTitle(String(post.title ?? ""))}</h1>
${banner ? `<figure><img src="${banner}" alt=""></figure>` : ""}
<article data-content>${renderRichText(post.content)}</article>
</div>
<script>
const renderRichText=${renderRichText.toString()};
const renderTitle=${renderTitle.toString()};
const channel=new URL(location.href).searchParams.get('__ridu_preview');
const announce=()=>parent.postMessage({type:'ridu-live-preview',ready:true,channel},'*');
announce();setInterval(announce,250);
addEventListener('message',(event)=>{
	const message=event.data;
	if(message?.type!=='ridu-live-preview'||message.channel!==channel||!message.data)return;
	if(typeof message.data.title==='string')document.querySelector('[data-title]').innerHTML=renderTitle(message.data.title);
	if(message.data.content&&typeof message.data.content==='object')document.querySelector('[data-content]').innerHTML=renderRichText(message.data.content);
});
</script></body></html>`;
	return new Response(html, { headers: { "Content-Type": "text/html; charset=utf-8" } });
}

function startJournal() {
	return Bun.serve({
		hostname: "127.0.0.1",
		port: previewPort,
		async fetch(request) {
			const path = new URL(request.url).pathname;
			if (path === "/fonts/geist.woff2") {
				return new Response(
					Bun.file(
						resolve(fontRoot, "@fontsource-variable/geist/files/geist-latin-wght-normal.woff2")
					)
				);
			}
			if (path === "/fonts/martian.woff2") {
				return new Response(
					Bun.file(
						resolve(
							fontRoot,
							"@fontsource-variable/martian-mono/files/martian-mono-latin-wght-normal.woff2"
						)
					)
				);
			}
			const id = path.match(/^\/journal\/([^/]+)$/)?.[1];
			if (id !== undefined) return renderJournal(request, decodeURIComponent(id));
			return new Response("Not found", { status: 404 });
		},
	});
}

// ---- Capture ----

async function capture() {
	const browser = await chromium.launch();
	try {
		const context = await browser.newContext({
			baseURL: apiURL,
			colorScheme: "dark",
			deviceScaleFactor: 2,
			locale: "en-US",
			viewport: { width: 1600, height: 1000 },
		});
		await context.addInitScript(() => localStorage.setItem("ridu-theme", "dark"));
		const page = await context.newPage();
		await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
		await page.goto("/admin/login");
		await page.getByLabel("Email address").fill("editor@riducms.test");
		await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-homepage");
		await page.getByRole("button", { name: "Sign in" }).click();
		await page.getByRole("navigation", { name: "Admin navigation" }).waitFor({ state: "attached" });
		await page.goto("/admin/collections/posts/hero-post");
		// Collapse the navigation so the editor and preview get the full width.
		const navigationToggle = page.locator('button[aria-controls="ridu-admin-navigation"]');
		if ((await navigationToggle.getAttribute("aria-expanded")) === "true")
			await navigationToggle.click();
		await page.getByRole("button", { name: "Live preview", exact: true }).click();
		await page.getByRole("region", { name: "Live preview" }).waitFor();
		await page.getByText("Connected", { exact: true }).waitFor();
		const frame = page.frameLocator('iframe[title*="preview" i]').first();
		await frame.locator("figure img").waitFor();
		await frame.locator("figure img").evaluate((image: HTMLImageElement) => image.decode());
		await page.waitForLoadState("networkidle");
		await page.evaluate(async () => await document.fonts.ready);
		await page.waitForTimeout(400);
		await mkdir(dirname(output), { recursive: true });
		const raw = resolve(captureRoot, "hero.raw.png");
		await page.screenshot({ path: raw, animations: "disabled" });
		await run("magick", [raw, "-strip", "-define", "png:exclude-chunk=date,time", output]);
		console.log(`captured ${output}`);
	} finally {
		await browser.close();
	}
}

await mkdir(captureRoot, { recursive: true });
if (!reuseProject) await generateProject();
await overlayJournal();
await prepareDatabase();
const journal = startJournal();
const application = startApplication();
try {
	await waitForReady(application);
	await capture();
} finally {
	application.kill("SIGINT");
	await Promise.race([application.exited, Bun.sleep(5_000)]);
	journal.stop(true);
}
