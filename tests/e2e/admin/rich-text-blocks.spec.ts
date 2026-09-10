import { readFile } from "node:fs/promises";
import type { SchemaField } from "@riducms/protocol";
import type { Page } from "@playwright/test";
import { insertBlock, bodyEditor, bodyCards, richDocument, block } from "./rich-text-block-fixture";
import { expect, test } from "./fixture";
import {
	chooseContentLocale,
	documentSaveButton,
	loginAsEditor,
	observePageErrors,
} from "./helpers";

test.use({ actionTimeout: 10_000 });

async function saveCollection(page: Page, collection: string) {
	const saved = page.waitForResponse(
		(response) =>
			["POST", "PATCH"].includes(response.request().method()) &&
			new RegExp(`/api/collections/${collection}(?:/[^/]+)?$`).test(
				new URL(response.url()).pathname
			)
	);
	await documentSaveButton(page).click();
	const response = await saved;
	expect(response.ok(), await response.text()).toBe(true);
	return (await response.json()).doc;
}
const saveArticle = (page: Page) => saveCollection(page, "block-articles");

test("schema blocks insert, validate, apply, cancel, duplicate, undo, remove, and reload", async ({
	page,
}) => {
	test.setTimeout(90_000);
	const errors = observePageErrors(page);
	await loginAsEditor(page);
	errors.consoleErrors.length = 0;
	await page.goto("/admin/collections/block-articles/create");
	await page.getByRole("textbox", { name: "Title", exact: true }).fill("Structured authoring");
	const editor = bodyEditor(page);
	let drawer = await insertBlock(page, editor, "Callout");
	await drawer.getByRole("button", { name: "Cancel", exact: true }).click();
	await expect(bodyCards(page)).toHaveCount(0);

	drawer = await insertBlock(page, editor, "Callout");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await expect(drawer).toBeVisible();
	await expect(drawer.getByRole("textbox", { name: "Callout title", exact: true })).toBeFocused();
	await drawer.getByRole("textbox", { name: "Callout title", exact: true }).fill("First callout");
	await expect(drawer.getByRole("textbox", { name: "Caption", exact: true })).toHaveValue(
		"Helpful context"
	);
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await expect(bodyCards(page)).toHaveCount(1);
	await expect(page.getByRole("textbox", { name: "Detail", exact: true })).toHaveCount(0);
	const originalKey = await bodyCards(page).first().getAttribute("data-block-key");
	expect(originalKey).toBeTruthy();

	await bodyCards(page).first().getByRole("button", { name: "Edit", exact: true }).click();
	drawer = page.getByRole("dialog", { name: "Edit Callout", exact: true });
	await drawer
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Discard this edit");
	await drawer.getByRole("button", { name: "Cancel", exact: true }).click();
	await expect(bodyCards(page).first()).toContainText("First callout");
	await bodyCards(page)
		.first()
		.getByRole("button", { name: "Select Callout block" })
		.press("Enter");
	drawer = page.getByRole("dialog", { name: "Edit Callout", exact: true });
	await drawer.getByRole("textbox", { name: "Callout title", exact: true }).fill("Applied edit");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await expect(bodyCards(page).first()).toContainText("Applied edit");
	await editor.focus();
	await page.keyboard.press("ControlOrMeta+z");
	await expect(bodyCards(page).first()).toContainText("First callout");
	await expect(bodyCards(page).first()).toHaveAttribute("data-block-key", originalKey!);
	await page.keyboard.press("ControlOrMeta+Shift+z");
	await expect(bodyCards(page).first()).toContainText("Applied edit");
	await page.keyboard.press("ControlOrMeta+z");
	await expect(bodyCards(page).first()).toContainText("First callout");
	await bodyCards(page).first().getByRole("button", { name: "Duplicate", exact: true }).click();
	await expect(bodyCards(page)).toHaveCount(2);
	expect(await bodyCards(page).last().getAttribute("data-block-key")).not.toBe(originalKey);
	await editor.focus();
	await page.keyboard.press("ControlOrMeta+z");
	await expect(bodyCards(page)).toHaveCount(1);
	await page.keyboard.press("ControlOrMeta+Shift+z");
	await expect(bodyCards(page)).toHaveCount(2);
	const duplicateKey = await bodyCards(page).last().getAttribute("data-block-key");
	await bodyCards(page)
		.last()
		.getByRole("button", { name: "Select Callout block" })
		.press("Delete");
	await expect(bodyCards(page)).toHaveCount(1);
	await editor.focus();
	await page.keyboard.press("ControlOrMeta+z");
	await expect(bodyCards(page)).toHaveCount(2);
	await expect(bodyCards(page).last()).toHaveAttribute("data-block-key", duplicateKey!);
	await bodyCards(page).last().getByRole("button", { name: "Remove", exact: true }).click();
	await expect(bodyCards(page)).toHaveCount(1);
	const stored = await saveArticle(page);
	const node = stored.body.root.children.find((node: { type: string }) => node.type === "block");
	expect(Object.keys(node).sort()).toEqual(["fields", "type", "version"]);
	expect(node.fields).toMatchObject({
		_key: originalKey,
		blockType: "callout",
		title: "First callout",
		caption: "Helpful context",
	});
	await page.reload();
	await expect(bodyCards(page)).toHaveCount(1);
	await expect(bodyCards(page).first()).toHaveAttribute("data-block-key", originalKey!);
	await expect(bodyCards(page).first()).toContainText("First callout");
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("lazy nested editors keep independent histories and commit only with the outer block", async ({
	page,
}) => {
	test.setTimeout(90_000);
	const errors = observePageErrors(page);
	await loginAsEditor(page);
	errors.consoleErrors.length = 0;
	await page.goto("/admin/collections/block-articles/create");
	await page.getByRole("textbox", { name: "Title", exact: true }).fill("Nested editors");
	const outer = await insertBlock(page, bodyEditor(page), "Callout");
	await outer.getByRole("textbox", { name: "Callout title", exact: true }).fill("Nested callout");
	const detail = outer.getByRole("textbox", { name: "Detail", exact: true });
	const extra = outer.getByRole("textbox", { name: "Extra detail", exact: true });
	await expect(detail).toBeVisible();
	await expect(extra).toBeVisible();
	await detail.fill("Detail text");
	await extra.fill("Extra text");
	const inner = await insertBlock(page, detail, "CTA");
	await inner.getByRole("textbox", { name: /^CTA label/ }).fill("Read more");
	await inner.getByRole("button", { name: "Apply", exact: true }).click();
	await expect(detail.locator('[data-block-type="cta"]')).toHaveCount(1);
	await extra.focus();
	await page.keyboard.press("ControlOrMeta+z");
	await expect(detail.locator('[data-block-type="cta"]')).toHaveCount(1);
	await extra.fill("Independent extra text");
	await outer.getByRole("button", { name: "Apply", exact: true }).click();
	await expect(page.getByRole("textbox", { name: "Detail", exact: true })).toHaveCount(0);
	await bodyCards(page).first().getByRole("button", { name: "Edit", exact: true }).click();
	const reopened = page.getByRole("dialog", { name: "Edit Callout", exact: true });
	await expect(reopened.getByRole("textbox", { name: "Detail", exact: true })).toContainText(
		"Read more"
	);
	await expect(reopened.getByRole("textbox", { name: "Extra detail", exact: true })).toContainText(
		"Independent extra text"
	);
	await reopened.getByRole("button", { name: "Cancel", exact: true }).click();
	const stored = await saveArticle(page);
	const node = stored.body.root.children.find((node: { type: string }) => node.type === "block");
	expect(
		node.fields.detail.root.children.some(
			(node: { type: string; fields?: { label?: string } }) =>
				node.type === "block" && node.fields?.label === "Read more"
		)
	).toBe(true);
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("shared child localization and whole-document locales preserve block identities", async ({
	page,
}) => {
	test.setTimeout(60_000);
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/block-articles?locale=en&draft=true", {
		data: {
			title: "Localized block article",
			body: richDocument([block("cta", { label: "English shared CTA" })]),
			localizedBody: richDocument([block("cta", { label: "English document CTA" })]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/block-articles/${original.id}?locale=fr`);
	await bodyCards(page).first().getByRole("button", { name: "Edit", exact: true }).click();
	let drawer = page.getByRole("dialog", { name: "Edit CTA", exact: true });
	await expect(drawer.getByRole("textbox", { name: /^CTA label/ })).toHaveValue(
		"English shared CTA"
	);
	await drawer.getByRole("textbox", { name: /^CTA label/ }).fill("CTA partagé français");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	const localizedCards = page.locator(
		'[data-field-path="localizedBody"] .ridu-richtext-embedded-card'
	);
	await localizedCards.first().getByRole("button", { name: "Edit", exact: true }).click();
	drawer = page.getByRole("dialog", { name: "Edit CTA", exact: true });
	await drawer.getByRole("textbox", { name: /^CTA label/ }).fill("Document français");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await saveArticle(page);
	const all = (
		await (
			await page.request.get(`/api/collections/block-articles/${original.id}?locale=all`)
		).json()
	).doc;
	expect(all.body.root.children[0].fields._key).toBe(original.body.root.children[0].fields._key);
	expect(all.body.root.children[0].fields.label).toEqual({
		en: "English shared CTA",
		fr: "CTA partagé français",
	});
	expect(all.localizedBody.en.root.children[0].fields._key).toBe(
		all.localizedBody.fr.root.children[0].fields._key
	);
	expect(all.localizedBody.en.root.children[0].fields.label).toBe("English document CTA");
	expect(all.localizedBody.fr.root.children[0].fields.label).toBe("Document français");
	await chooseContentLocale(page, "English", "en");
	await expect(bodyCards(page).first()).toContainText("English shared CTA");
	await expect(localizedCards.first()).toContainText("English document CTA");
});

test("pending validation blocks edits and received issues follow keyboard reorder", async ({
	page,
}) => {
	test.setTimeout(60_000);
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Exact issues",
			body: richDocument([
				block("callout", { title: "First" }),
				block("callout", { title: "Second" }),
			]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/block-articles/${original.id}`);
	const key = original.body.root.children[0].fields._key;
	const failing = page.locator(`[data-block-key="${key}"]`);
	await failing.getByRole("button", { name: "Edit", exact: true }).click();
	let drawer = page.getByRole("dialog", { name: "Edit Callout", exact: true });
	await drawer.getByRole("textbox", { name: "Callout title", exact: true }).fill("invalid");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	let release!: () => void;
	let captured!: () => void;
	const waiting = new Promise<void>((resolve) => {
		release = resolve;
	});
	const responseReady = new Promise<void>((resolve) => {
		captured = resolve;
	});
	await page.route(`**/api/collections/block-articles/${original.id}*`, async (route) => {
		if (route.request().method() !== "PATCH") {
			await route.continue();
			return;
		}
		const response = await route.fetch();
		captured();
		await waiting;
		await route.fulfill({ response });
	});
	await documentSaveButton(page).click();
	await responseReady;
	await expect(failing.getByRole("button", { name: "Select Callout block" })).toBeDisabled();
	await expect(bodyEditor(page)).toHaveAttribute("contenteditable", "false");
	release();
	await expect(failing).toHaveAttribute("data-invalid", "true");
	await failing.getByRole("button", { name: "Select Callout block" }).press("Alt+Shift+ArrowDown");
	await expect(bodyCards(page).last()).toHaveAttribute("data-block-key", key);
	await expect(failing).toHaveAttribute("data-invalid", "true");
	await expect(bodyCards(page).first()).toHaveAttribute("data-invalid", "false");
	await failing.getByRole("button", { name: "Edit", exact: true }).click();
	drawer = page.getByRole("dialog", { name: "Edit Callout", exact: true });
	await expect(
		drawer.getByText("Use a descriptive callout title", { exact: true }).first()
	).toBeVisible();
	await expect(drawer.getByRole("textbox", { name: "Callout title", exact: true })).toBeFocused();
	await drawer.getByRole("textbox", { name: "Callout title", exact: true }).fill("Corrected title");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await page.unroute(`**/api/collections/block-articles/${original.id}*`);
	await saveArticle(page);
});

test("native block clipboard refreshes identities and rejects unavailable variants", async ({
	page,
}) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Clipboard article",
			body: richDocument([
				block("callout", { title: "Copy me", links: [{ label: "Nested row" }] }),
			]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/block-articles/${original.id}`);
	await bodyCards(page).first().getByRole("button", { name: "Select Callout block" }).click();
	await expect(
		bodyCards(page).first().getByRole("button", { name: "Select Callout block" })
	).toHaveAttribute("aria-pressed", "true");
	const clipboard = await bodyEditor(page).evaluate(async (element) => {
		const clipboardData = new DataTransfer();
		element.dispatchEvent(
			new ClipboardEvent("copy", { clipboardData, bubbles: true, cancelable: true })
		);
		await Promise.resolve();
		return clipboardData.getData("application/x-lexical-editor");
	});
	expect(clipboard).toContain('"blockType":"callout"');
	await bodyEditor(page).focus();
	await bodyEditor(page).press("ControlOrMeta+End");
	await bodyEditor(page).press("Enter");
	await bodyEditor(page).evaluate((element, serialized) => {
		const clipboardData = new DataTransfer();
		clipboardData.setData("application/x-lexical-editor", serialized);
		element.dispatchEvent(
			new ClipboardEvent("paste", { clipboardData, bubbles: true, cancelable: true })
		);
	}, clipboard);
	await expect(bodyCards(page)).toHaveCount(2);
	const saved = await saveArticle(page);
	const nodes = saved.body.root.children.filter((node: { type: string }) => node.type === "block");
	expect(nodes[0].fields._key).not.toBe(nodes[1].fields._key);
	expect(nodes[0].fields.links[0]._key).not.toBe(nodes[1].fields.links[0]._key);
	await bodyCards(page).last().getByRole("button", { name: "Select Callout block" }).click();
	const cutKey = await bodyCards(page).last().getAttribute("data-block-key");
	const cutClipboard = await bodyEditor(page).evaluate((element) => {
		const clipboardData = new DataTransfer();
		element.dispatchEvent(
			new ClipboardEvent("cut", { clipboardData, bubbles: true, cancelable: true })
		);
		return clipboardData.getData("application/x-lexical-editor");
	});
	expect(cutClipboard).toContain(cutKey!);
	await expect(bodyCards(page)).toHaveCount(1);
	await bodyEditor(page).focus();
	await page.keyboard.press("ControlOrMeta+z");
	await expect(bodyCards(page)).toHaveCount(2);
	await expect(bodyCards(page).last()).toHaveAttribute("data-block-key", cutKey!);
	await page.keyboard.press("ControlOrMeta+Shift+z");
	await expect(bodyCards(page)).toHaveCount(1);
	await page.keyboard.press("ControlOrMeta+z");
	await expect(bodyCards(page)).toHaveCount(2);
	await expect(bodyCards(page).last()).toHaveAttribute("data-block-key", cutKey!);
	await bodyEditor(page).evaluate((element) => {
		const clipboardData = new DataTransfer();
		clipboardData.setData(
			"application/x-lexical-editor",
			JSON.stringify({
				namespace: "historical",
				nodes: [
					{ type: "block", version: 1, fields: { blockType: "missing", _key: "historical" } },
				],
			})
		);
		element.dispatchEvent(
			new ClipboardEvent("paste", { clipboardData, bubbles: true, cancelable: true })
		);
	});
	await expect(
		page.getByRole("alert").filter({ hasText: "does not support a block type" })
	).toBeVisible();
	await expect(bodyCards(page)).toHaveCount(2);
});

test("schema removal and rename preserve stored content until explicit recovery", async ({
	page,
}) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Schema recovery",
			body: richDocument([block("callout", { title: "Retain this historical payload" })]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	const headers = { "X-Ridu-Test-Reset-Token": process.env.RIDU_BROWSER_RESET_TOKEN! };
	for (const change of [{ retireCallout: true }, { renameCallout: true }]) {
		const changed = await page.request.post("/__ridu-test/blocks-schema", {
			headers,
			data: change,
		});
		expect(changed.ok(), await changed.text()).toBe(true);
		const read = await page.request.get(`/api/collections/block-articles/${original.id}`);
		expect(read.ok()).toBe(false);
		expect(await read.text()).toContain("unknown_embedded_schema");
		await page.goto(`/admin/collections/block-articles/${original.id}`);
		await expect(
			page.getByText(/restore.*schema|embedded.*recovery|block.*no longer configured/i).first()
		).toBeVisible();
		const restored = await page.request.post("/__ridu-test/blocks-schema", { headers, data: {} });
		expect(restored.ok(), await restored.text()).toBe(true);
		await page.reload();
		await expect(bodyCards(page).first()).toContainText("Retain this historical payload");
		await expect(bodyCards(page).first()).toHaveAttribute(
			"data-block-key",
			original.body.root.children[0].fields._key
		);
	}
});

test("100 mixed cards keep nested editors lazy across initial load and reload", async ({
	page,
}) => {
	await loginAsEditor(page);
	const children = Array.from({ length: 100 }, (_, index) =>
		index < 20
			? block("callout", {
					title: `Callout ${index}`,
					detail: richDocument([
						{
							type: "paragraph",
							version: 1,
							children: [{ type: "text", version: 1, text: "A nested rich-text body" }],
						},
					]),
				})
			: block("cta", { label: `CTA ${index}` })
	);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: { title: "Lazy block fixture", body: richDocument(children) },
	});
	expect(created.ok(), await created.text()).toBe(true);
	const document = (await created.json()).doc;
	await page.goto(`/admin/collections/block-articles/${document.id}`);
	await expect(bodyCards(page)).toHaveCount(100);
	await expect(page.locator('.ridu-richtext-content[contenteditable="true"]')).toHaveCount(2);
	await page.reload();
	await expect(bodyCards(page)).toHaveCount(100);
	await expect(page.locator('.ridu-richtext-content[contenteditable="true"]')).toHaveCount(2);
});

test("all block variants use ordinary references/uploads and survive publish and restore", async ({
	page,
}) => {
	test.setTimeout(60_000);
	await loginAsEditor(page);
	await page.goto("/admin/collections/block-articles/create");
	await page.getByRole("textbox", { name: "Title", exact: true }).fill("Release article embeds");
	let drawer = await insertBlock(page, bodyEditor(page), "CTA");
	await drawer.getByRole("textbox", { name: /^CTA label/ }).fill("Explore Ridu");
	await drawer.getByRole("button", { name: "Destination", exact: true }).click();
	await page.getByRole("option", { name: /About Ridu/ }).click();
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	drawer = await insertBlock(page, bodyEditor(page), "Media");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await expect(drawer.getByRole("button", { name: "Block asset", exact: true })).toBeFocused();
	await drawer.getByRole("button", { name: "Block asset", exact: true }).click();
	await page.getByRole("option", { name: "Warm orange Ridu cover", exact: true }).click();
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	drawer = await insertBlock(page, bodyEditor(page), "Callout");
	await drawer.getByRole("textbox", { name: "Callout title", exact: true }).fill("Release callout");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	const saved = await saveArticle(page);
	expect(saved._status).toBe("draft");
	const savedBlocks = saved.body.root.children.filter(
		(node: { type: string }) => node.type === "block"
	);
	expect(
		savedBlocks.map((node: { fields: { blockType: string } }) => node.fields.blockType)
	).toEqual(["cta", "media", "callout"]);
	expect(savedBlocks[0].fields.destination).toBe("pages_10");
	expect(typeof savedBlocks[1].fields.asset).toBe("string");
	const publishResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/block-articles/${saved.id}/publish`
	);
	await page.getByRole("button", { name: "Publish", exact: true }).click();
	expect((await publishResponse).ok()).toBe(true);
	await expect(page.getByRole("button", { name: "Publish changes", exact: true })).toBeVisible();
	await bodyCards(page).last().getByRole("button", { name: "Edit", exact: true }).click();
	drawer = page.getByRole("dialog", { name: "Edit Callout", exact: true });
	await drawer.getByRole("textbox", { name: "Callout title", exact: true }).fill("Later revision");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	const publishedAgain = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/block-articles/${saved.id}/publish`
	);
	await page.getByRole("button", { name: "Publish changes", exact: true }).click();
	expect((await publishedAgain).ok()).toBe(true);
	await page.goto(`/admin/collections/block-articles/${saved.id}/versions/${saved._revision}`);
	await page.getByRole("button", { name: "Restore as draft", exact: true }).click();
	await expect(bodyCards(page).last()).toContainText("Release callout");
	const restored = (
		await (await page.request.get(`/api/collections/block-articles/${saved.id}`)).json()
	).doc;
	expect(restored._status).toBe("draft");
	expect(restored.body).toEqual(saved.body);
});

test("collapsed block fields mount inner editors only on demand and retain their draft", async ({
	page,
}) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/block-articles/create");
	await page.getByRole("textbox", { name: "Title", exact: true }).fill("Lazy advanced editor");
	const drawer = await insertBlock(page, bodyEditor(page), "Callout");
	await drawer
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Advanced callout");
	await expect(drawer.getByRole("textbox", { name: "Advanced detail", exact: true })).toHaveCount(
		0
	);
	const section = drawer.locator("summary").filter({ hasText: /Advanced content/i });
	await section.click();
	const advanced = drawer.getByRole("textbox", { name: "Advanced detail", exact: true });
	await expect(advanced).toBeVisible();
	await advanced.fill("Only mounted when opened");
	await section.click();
	await expect(advanced).toHaveCount(0);
	await section.click();
	await expect(advanced).toContainText("Only mounted when opened");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await bodyCards(page).first().getByRole("button", { name: "Edit", exact: true }).click();
	const reopened = page.getByRole("dialog", { name: "Edit Callout", exact: true });
	await expect(reopened.getByRole("textbox", { name: "Advanced detail", exact: true })).toHaveCount(
		0
	);
	await reopened
		.locator("summary")
		.filter({ hasText: /Advanced content/i })
		.click();
	await expect(
		reopened.getByRole("textbox", { name: "Advanced detail", exact: true })
	).toContainText("Only mounted when opened");
	await reopened.getByRole("button", { name: "Cancel", exact: true }).click();
	await saveArticle(page);
});

test("drawer drafts block outer submission, preview only applied edits, and preserve conflicts", async ({
	page,
}) => {
	test.setTimeout(60_000);
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Preview block article",
			body: richDocument([block("callout", { title: "Saved callout" })]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/block-articles/${original.id}`);
	await page.getByRole("button", { name: "Live preview", exact: true }).click();
	const preview = page.frameLocator('iframe[title="Live preview"]');
	await expect(preview.locator("[data-preview-blocks]")).toContainText("Saved callout");
	await bodyCards(page).first().getByRole("button", { name: "Edit", exact: true }).click();
	let drawer = page.getByRole("dialog", { name: "Edit Callout", exact: true });
	await drawer
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Unapplied drawer title");
	let writes = 0;
	page.on("request", (request) => {
		if (
			request.method() === "PATCH" &&
			new URL(request.url()).pathname === `/api/collections/block-articles/${original.id}`
		)
			writes++;
	});
	await page
		.locator("form")
		.filter({ has: page.locator('input[name="title"]') })
		.evaluate((form) => (form as HTMLFormElement).requestSubmit());
	await expect(
		page.getByText("Apply or cancel the open block editor before saving this document.").first()
	).toBeAttached();
	await expect(drawer).toBeVisible();
	expect(writes).toBe(0);
	await expect(preview.locator("[data-preview-blocks]")).toContainText("Saved callout");
	await drawer
		.getByRole("textbox", { name: "Callout title", exact: true })
		.press("Alt+Shift+ArrowDown");
	await expect(bodyCards(page).first()).toHaveAttribute(
		"data-block-key",
		original.body.root.children[0].fields._key
	);
	await drawer.getByRole("button", { name: "Cancel", exact: true }).click();
	await bodyCards(page).first().getByRole("button", { name: "Edit", exact: true }).click();
	drawer = page.getByRole("dialog", { name: "Edit Callout", exact: true });
	await drawer
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Applied preview title");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await expect(preview.locator("[data-preview-blocks]")).toContainText("Applied preview title");
	const remote = await page.request.patch(`/api/collections/block-articles/${original.id}`, {
		headers: { "If-Match": `"${original._revision}"` },
		data: { caption: "Remote sibling edit" },
	});
	expect(remote.ok(), await remote.text()).toBe(true);
	const failedSave = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/block-articles/${original.id}`
	);
	await documentSaveButton(page).click();
	expect((await failedSave).status()).toBe(409);
	await expect(bodyCards(page).first()).toContainText("Applied preview title");
	const stored = (
		await (await page.request.get(`/api/collections/block-articles/${original.id}`)).json()
	).doc;
	expect(stored.caption).toBe("Remote sibling edit");
	expect(stored.body.root.children[0].fields.title).toBe("Saved callout");
});

test("toolbar schema picker cancels without inserting a placeholder and inserts at its target", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Toolbar article",
			body: richDocument([
				{
					type: "paragraph",
					version: 1,
					children: [{ type: "text", version: 1, text: "Insertion anchor" }],
				},
			]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/block-articles/${original.id}`);
	const editor = bodyEditor(page);
	await editor.locator("p").first().hover();
	const toolbar = page
		.locator('[data-field-path="body"]')
		.first()
		.getByRole("toolbar", { name: "Block actions" });
	await toolbar.getByRole("button", { name: "Add block", exact: true }).click();
	let picker = page.getByRole("dialog", { name: "Insert block", exact: true });
	await picker.getByRole("combobox", { name: "Filter blocks" }).fill("Callout");
	await picker.getByRole("option", { name: /^Callout Structured block/ }).click();
	let drawer = page.getByRole("dialog", { name: "Insert Callout", exact: true });
	await drawer.getByRole("button", { name: "Cancel", exact: true }).click();
	await expect(editor.locator(":scope > *")).toHaveCount(1);
	await expect(bodyCards(page)).toHaveCount(0);
	await editor.locator("p").first().hover();
	await toolbar.getByRole("button", { name: "Add block", exact: true }).click();
	picker = page.getByRole("dialog", { name: "Insert block", exact: true });
	await picker.getByRole("combobox", { name: "Filter blocks" }).fill("Callout");
	await picker.getByRole("option", { name: /^Callout Structured block/ }).click();
	drawer = page.getByRole("dialog", { name: "Insert Callout", exact: true });
	await drawer
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Inserted after anchor");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await expect(bodyCards(page)).toHaveCount(1);
	const saved = await saveArticle(page);
	expect(saved.body.root.children.map((node: { type: string }) => node.type)).toEqual([
		"paragraph",
		"block",
	]);
	expect(errors.pageErrors).toEqual([]);
});

test("a historical variant remains exportable when the loaded admin schema loses its definition", async ({
	page,
}) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Export historical block",
			body: richDocument([
				block("callout", { title: "Do not lose this", caption: "Historical payload" }),
			]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.route("**/api/schema", async (route) => {
		const response = await route.fetch();
		const envelope = (await response.json()) as {
			schema: { collections: { slug: string; fields: SchemaField[] }[] };
		};
		const body = envelope.schema.collections
			.find((collection) => collection.slug === "block-articles")
			?.fields.find((field) => field.name === "body");
		const branch = body?.plugin?.embeddedTrees
			?.find((tree) => tree.key === "blocks")
			?.cases.find((branch) => branch.tagValue === "block");
		if (branch === undefined) throw new Error("Missing test embedded schema");
		branch.types = branch.types!.filter((type) => type.slug !== "callout");
		await route.fulfill({ response, json: envelope });
	});
	await page.goto(`/admin/collections/block-articles/${original.id}`);
	const card = bodyCards(page).first();
	await expect(card.getByRole("button", { name: "Edit", exact: true })).toBeDisabled();
	const downloadEvent = page.waitForEvent("download");
	await card.getByRole("button", { name: "Export block JSON", exact: true }).click();
	const download = await downloadEvent;
	const path = await download.path();
	if (path === null) throw new Error("Expected historical block JSON download");
	expect(JSON.parse(await readFile(path, "utf8"))).toEqual(original.body.root.children[0]);
	await page.getByRole("textbox", { name: "Caption", exact: true }).fill("Sibling edit");
	await documentSaveButton(page).click();
	await expect(page.getByText(/embedded.*schema|embedded.*recovery/i).first()).toBeVisible();
	const stored = (
		await (await page.request.get(`/api/collections/block-articles/${original.id}`)).json()
	).doc;
	expect(stored.body).toEqual(original.body);
});

test("draft validation reveals an invalid child through multiple lazy disclosures", async ({
	page,
}) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Nested error reveal",
			body: richDocument([
				block("callout", { title: "Links", links: [{ label: "Original", href: "/about" }] }),
			]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.route("**/api/schema", async (route) => {
		const response = await route.fetch();
		const envelope = (await response.json()) as {
			schema: { collections: { slug: string; fields: SchemaField[] }[] };
		};
		const links = envelope.schema.collections
			.find((c) => c.slug === "block-articles")
			?.fields.find((f) => f.name === "body")
			?.plugin?.embeddedTrees?.find((t) => t.key === "blocks")
			?.cases.find((c) => c.tagValue === "block")
			?.types?.find((t) => t.slug === "callout")
			?.fields.find((f) => f.name === "links");
		const label = links?.nested?.fields.find((f) => f.name === "label");
		if (links === undefined || label === undefined) throw new Error("Missing links fixture");
		links.admin.collapsible = {
			id: "outer-links",
			label: "Link details",
			initiallyCollapsed: true,
		};
		label.admin.collapsible = {
			id: "inner-label",
			label: "Link label details",
			initiallyCollapsed: true,
		};
		await route.fulfill({ response, json: envelope });
	});
	await page.goto(`/admin/collections/block-articles/${original.id}`);
	await bodyCards(page).first().getByRole("button", { name: "Edit", exact: true }).click();
	const drawer = page.getByRole("dialog", { name: "Edit Callout", exact: true });
	const outer = drawer.locator('[data-field-collapsible^="outer-links"]');
	const inner = drawer.locator('[data-field-collapsible^="inner-label"]');
	await expect(outer).not.toHaveAttribute("open");
	await outer.locator(":scope > summary").click();
	await inner.locator(":scope > summary").click();
	const label = drawer.getByRole("textbox", { name: "Label", exact: true });
	await label.fill("");
	await inner.locator(":scope > summary").click();
	await outer.locator(":scope > summary").click();
	await expect(label).toHaveCount(0);
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await expect(outer).toHaveAttribute("open");
	await expect(inner).toHaveAttribute("open");
	await expect(label).toBeFocused();
	await label.fill("Corrected");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	const stored = await saveArticle(page);
	expect(stored.body.root.children[0].fields.links[0].label).toBe("Corrected");
});

test("unsupported historical document properties remain exportable without a lossy editor import", async ({
	page,
}) => {
	const errors = observePageErrors(page);
	await loginAsEditor(page);
	errors.consoleErrors.length = 0;
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Historical envelope",
			body: richDocument([
				{ type: "paragraph", children: [{ type: "text", text: "Keep this text" }] },
			]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	const historical = structuredClone(original.body);
	historical.root.children[0].children[0].extension = { important: "Do not lose this" };
	await page.route(`**/api/collections/block-articles/${original.id}*`, async (route) => {
		if (route.request().method() !== "GET") {
			await route.continue();
			return;
		}
		const response = await route.fetch();
		const envelope = await response.json();
		envelope.doc.body = historical;
		await route.fulfill({ response, json: envelope });
	});
	await page.goto(`/admin/collections/block-articles/${original.id}`);
	const field = page.locator('[data-field-path="body"]').first();
	await expect(field.getByRole("alert")).toContainText("body.root.children.0.children.0.extension");
	await expect(bodyEditor(page)).toHaveCount(0);
	const downloading = page.waitForEvent("download");
	await field.getByRole("button", { name: "Export document JSON", exact: true }).click();
	const download = await downloading;
	const path = await download.path();
	if (path === null) throw new Error("Missing historical export");
	expect(JSON.parse(await readFile(path, "utf8"))).toEqual(historical);
	await page.getByRole("textbox", { name: "Caption", exact: true }).fill("Sibling edit");
	const submitted = page.waitForRequest(
		(request) => request.method() === "PATCH" && request.url().includes(original.id)
	);
	const rejected = page.waitForResponse(
		(response) => response.request().method() === "PATCH" && response.url().includes(original.id)
	);
	await documentSaveButton(page).click();
	expect((await submitted).postDataJSON().body).toEqual(historical);
	expect((await rejected).status()).toBe(422);
	expect(errors.pageErrors).toEqual([]);
});

test("release page composes Hero Content Media and CTA with rich-text embeds and localized revisions", async ({
	page,
}) => {
	test.setTimeout(90_000);
	const errors = observePageErrors(page);
	await loginAsEditor(page);
	errors.consoleErrors.length = 0;
	await page.goto("/admin/collections/block-pages/create");
	await page.getByRole("textbox", { name: "Title", exact: true }).fill("Release layout page");
	const layout = page.locator('[data-field-path="layout"]').first();
	for (const type of ["Hero", "Content", "Media", "CTA"]) {
		await layout
			.getByRole("button", { name: "Add block", exact: true })
			.filter({ hasText: "Add block" })
			.click();
		await page.getByRole("dialog").getByRole("button", { name: type, exact: true }).click();
	}
	await page.locator('input[name="layout.0.heading"]').fill("Hero for the release");
	await page.locator('input[name="layout.1.title"]').fill("Structured content");
	const editor = page.getByRole("textbox", { name: "Body", exact: true });
	await editor.fill("Portable page introduction");
	let drawer = await insertBlock(page, editor, "Callout");
	await drawer.getByRole("textbox", { name: "Callout title", exact: true }).fill("Layout callout");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	drawer = await insertBlock(page, editor, "CTA");
	await drawer.getByRole("textbox", { name: /^CTA label/ }).fill("Embedded action");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await layout
		.locator('[data-field-path="layout.2.asset"]')
		.getByRole("button", { name: "Block asset", exact: true })
		.click();
	await page.getByRole("option", { name: "Warm orange Ridu cover", exact: true }).click();
	await page.locator('input[name="layout.2.caption"]').fill("Release media");
	await page.locator('input[name="layout.3.label"]').fill("Explore the page");
	await layout
		.locator('[data-field-path="layout.3.destination"]')
		.getByRole("button", { name: "Destination", exact: true })
		.click();
	await page.getByRole("option", { name: /About Ridu/ }).click();
	const saved = await saveCollection(page, "block-pages");
	expect(saved.layout.map((row: { blockType: string }) => row.blockType)).toEqual([
		"hero",
		"content",
		"media",
		"cta",
	]);
	expect(saved.layout[3].destination).toBe("pages_10");
	expect(typeof saved.layout[2].asset).toBe("string");
	const keys = saved.layout.map((row: { _key: string }) => row._key);
	const embedded = saved.layout[1].body.root.children.filter(
		(node: { type: string }) => node.type === "block"
	);
	expect(embedded.map((node: { fields: { blockType: string } }) => node.fields.blockType)).toEqual([
		"callout",
		"cta",
	]);
	await page.reload();
	await expect(editor).toContainText("Layout callout");
	await chooseContentLocale(page, "French", "fr");
	await page.locator('input[name="layout.0.heading"]').fill("En-tête français");
	await page.locator('input[name="layout.3.label"]').fill("Explorer la page");
	await saveCollection(page, "block-pages");
	const all = (
		await (await page.request.get(`/api/collections/block-pages/${saved.id}?locale=all`)).json()
	).doc;
	expect(all.layout.map((row: { _key: string }) => row._key)).toEqual(keys);
	expect(all.layout[0].heading).toEqual({ en: "Hero for the release", fr: "En-tête français" });
	expect(all.layout[3].label).toEqual({ en: "Explore the page", fr: "Explorer la page" });
	expect(
		all.layout[1].body.root.children
			.filter((node: { type: string }) => node.type === "block")
			.map((node: { fields: { _key: string } }) => node.fields._key)
	).toEqual(embedded.map((node: { fields: { _key: string } }) => node.fields._key));
	await chooseContentLocale(page, "English", "en");
	const publishing = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/block-pages/${saved.id}/publish`
	);
	await page.getByRole("button", { name: "Publish", exact: true }).click();
	expect((await publishing).ok()).toBe(true);
	await page.goto(`/admin/collections/block-pages/${saved.id}/versions/${saved._revision}`);
	await page.getByRole("button", { name: "Restore as draft", exact: true }).click();
	await expect(editor).toContainText("Layout callout");
	const restored = (
		await (await page.request.get(`/api/collections/block-pages/${saved.id}?locale=en`)).json()
	).doc;
	expect(restored.layout).toEqual(saved.layout);
	expect(restored._status).toBe("draft");
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("server-normalized save replaces the mounted editor before subsequent edits", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Normalized document",
			body: richDocument([
				block("callout", { title: "Before normalization" }),
				{ type: "paragraph", children: [{ type: "text", text: "Prose" }] },
			]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	const key = original.body.root.children[0].fields._key;
	await page.goto(`/admin/collections/block-articles/${original.id}`);
	let normalized = false;
	await page.route(`**/api/collections/block-articles/${original.id}*`, async (route) => {
		if (route.request().method() !== "PATCH" || normalized) {
			await route.continue();
			return;
		}
		// A server-side field hook may normalize a payload on save. Change the
		// submitted fixture value so the real persisted response carries that result.
		const value = route.request().postDataJSON();
		value.body.root.children.find((node: { type: string }) => node.type === "block").fields.title =
			"Server normalization";
		normalized = true;
		await route.continue({ postData: JSON.stringify(value) });
	});
	await page.getByRole("textbox", { name: "Title", exact: true }).fill("Trigger normalization");
	await saveArticle(page);
	await expect(bodyCards(page).first()).toContainText("Server normalization");
	await expect(bodyCards(page).first()).toHaveAttribute("data-block-key", key);
	await expect(bodyEditor(page)).toHaveAttribute("contenteditable", "true");
	await bodyEditor(page).locator("p").last().click();
	await bodyEditor(page).press("ControlOrMeta+End");
	await page.keyboard.type(" edited after save");
	await expect(bodyEditor(page)).toContainText("edited after save");
	const saved = await saveArticle(page);
	expect(
		saved.body.root.children.find((node: { type: string }) => node.type === "block").fields
	).toMatchObject({ _key: key, title: "Server normalization" });
	await page.reload();
	await expect(bodyCards(page).first()).toContainText("Server normalization");
	await expect(bodyEditor(page)).toContainText("edited after save");
	expect(errors.pageErrors).toEqual([]);
});
