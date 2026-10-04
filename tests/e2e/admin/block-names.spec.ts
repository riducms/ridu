import { expect, test } from "./fixture";
import { submitDocumentForm, loginAsEditor, observePageErrors } from "./helpers";
import { block, richDocument, bodyCards, bodyEditor, insertBlock } from "./rich-text-block-fixture";

test("names stay separate from content through collapse, reorder, duplicate and save", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/block-names?draft=true", {
		data: {
			title: "Named layout",
			layout: [
				{ blockType: "cta", heading: "Published heading" },
				{
					blockType: "cta",
					heading: "Protected heading",
					blockName: "Protected name",
					nameLocked: true,
				},
				{
					blockType: "hidden-name",
					heading: "Visible content",
					blockName: "Secret editorial name",
				},
			],
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/block-names/${original.id}`);
	const layout = page.locator('[data-field-path="layout"]').first();
	const input = page.locator('input[name="layout.0.blockName"]');
	await expect(input).toHaveValue("");
	await expect(input).toHaveAttribute("placeholder", "Untitled");
	await expect(layout.getByRole("textbox", { name: "Block name", exact: true })).toHaveCount(2);
	await expect(page.locator('input[name="layout.1.blockName"]')).not.toBeEditable();
	await expect(layout).not.toContainText("Secret editorial name");
	await expect(layout.locator('[aria-label*="Secret editorial name"]')).toHaveCount(0);
	await expect(layout.getByPlaceholder("Secret editorial name")).toHaveCount(0);
	await layout.getByRole("button", { name: "Collapse", exact: true }).first().click();
	await expect(page.locator('input[name="layout.0.heading"]')).toHaveCount(0);
	await input.fill("Footer newsletter");
	await input.press("Enter");
	await expect(input).toHaveValue("Footer newsletter");
	await layout.getByRole("button", { name: "Open Footer newsletter actions", exact: true }).click();
	await page.getByRole("menuitem", { name: "Move down", exact: true }).click();
	await expect(page.locator('input[name="layout.1.blockName"]')).toHaveValue("Footer newsletter");
	await expect(page.locator('input[name="layout.0.blockName"]')).not.toBeEditable();
	await layout.getByRole("button", { name: "Open Footer newsletter actions", exact: true }).click();
	await page.getByRole("menuitem", { name: "Duplicate", exact: true }).click();
	await expect(page.locator('input[name="layout.2.blockName"]')).toHaveValue("Footer newsletter");
	const saved = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/block-names/${original.id}`
	);
	await submitDocumentForm(page);
	const response = await saved;
	expect(response.ok(), await response.text()).toBe(true);
	await page.reload();
	const stored = (
		await (await page.request.get(`/api/collections/block-names/${original.id}`)).json()
	).doc;
	expect(stored.layout[1]).toMatchObject({
		_key: original.layout[0]._key,
		blockName: "Footer newsletter",
		heading: "Published heading",
	});
	expect(stored.layout[2]._key).not.toBe(stored.layout[1]._key);
	expect(stored.layout[0]).toMatchObject({
		_key: original.layout[1]._key,
		blockName: "Protected name",
	});
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("rich-text names retain focus and history beside inline block fields", async ({ page }) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/block-names?draft=true", {
		data: {
			title: "Named rich text",
			body: richDocument([
				block("callout", { heading: "Content heading", detail: richDocument([]) }),
				block("cta", { heading: "Second heading", blockName: "Second name" }),
				block("hidden-name", {
					heading: "Visible hidden-name content",
					blockName: "Secret rich-text editorial name",
				}),
			]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/block-names/${original.id}`);
	const cards = bodyCards(page);
	const name = cards.first().getByRole("textbox", { name: "Block name", exact: true });
	await expect(name).toHaveAttribute("placeholder", "Untitled");
	const headerAlignment = await cards.first().evaluate((card) => {
		const input = card.querySelector("input");
		const type = card.querySelector("[data-block-select] > span");
		if (!(input instanceof HTMLInputElement) || !(type instanceof HTMLElement)) {
			throw new Error("Expected the inline block header and type label.");
		}
		const name = input.getBoundingClientRect();
		const label = type.getBoundingClientRect();
		return Math.abs(name.top + name.height / 2 - (label.top + label.height / 2));
	});
	expect(headerAlignment).toBeLessThan(30);
	await page.setViewportSize({ width: 390, height: 844 });
	const narrowCard = await cards.first().evaluate((card) => {
		const type = card.querySelector("[data-block-select] > span");
		if (!(type instanceof HTMLElement)) throw new Error("Expected the block type label.");
		return {
			cardHeight: card.getBoundingClientRect().height,
			lineHeight: Number.parseFloat(getComputedStyle(type).lineHeight),
			typeHeight: type.getBoundingClientRect().height,
		};
	});
	expect(narrowCard.typeHeight).toBeLessThan(narrowCard.lineHeight * 1.5);
	expect(narrowCard.cardHeight).toBeGreaterThan(narrowCard.typeHeight);
	await page.setViewportSize({ width: 1280, height: 720 });
	await name.pressSequentially("Hero promotion");
	const composition = await name.evaluate((element) => {
		if (!(element instanceof HTMLInputElement)) throw new Error("Expected the block name input.");
		const input = element;
		input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true }));
		input.value += " 漢字";
		input.dispatchEvent(
			new InputEvent("input", {
				bubbles: true,
				composed: true,
				data: " 漢字",
				inputType: "insertCompositionText",
				isComposing: true,
			})
		);
		const enterAccepted = input.dispatchEvent(
			new KeyboardEvent("keydown", {
				bubbles: true,
				cancelable: true,
				key: "Enter",
				isComposing: true,
			})
		);
		input.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data: " 漢字" }));
		return { enterAccepted, value: input.value };
	});
	expect(composition).toEqual({ enterAccepted: true, value: "Hero promotion 漢字" });
	await expect(name).toBeFocused();
	await name.press("ControlOrMeta+z");
	await expect(name).toHaveValue("");
	await name.press("ControlOrMeta+Shift+z");
	await expect(name).toHaveValue("Hero promotion 漢字");
	await name.press("Enter");
	await name.press("Alt+Shift+ArrowDown");
	await expect(cards).toHaveCount(3);
	await expect(cards.first().getByRole("textbox", { name: "Block name", exact: true })).toHaveValue(
		"Hero promotion 漢字"
	);
	const hiddenCard = cards.nth(2);
	await expect(hiddenCard.getByRole("textbox", { name: "Block name", exact: true })).toHaveCount(0);
	await expect(hiddenCard).not.toContainText("Secret rich-text editorial name");
	await expect(hiddenCard.locator('[aria-label*="Secret rich-text editorial name"]')).toHaveCount(
		0
	);
	await expect(hiddenCard.getByPlaceholder("Secret rich-text editorial name")).toHaveCount(0);
	await cards.first().getByRole("button", { name: "Collapse Callout" }).click();
	await expect(cards.first().getByRole("textbox", { name: "Heading", exact: true })).toHaveCount(0);
	await expect(name).toHaveValue("Hero promotion 漢字");
	await cards.first().getByRole("button", { name: "Expand Callout" }).click();
	await cards.first().getByRole("textbox", { name: "Heading", exact: true }).fill("Edited heading");
	await expect(name).toHaveValue("Hero promotion 漢字");
	await name.fill("Saved immediately");
	const saved = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/block-names/${original.id}`
	);
	await submitDocumentForm(page);
	const response = await saved;
	expect(response.ok(), await response.text()).toBe(true);
	await page.reload();
	await expect(name).toHaveValue("Saved immediately");
	const stored = (
		await (await page.request.get(`/api/collections/block-names/${original.id}`)).json()
	).doc;
	expect(stored.body.root.children[0].fields).toMatchObject({
		_key: original.body.root.children[0].fields._key,
		heading: "Edited heading",
		blockName: "Saved immediately",
	});
	expect(stored.body.root.children[1].fields.blockName).toBe("Second name");
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("server name validation focuses the header without mounting collapsed content", async ({
	page,
}) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/block-names?draft=true", {
		data: { title: "Header validation", layout: [{ blockType: "cta", heading: "Heading" }] },
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/block-names/${original.id}`);
	const layout = page.locator('[data-field-path="layout"]').first();
	await layout.getByRole("button", { name: "Collapse", exact: true }).click();
	const name = layout.getByRole("textbox", { name: "Block name", exact: true });
	await name.fill("invalid");
	await submitDocumentForm(page);
	await expect(layout.getByText("Choose a descriptive block name", { exact: true })).toBeVisible();
	await expect(name).toBeFocused();
	await expect(page.locator('input[name="layout.0.heading"]')).toHaveCount(0);
	await name.fill("");
	await submitDocumentForm(page);
	await page.reload();
	await expect(name).toHaveValue("");
	await expect(name).toHaveAttribute("placeholder", "Untitled");
	expect((await name.boundingBox())?.width).toBeLessThan(100);
});

test("new blocks have independent header names without application naming configuration", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	await page.goto("/admin/collections/block-articles/create?locale=en");
	await page.getByRole("textbox", { name: "Title", exact: true }).fill("Default block names");
	const card = await insertBlock(page, bodyEditor(page), "Callout");
	const name = card.getByRole("textbox", { name: "Block name", exact: true });
	const title = card.getByRole("textbox", { name: "Callout title", exact: true });
	const caption = card.getByRole("textbox", { name: "Caption", exact: true });
	await expect(name).toHaveValue("");
	await expect(name).toHaveAttribute("placeholder", "Untitled");
	await expect(caption).toHaveValue("Helpful context");
	await expect(card.locator(".ridu-richtext-block__header")).not.toContainText("Helpful context");
	await expect(card.getByRole("textbox", { name: "Block name", exact: true })).toHaveCount(1);
	await title.fill("Article heading");
	await name.pressSequentially("Editor note");
	await expect(name).toBeFocused();
	await name.hover();
	await expect(name).not.toHaveCSS("box-shadow", "none");
	await name.press("ControlOrMeta+z");
	await expect(name).toHaveValue("");
	await expect(title).toHaveValue("Article heading");
	await name.press("ControlOrMeta+Shift+z");
	await expect(name).toHaveValue("Editor note");
	await card.getByRole("button", { name: "Collapse Callout" }).click();
	await expect(name).toHaveValue("Editor note");
	const response = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === "/api/collections/block-articles"
	);
	await submitDocumentForm(page);
	const saved = await response;
	expect(saved.ok(), await saved.text()).toBe(true);
	const stored = (await saved.json()).doc;
	expect(
		stored.body.root.children.find((node: { type: string }) => node.type === "block").fields
	).toMatchObject({
		blockName: "Editor note",
		title: "Article heading",
		caption: "Helpful context",
	});
	await page.waitForURL((url) => url.pathname === `/admin/collections/block-articles/${stored.id}`);
	await page.reload();
	await expect(
		bodyCards(page).first().getByRole("textbox", { name: "Block name", exact: true })
	).toHaveValue("Editor note");
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});
