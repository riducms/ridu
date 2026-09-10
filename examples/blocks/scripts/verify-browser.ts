import { chromium, expect } from "@playwright/test";
import { createServer } from "vite";
import { page } from "./seed";
import { createClient } from "../generated/ridu.generated";

const server = await createServer({ server: { port: 0 } });
await server.listen();
try {
	const origin = server.resolvedUrls?.local[0];
	if (!origin) throw new Error("Reference Vite server did not bind a local port");
	const browser = await chromium.launch({ headless: true });
	try {
		const tab = await browser.newPage();
		const errors: string[] = [];
		tab.on("pageerror", (error) => errors.push(error.message));
		await tab.goto(`${origin}?page=${encodeURIComponent(page.id)}`);
		await expect(tab.getByRole("heading", { name: "Hello", exact: true })).toBeVisible();
		await expect(tab.getByRole("heading", { name: "Story", exact: true })).toBeVisible();
		await expect(tab.locator("section p strong")).toHaveText("The story continues in rich text.");
		await expect(tab.getByRole("img", { name: "Mountains" })).toBeVisible();
		await expect
			.poll(() =>
				tab
					.getByRole("img", { name: "Mountains" })
					.evaluate((image: HTMLImageElement) => image.naturalWidth)
			)
			.toBeGreaterThan(0);
		await expect(tab.locator("aside")).toHaveText("Read more");
		if (errors.length) throw new Error(errors.join("\n"));
		const client = createClient({
			baseURL: process.env.RIDU_URL ?? "http://localhost:8080",
		});
		const reread = await client.find("pages", page.id);
		if (
			reread.layout?.map((block) => block._key).join(",") !==
			page.layout?.map((block) => block._key).join(",")
		)
			throw new Error("Browser rendering changed occurrence identities");

		// A stale frontend must expose an unknown API variant without hiding the other blocks.
		await tab.route(`**/api/collections/pages/${page.id}?*`, async (route) => {
			const response = await route.fetch();
			const body = await response.json();
			body.doc.layout.splice(1, 0, { blockType: "future", _key: "future-block" });
			await route.fulfill({ response, json: body });
		});
		await tab.reload();
		await expect(tab.getByRole("alert")).toHaveText(
			"No renderer for this block. Add its renderer to Blocks.svelte."
		);
		await expect(tab.getByRole("heading", { name: "Hello", exact: true })).toBeVisible();
		await expect(tab.locator("section p strong")).toHaveText("The story continues in rich text.");
		await expect(tab.locator("aside")).toHaveText("Read more");

		const referencePage = await client.create("pages", {
			title: "Embedded reference",
			layout: [
				{
					blockType: "content",
					body: {
						version: 1,
						root: {
							type: "root",
							children: [{ type: "relationship", relationTo: "pages", id: page.id }],
						},
					},
				},
				{ blockType: "hero", heading: "After the reference" },
			],
		});
		await client.publish("pages", referencePage.id, { revision: referencePage._revision });
		await tab.goto(`${origin}?page=${encodeURIComponent(referencePage.id)}`);
		await expect(tab.getByRole("alert")).toHaveText(
			"No renderer registered for rich-text node relationship"
		);
		await expect(tab.getByRole("heading", { name: "After the reference" })).toBeVisible();
		if (errors.length) throw new Error(errors.join("\n"));
		console.log("Reference SDK → API → Svelte browser → API contract passed");
	} finally {
		await browser.close();
	}
} finally {
	await server.close();
}
