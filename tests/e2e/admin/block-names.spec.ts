import { expect, test } from "./fixture";
import { documentSaveButton, loginAsEditor, observePageErrors } from "./helpers";
import { block, richDocument, bodyCards } from "./rich-text-block-fixture";

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
	await expect(input).toHaveAttribute("placeholder", "Published heading");
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
	await documentSaveButton(page).click();
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

test("rich-text names retain focus and history and share drawer Apply/Cancel", async ({ page }) => {
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
	await expect(name).toHaveAttribute("placeholder", "Content heading");
	const headerGap = await cards.first().evaluate((card) => {
		const input = card.querySelector("input");
		const type = card.querySelector("[data-block-select] > span");
		if (!(input instanceof HTMLInputElement) || !(type instanceof HTMLElement))
			throw new Error("Expected the named card header and type label.");
		return type.getBoundingClientRect().top - input.getBoundingClientRect().bottom;
	});
	expect(headerGap).toBeGreaterThanOrEqual(0);
	expect(headerGap).toBeLessThan(24);
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
	expect(narrowCard.cardHeight).toBeLessThan(180);
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
	await cards.first().getByRole("button", { name: "Edit", exact: true }).click();
	const drawer = page.getByRole("dialog", { name: "Edit Callout", exact: true });
	const draftName = drawer.getByRole("textbox", { name: "Block name", exact: true });
	await expect(name).not.toBeEditable();
	await expect(draftName).toHaveCount(1);
	await draftName.fill("Cancelled name");
	await drawer.getByRole("button", { name: "Cancel", exact: true }).click();
	await expect(name).toHaveValue("Hero promotion 漢字");
	await cards.first().getByRole("button", { name: "Edit", exact: true }).click();
	await draftName.fill("Applied name");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await expect(name).toHaveValue("Applied name");
	await name.fill("Saved immediately");
	const saved = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/block-names/${original.id}`
	);
	await documentSaveButton(page).click();
	const response = await saved;
	expect(response.ok(), await response.text()).toBe(true);
	await page.reload();
	await expect(name).toHaveValue("Saved immediately");
	const stored = (
		await (await page.request.get(`/api/collections/block-names/${original.id}`)).json()
	).doc;
	expect(stored.body.root.children[0].fields).toMatchObject({
		_key: original.body.root.children[0].fields._key,
		heading: "Content heading",
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
	await documentSaveButton(page).click();
	await expect(layout.getByText("Choose a descriptive block name", { exact: true })).toBeVisible();
	await expect(name).toBeFocused();
	await expect(page.locator('input[name="layout.0.heading"]')).toHaveCount(0);
	await name.fill("");
	await documentSaveButton(page).click();
	await page.reload();
	await expect(name).toHaveValue("");
	await expect(name).toHaveAttribute("placeholder", "Heading");
});
