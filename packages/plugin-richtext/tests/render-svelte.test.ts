import { expect, it } from "bun:test";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { compile } from "svelte/compiler";
import { render } from "svelte/server";
import { build, type Plugin } from "vite";

it("renders Svelte block components without importing an editor", async () => {
	const dependencyGraph = await publishedRendererDependencyGraph();
	expect(dependencyGraph.some((id) => id.endsWith("/src/render/index.ts"))).toBe(true);
	expect(dependencyGraph.some((id) => id.endsWith("/src/render/rich-text.svelte"))).toBe(true);
	expect(dependencyGraph.filter(isEditorDependency)).toEqual([]);

	const parent = fileURLToPath(new URL("../.ridu/", import.meta.url));
	await mkdir(parent, { recursive: true });
	const directory = await mkdtemp(`${parent}richtext-render-`);
	try {
		const source = await readFile(
			new URL("../src/render/rich-text.svelte", import.meta.url),
			"utf8"
		);
		const compiled = compile(source, {
			filename: "rich-text.svelte",
			generate: "server",
		}).js.code;
		await writeFile(`${directory}/rich-text.mjs`, compiled);
		const block = compile(
			`<script lang="ts">let { block } = $props();</script><aside>{block.title}</aside>`,
			{ filename: "callout.svelte", generate: "server" }
		).js.code;
		await writeFile(`${directory}/callout.mjs`, block);
		const { default: RichText } = await import(`${directory}/rich-text.mjs`);
		const { default: Callout } = await import(`${directory}/callout.mjs`);
		const value = {
			version: 1,
			root: {
				type: "root",
				children: [
					{ type: "paragraph", children: [{ type: "text", text: "<safe>" }] },
					{
						type: "block",
						version: 1,
						fields: { blockType: "callout", _key: "one", title: "Hello" },
					},
				],
			},
		};
		const result = render(RichText, { props: { value, blocks: { callout: Callout } } });
		expect(result.body).toContain("&lt;safe&gt;");
		expect(result.body).toContain("Hello</aside>");
		expect(() => render(RichText, { props: { value } }).body).toThrow("No renderer registered");
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
});

async function publishedRendererDependencyGraph() {
	const packageRoot = fileURLToPath(new URL("../", import.meta.url));
	const manifest = JSON.parse(
		await readFile(new URL("../package.json", import.meta.url), "utf8")
	) as { exports?: Record<string, unknown> };
	const entry = manifest.exports?.["./svelte"];
	if (typeof entry !== "string")
		throw new Error("package does not publish a Svelte renderer entry");

	const dependencies = new Set<string>();
	const captureGraph: Plugin = {
		name: "ridu-rich-text-renderer-dependency-graph",
		moduleParsed(module) {
			dependencies.add(module.id);
			for (const dependency of [...module.importedIds, ...module.dynamicallyImportedIds]) {
				dependencies.add(dependency);
			}
		},
	};
	await build({
		root: packageRoot,
		configFile: false,
		logLevel: "silent",
		plugins: [svelte({ configFile: false, compilerOptions: { runes: true } }), captureGraph],
		ssr: { noExternal: true },
		build: {
			write: false,
			ssr: true,
			rollupOptions: { input: entry },
		},
	});
	return [...dependencies].map((id) => id.replaceAll("\\", "/")).sort();
}

function isEditorDependency(id: string) {
	const module = id.split("?", 1)[0]!;
	return (
		module === "lexical" ||
		module.startsWith("@lexical/") ||
		module === "@hvniel/lexical-svelte" ||
		module.startsWith("@hvniel/lexical-svelte/") ||
		module.includes("/node_modules/lexical/") ||
		module.includes("/node_modules/@lexical/") ||
		module.includes("/node_modules/@hvniel/lexical-svelte/")
	);
}
