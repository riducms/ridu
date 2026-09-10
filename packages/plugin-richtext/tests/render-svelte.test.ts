import { expect, it } from "bun:test";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { compile } from "svelte/compiler";
import { render } from "svelte/server";

it("renders Svelte block components without importing an editor", async () => {
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
		expect(compiled).not.toContain("lexical");
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
