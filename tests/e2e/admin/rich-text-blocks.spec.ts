import { readFile } from "node:fs/promises";
import type { SchemaField } from "@riducms/protocol";
import type { Page } from "@playwright/test";
import { insertBlock, bodyEditor, bodyCards, richDocument, block } from "./rich-text-block-fixture";
import { expect, test } from "./fixture";
import {
	chooseContentLocale,
	submitDocumentForm,
	loginAsEditor,
	observePageErrors,
	useAdminRuntimeFallback,
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
	await submitDocumentForm(page);
	const response = await saved;
	expect(response.ok(), await response.text()).toBe(true);
	const document = (await response.json()).doc;
	await expect(page).toHaveURL(
		(url) => url.pathname === `/admin/collections/${collection}/${document.id}`
	);
	return document;
}
const saveArticle = (page: Page) => saveCollection(page, "block-articles");

test("expanded inline block drag stays in its editor and can be undone", async ({ page }) => {
	await page.setViewportSize({ width: 1440, height: 2200 });
	await loginAsEditor(page);
	const paragraph = (text: string) => ({
		type: "paragraph",
		version: 1,
		children: [{ type: "text", version: 1, text }],
	});
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Drag scoped callout",
			body: richDocument([
				paragraph("Before card"),
				block("callout", { title: "Movable callout" }),
				paragraph("After card"),
			]),
			localizedBody: richDocument([paragraph("Other editor")]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const article = (await created.json()).doc;
	await page.goto(`/admin/collections/block-articles/${article.id}`);

	const editor = bodyEditor(page);
	const canvas = editor.locator("..");
	const card = bodyCards(page).first();
	const blocks = editor.locator(":scope > :not([data-lexical-cursor])");
	const dropLine = canvas.locator(":scope > .ridu-richtext-drop-line");
	const grip = canvas.locator(":scope > .ridu-richtext-block-toolbar").getByRole("button", {
		name: "Drag to move block",
	});
	await card.locator(".ridu-richtext-block__header").hover();
	await grip.hover();
	const start = await grip.boundingBox();
	const last = await editor.getByText("After card").boundingBox();
	expect(start).not.toBeNull();
	expect(last).not.toBeNull();
	await page.mouse.move(start!.x + start!.width / 2, start!.y + start!.height / 2);
	await page.mouse.down();
	await page.mouse.move(start!.x + start!.width / 2 + 8, start!.y + start!.height / 2 + 2, {
		steps: 4,
	});
	await page.mouse.move(last!.x + last!.width / 2, last!.y + last!.height * 0.8, {
		steps: 16,
	});
	await page.mouse.move(last!.x + last!.width / 2 + 2, last!.y + last!.height * 0.8, {
		steps: 4,
	});
	await expect(dropLine).toHaveCSS("opacity", "0.8");
	await page.mouse.up();
	await expect(blocks.last().getByRole("textbox", { name: "Callout title" })).toHaveValue(
		"Movable callout"
	);
	await expect(editor).toBeFocused();

	const otherEditor = page.locator('[data-field-path="localizedBody"] .ridu-richtext-content');
	await expect(otherEditor).toContainText("Other editor");
	await card.locator(".ridu-richtext-block__header").hover();
	await grip.hover();
	const secondStart = await grip.boundingBox();
	const other = await otherEditor.boundingBox();
	expect(secondStart).not.toBeNull();
	expect(other).not.toBeNull();
	await page.mouse.move(
		secondStart!.x + secondStart!.width / 2,
		secondStart!.y + secondStart!.height / 2
	);
	await page.mouse.down();
	await page.mouse.move(
		secondStart!.x + secondStart!.width / 2 + 8,
		secondStart!.y + secondStart!.height / 2 + 2,
		{
			steps: 4,
		}
	);
	await page.mouse.move(other!.x + other!.width / 2, other!.y + other!.height / 2, {
		steps: 16,
	});
	await expect(dropLine).toHaveCSS("opacity", "0");
	await page.mouse.up();
	await expect(blocks.last().getByRole("textbox", { name: "Callout title" })).toHaveValue(
		"Movable callout"
	);
	await expect(otherEditor.locator(":scope > :not([data-lexical-cursor])")).toHaveCount(1);
	await expect(otherEditor.locator(":scope > p")).toHaveText("Other editor");
	await expect(otherEditor.locator(":scope > .ridu-richtext-embedded")).toHaveCount(0);

	await editor.focus();
	await editor.press("ControlOrMeta+z");
	await expect(blocks.nth(1).getByRole("textbox", { name: "Callout title" })).toHaveValue(
		"Movable callout"
	);
});

test("schema blocks edit inline, validate, duplicate, undo, remove, and reload", async ({
	page,
}) => {
	test.setTimeout(90_000);
	const errors = observePageErrors(page);
	await loginAsEditor(page);
	errors.consoleErrors.length = 0;
	await page.goto("/admin/collections/block-articles/create");
	await page.getByRole("textbox", { name: "Title", exact: true }).fill("Structured authoring");
	const editor = bodyEditor(page);
	const callout = await insertBlock(page, editor, "Callout");
	await expect(callout.getByRole("button", { name: "Collapse Callout" })).toHaveAttribute(
		"aria-expanded",
		"true"
	);
	await expect(callout.getByRole("textbox", { name: "Callout title", exact: true })).toBeVisible();
	await expect(callout.getByRole("textbox", { name: "Caption", exact: true })).toHaveValue(
		"Helpful context"
	);
	await submitDocumentForm(page);
	await expect(callout.getByRole("textbox", { name: "Callout title", exact: true })).toBeFocused();
	await expect(callout).toHaveAttribute("data-invalid", "true");
	await callout.getByRole("textbox", { name: "Callout title", exact: true }).fill("First callout");
	await expect(bodyCards(page)).toHaveCount(1);
	const originalKey = await bodyCards(page).first().getAttribute("data-block-key");
	expect(originalKey).toBeTruthy();

	await callout.getByRole("textbox", { name: "Caption", exact: true }).focus();
	await callout.getByRole("textbox", { name: "Callout title", exact: true }).fill("Edited inline");
	await expect(callout.getByRole("textbox", { name: "Callout title", exact: true })).toHaveValue(
		"Edited inline"
	);
	await editor.focus();
	await page.keyboard.press("ControlOrMeta+z");
	await expect(callout.getByRole("textbox", { name: "Callout title", exact: true })).toHaveValue(
		"First callout"
	);
	await expect(bodyCards(page).first()).toHaveAttribute("data-block-key", originalKey!);
	await page.keyboard.press("ControlOrMeta+Shift+z");
	await expect(callout.getByRole("textbox", { name: "Callout title", exact: true })).toHaveValue(
		"Edited inline"
	);
	await page.keyboard.press("ControlOrMeta+z");
	await expect(callout.getByRole("textbox", { name: "Callout title", exact: true })).toHaveValue(
		"First callout"
	);
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
	await expect(bodyCards(page).first().getByRole("textbox", { name: "Callout title" })).toHaveValue(
		"First callout"
	);
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("inline text fields and nested rich text retain focus through sequential typing", async ({
	page,
}) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/block-articles/create");
	await page.getByRole("textbox", { name: "Title", exact: true }).fill("Sequential typing");
	const callout = await insertBlock(page, bodyEditor(page), "Callout");
	const title = callout.getByRole("textbox", { name: "Callout title", exact: true });
	await title.pressSequentially("Typed callout", { delay: 10 });
	await expect(title).toHaveValue("Typed callout");
	await expect(title).toBeFocused();
	const caption = callout.getByRole("textbox", { name: "Caption", exact: true });
	await caption.fill("");
	await caption.pressSequentially("Typed caption", { delay: 10 });
	await expect(caption).toHaveValue("Typed caption");
	await expect(caption).toBeFocused();
	const addRow = callout.getByRole("button", { name: "Add row", exact: true });
	await addRow.focus();
	await addRow.press("ControlOrMeta+z");
	await expect(caption).not.toHaveValue("Typed caption");
	await addRow.press("ControlOrMeta+Shift+z");
	await expect(caption).toHaveValue("Typed caption");
	const detail = callout.getByRole("textbox", { name: "Detail", exact: true });
	await detail.click();
	await detail.pressSequentially("Nested body text", { delay: 10 });
	await expect(detail).toContainText("Nested body text");
	await expect(detail).toBeFocused();
	const saved = await saveArticle(page);
	const node = saved.body.root.children.find((entry: { type: string }) => entry.type === "block");
	expect(node.fields).toMatchObject({
		blockType: "callout",
		title: "Typed callout",
		caption: "Typed caption",
	});
	expect(node.fields.detail.root.children[0].children[0].text).toBe("Nested body text");
});

test("read-only users can inspect inline block fields without edit actions", async ({
	browser,
	page,
}) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Locked inline block",
			body: richDocument([block("callout", { title: "Visible read-only value" })]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;

	const viewer = await browser.newContext();
	try {
		const view = await viewer.newPage();
		const auth = await view.request.post("/api/auth/users/login", {
			data: { email: "demo@riducms.local", password: "ridu-demo" },
		});
		expect(auth.ok(), await auth.text()).toBe(true);
		await view.goto(`/admin/collections/block-articles/${original.id}`);
		await expect(
			view.getByRole("button", { name: "Open account menu for Demo Author" })
		).toBeVisible();
		await expect(bodyEditor(view)).toHaveAttribute("contenteditable", "false");
		const callout = bodyCards(view).first();
		await expect(callout.getByRole("textbox", { name: "Callout title" })).toHaveValue(
			"Visible read-only value"
		);
		await expect(callout.getByRole("textbox", { name: "Callout title" })).not.toBeEditable();
		await expect(callout.getByRole("button", { name: "Duplicate" })).toHaveCount(0);
		await expect(callout.getByRole("button", { name: "Remove" })).toHaveCount(0);
		await callout.getByRole("button", { name: "Collapse Callout" }).click();
		await expect(callout.getByRole("button", { name: "Expand Callout" })).toBeVisible();
	} finally {
		await viewer.close();
	}
});

test("nested inline editors keep independent histories and save with the outer block", async ({
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
	await expect(detail.locator('[data-block-type="cta"]')).toHaveCount(1);
	await extra.focus();
	await page.keyboard.press("ControlOrMeta+z");
	await expect(detail.locator('[data-block-type="cta"]')).toHaveCount(1);
	await extra.fill("Independent extra text");
	await expect(outer.getByRole("textbox", { name: "Detail", exact: true })).toContainText(
		"Read more"
	);
	await expect(outer.getByRole("textbox", { name: "Extra detail", exact: true })).toContainText(
		"Independent extra text"
	);
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
	const shared = bodyCards(page).first();
	await expect(shared.getByRole("textbox", { name: /^CTA label/ })).toHaveValue(
		"English shared CTA"
	);
	await shared.getByRole("textbox", { name: /^CTA label/ }).fill("CTA partagé français");
	const localizedCards = page.locator(
		'[data-field-path="localizedBody"] article.ridu-richtext-block'
	);
	await localizedCards
		.first()
		.getByRole("textbox", { name: /^CTA label/ })
		.fill("Document français");
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
	await expect(shared.getByRole("textbox", { name: /^CTA label/ })).toHaveValue(
		"English shared CTA"
	);
	await expect(localizedCards.first().getByRole("textbox", { name: /^CTA label/ })).toHaveValue(
		"English document CTA"
	);
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
	await failing.getByRole("textbox", { name: "Callout title", exact: true }).fill("invalid");
	await failing.getByRole("button", { name: "Collapse Callout" }).click();
	await expect(failing.getByRole("textbox", { name: "Callout title", exact: true })).toHaveCount(0);
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
	await submitDocumentForm(page);
	await responseReady;
	await expect(bodyEditor(page)).toHaveAttribute("contenteditable", "false");
	release();
	await expect(failing).toHaveAttribute("data-invalid", "true");
	await expect(failing.getByRole("button", { name: "Collapse Callout" })).toHaveAttribute(
		"aria-expanded",
		"true"
	);
	await expect(failing.getByRole("textbox", { name: "Callout title", exact: true })).toBeFocused();
	await failing.getByRole("button", { name: "Select Callout block" }).press("Alt+Shift+ArrowDown");
	await expect(bodyCards(page).last()).toHaveAttribute("data-block-key", key);
	await expect(failing).toHaveAttribute("data-invalid", "true");
	await expect(bodyCards(page).first()).toHaveAttribute("data-invalid", "false");
	await expect(failing.getByText("Use a descriptive callout title", { exact: true })).toBeVisible();
	await failing
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Corrected title");
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
		await expect(
			bodyCards(page).first().getByRole("textbox", { name: "Callout title" })
		).toHaveValue("Retain this historical payload");
		await expect(bodyCards(page).first()).toHaveAttribute(
			"data-block-key",
			original.body.root.children[0].fields._key
		);
	}
});

test("100 mixed inline blocks expose their fields and preserve collapsed editor state", async ({
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
	const first = bodyCards(page).first();
	await expect(first.getByRole("textbox", { name: "Detail", exact: true })).toBeVisible();
	await expect(first.getByRole("textbox", { name: "Extra detail", exact: true })).toBeVisible();
	await expect(first.locator('.ridu-richtext-content[contenteditable="true"]')).toHaveCount(2);
	await first.getByRole("button", { name: "Collapse Callout" }).click();
	await expect(first.getByRole("textbox", { name: "Detail", exact: true })).toHaveCount(0);
	await expect(first.locator('.ridu-richtext-content[contenteditable="true"]')).toHaveCount(2);
	await first.getByRole("button", { name: "Expand Callout" }).click();
	await expect(first.getByRole("textbox", { name: "Detail", exact: true })).toBeVisible();
	await page.reload();
	await expect(bodyCards(page)).toHaveCount(100);
	await expect(
		bodyCards(page).first().getByRole("textbox", { name: "Detail", exact: true })
	).toBeVisible();
});

test("all block variants use ordinary references/uploads and survive publish and restore", async ({
	page,
}) => {
	test.setTimeout(60_000);
	await loginAsEditor(page);
	await page.goto("/admin/collections/block-articles/create");
	await page.getByRole("textbox", { name: "Title", exact: true }).fill("Release article embeds");
	let embedded = await insertBlock(page, bodyEditor(page), "CTA");
	await embedded.getByRole("textbox", { name: /^CTA label/ }).fill("Explore Ridu");
	await embedded.getByRole("combobox", { name: "Destination", exact: true }).click();
	await page.getByRole("option", { name: /About Ridu/ }).click();
	embedded = await insertBlock(page, bodyEditor(page), "Media");
	await embedded.getByRole("button", { name: "Choose from existing", exact: true }).click();
	await page
		.getByRole("dialog", { name: "Select block asset", exact: true })
		.getByRole("button", { name: "ridu-cover.png", exact: true })
		.click();
	embedded = await insertBlock(page, bodyEditor(page), "Callout");
	await embedded
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Release callout");
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
	await page.getByRole("button", { name: "Publish changes", exact: true }).click();
	expect((await publishResponse).ok()).toBe(true);
	await expect(page.getByRole("button", { name: "Publish changes", exact: true })).toBeVisible();
	await bodyCards(page)
		.last()
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Later revision");
	const publishedAgain = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/block-articles/${saved.id}/publish`
	);
	await page.getByRole("button", { name: "Publish changes", exact: true }).click();
	expect((await publishedAgain).ok()).toBe(true);
	await page.goto(`/admin/collections/block-articles/${saved.id}/versions/${saved._revision}`);
	await page.getByRole("button", { name: "Restore this version", exact: true }).click();
	await page.getByRole("button", { name: "Confirm", exact: true }).click();
	await expect(bodyCards(page).last().getByRole("textbox", { name: "Callout title" })).toHaveValue(
		"Release callout"
	);
	const restored = (
		await (await page.request.get(`/api/collections/block-articles/${saved.id}`)).json()
	).doc;
	expect(restored._status).toBe("draft");
	expect(restored.body).toEqual(saved.body);
});

test("nested disclosures in inline blocks mount editors on demand and retain their draft", async ({
	page,
}) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/block-articles/create");
	await page.getByRole("textbox", { name: "Title", exact: true }).fill("Lazy advanced editor");
	const callout = await insertBlock(page, bodyEditor(page), "Callout");
	await callout
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Advanced callout");
	await expect(callout.getByRole("textbox", { name: "Advanced detail", exact: true })).toHaveCount(
		0
	);
	const section = callout.locator("summary").filter({ hasText: /Advanced content/i });
	await section.click();
	const advanced = callout.getByRole("textbox", { name: "Advanced detail", exact: true });
	await expect(advanced).toBeVisible();
	await advanced.fill("Only mounted when opened");
	await section.click();
	await expect(advanced).toHaveCount(0);
	await section.click();
	await expect(advanced).toContainText("Only mounted when opened");
	await callout.getByRole("button", { name: "Collapse Callout" }).click();
	await expect(callout.getByRole("textbox", { name: "Advanced detail", exact: true })).toHaveCount(
		0
	);
	await callout.getByRole("button", { name: "Expand Callout" }).click();
	await expect(advanced).toBeVisible();
	await expect(
		callout.getByRole("textbox", { name: "Advanced detail", exact: true })
	).toContainText("Only mounted when opened");
	await saveArticle(page);
});

test("inline block edits update preview before save and preserve conflicts", async ({ page }) => {
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
	const callout = bodyCards(page).first();
	let writes = 0;
	page.on("request", (request) => {
		if (
			request.method() === "PATCH" &&
			new URL(request.url()).pathname === `/api/collections/block-articles/${original.id}`
		)
			writes++;
	});
	await callout
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Inline preview title");
	await expect(preview.locator("[data-preview-blocks]")).toContainText("Inline preview title");
	expect(writes).toBe(0);
	await expect(callout).toHaveAttribute(
		"data-block-key",
		original.body.root.children[0].fields._key
	);
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
	await submitDocumentForm(page);
	expect((await failedSave).status()).toBe(409);
	await expect(callout.getByRole("textbox", { name: "Callout title", exact: true })).toHaveValue(
		"Inline preview title"
	);
	const stored = (
		await (await page.request.get(`/api/collections/block-articles/${original.id}`)).json()
	).doc;
	expect(stored.caption).toBe("Remote sibling edit");
	expect(stored.body.root.children[0].fields.title).toBe("Saved callout");
});

test("toolbar schema picker inserts inline at its target and removes cleanly", async ({ page }) => {
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
	await picker.getByRole("option", { name: "Callout", exact: true }).click();
	await expect(bodyCards(page)).toHaveCount(1);
	await bodyCards(page).first().getByRole("button", { name: "Remove", exact: true }).click();
	await expect(bodyCards(page)).toHaveCount(0);
	await expect(editor.locator(":scope > p").first()).toHaveText("Insertion anchor");
	await editor.locator("p").first().hover();
	await toolbar.getByRole("button", { name: "Add block", exact: true }).click();
	picker = page.getByRole("dialog", { name: "Insert block", exact: true });
	await picker.getByRole("combobox", { name: "Filter blocks" }).fill("Callout");
	await picker.getByRole("option", { name: "Callout", exact: true }).click();
	const callout = bodyCards(page).first();
	await expect(callout).toBeVisible();
	await callout
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Inserted after anchor");
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
	await useAdminRuntimeFallback(page);
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
	await expect(card.getByRole("textbox", { name: "Callout title", exact: true })).toHaveCount(0);
	await expect(card.getByRole("alert")).toBeVisible();
	const downloadEvent = page.waitForEvent("download");
	await card.getByRole("button", { name: "Export block JSON", exact: true }).click();
	const download = await downloadEvent;
	const path = await download.path();
	if (path === null) throw new Error("Expected historical block JSON download");
	expect(JSON.parse(await readFile(path, "utf8"))).toEqual(original.body.root.children[0]);
	await page.getByRole("textbox", { name: "Caption", exact: true }).fill("Sibling edit");
	await submitDocumentForm(page);
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
	await useAdminRuntimeFallback(page);
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
	const callout = bodyCards(page).first();
	const outer = callout.locator('[data-field-collapsible^="outer-links"]');
	const inner = callout.locator('[data-field-collapsible^="inner-label"]');
	await expect(outer).not.toHaveAttribute("open");
	await outer.locator(":scope > summary").click();
	await inner.locator(":scope > summary").click();
	const label = callout.getByRole("textbox", { name: "Label", exact: true });
	await label.fill("");
	await inner.locator(":scope > summary").click();
	await outer.locator(":scope > summary").click();
	await expect(label).toHaveCount(0);
	await callout.getByRole("button", { name: "Collapse Callout" }).click();
	await submitDocumentForm(page);
	await expect(callout.getByRole("button", { name: "Collapse Callout" })).toHaveAttribute(
		"aria-expanded",
		"true"
	);
	await expect(outer).toHaveAttribute("open");
	await expect(inner).toHaveAttribute("open");
	await expect(label).toBeFocused();
	await label.fill("Corrected");
	const stored = await saveArticle(page);
	expect(stored.body.root.children[0].fields.links[0].label).toBe("Corrected");
});

test("unsupported historical document properties remain exportable without a lossy editor import", async ({
	page,
}) => {
	const errors = observePageErrors(page);
	await loginAsEditor(page);
	await useAdminRuntimeFallback(page);
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
	const saved = page.waitForResponse(
		(response) => response.request().method() === "PATCH" && response.url().includes(original.id)
	);
	await submitDocumentForm(page);
	const patch = (await submitted).postDataJSON();
	expect(patch).not.toHaveProperty("body");
	expect(patch.caption).toBe("Sibling edit");
	const response = await saved;
	expect(response.ok(), await response.text()).toBe(true);
	const stored = (await response.json()).doc;
	expect(stored.body).toEqual(original.body);
	expect(stored.caption).toBe("Sibling edit");
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
	let inlineBlock = await insertBlock(page, editor, "Callout");
	await inlineBlock
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Layout callout");
	inlineBlock = await insertBlock(page, editor, "CTA");
	await inlineBlock.getByRole("textbox", { name: /^CTA label/ }).fill("Embedded action");
	await layout
		.locator('[data-field-path="layout.2.asset"]')
		.getByRole("button", { name: "Choose from existing", exact: true })
		.click();
	await page
		.getByRole("dialog", { name: "Select block asset", exact: true })
		.getByRole("button", { name: "ridu-cover.png", exact: true })
		.click();
	await page.locator('input[name="layout.2.caption"]').fill("Release media");
	await page.locator('input[name="layout.3.label"]').fill("Explore the page");
	await layout
		.locator('[data-field-path="layout.3.destination"]')
		.getByRole("combobox", { name: "Destination", exact: true })
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
	await expect(
		editor.locator('article[data-block-type="callout"]').getByRole("textbox", {
			name: "Callout title",
		})
	).toHaveValue("Layout callout");
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
	await page.getByRole("button", { name: "Publish changes", exact: true }).click();
	expect((await publishing).ok()).toBe(true);
	await page.goto(`/admin/collections/block-pages/${saved.id}/versions/${saved._revision}`);
	await page.getByRole("button", { name: "Restore this version", exact: true }).click();
	await page.getByRole("button", { name: "Confirm", exact: true }).click();
	await expect(
		editor.locator('article[data-block-type="callout"]').getByRole("textbox", {
			name: "Callout title",
		})
	).toHaveValue("Layout callout");
	const restored = (
		await (await page.request.get(`/api/collections/block-pages/${saved.id}?locale=en`)).json()
	).doc;
	expect(restored.layout).toEqual(saved.layout);
	expect(restored._status).toBe("draft");
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("server-normalized save stays clean on navigation and replaces the editor before later edits", async ({
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
	await expect(bodyCards(page).first().getByRole("textbox", { name: "Callout title" })).toHaveValue(
		"Server normalization"
	);
	await expect(bodyCards(page).first()).toHaveAttribute("data-block-key", key);
	await expect(bodyEditor(page)).toHaveAttribute("contenteditable", "true");
	await page
		.getByRole("navigation", { name: "Breadcrumb" })
		.getByRole("link", { name: "Block articles", exact: true })
		.click();
	await expect(page).toHaveURL((url) => url.pathname === "/admin/collections/block-articles");
	await expect(page.getByRole("dialog", { name: "Leave without saving?" })).toHaveCount(0);
	await page.goto(`/admin/collections/block-articles/${original.id}`);
	await expect(bodyCards(page).first().getByRole("textbox", { name: "Callout title" })).toHaveValue(
		"Server normalization"
	);
	await bodyEditor(page).locator("p").last().click();
	await bodyEditor(page).press("ControlOrMeta+End");
	await page.keyboard.type(" edited after save");
	await expect(bodyEditor(page)).toContainText("edited after save");
	const saved = await saveArticle(page);
	expect(
		saved.body.root.children.find((node: { type: string }) => node.type === "block").fields
	).toMatchObject({ _key: key, title: "Server normalization" });
	await page.reload();
	await expect(bodyCards(page).first().getByRole("textbox", { name: "Callout title" })).toHaveValue(
		"Server normalization"
	);
	await expect(bodyEditor(page)).toContainText("edited after save");
	expect(errors.pageErrors).toEqual([]);
});
