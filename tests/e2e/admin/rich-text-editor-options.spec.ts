import { expect, test } from "./fixture";

import { loginAsEditor, observePageErrors, selectRichText } from "./helpers";

test("a fixed toolbar formats text beside the floating toolbar in a gutterless editor", async ({
	page,
}) => {
	const errors = observePageErrors(page);
	await loginAsEditor(page);
	await page.goto("/admin/collections/editor-options/create");
	const field = page.locator('[data-field-path="pinned"]');
	const toolbar = field.getByRole("toolbar", { name: "Editor toolbar" });
	const editor = page.getByRole("textbox", { name: "Pinned toolbar" });
	await expect(toolbar).toBeVisible();

	// Before the editor has a caret, a toolbar command places one at the end.
	await toolbar.getByRole("button", { name: "Bold" }).click();
	await expect(editor).toBeFocused();
	await page.keyboard.type("Bold start");
	await expect(editor.locator("strong")).toHaveText("Bold start");
	await expect(toolbar.getByRole("button", { name: "Bold" })).toHaveAttribute(
		"aria-pressed",
		"true"
	);

	// The fixed toolbar follows the caret and changes the current block.
	await toolbar.getByRole("button", { name: "Text style", exact: true }).click();
	await page.getByRole("menuitem", { name: "Heading 2", exact: true }).click();
	await expect(editor.locator(":scope > h2")).toHaveText("Bold start");
	await expect(editor).toBeFocused();
	// A new paragraph keeps the caret's bold format until the toolbar turns it off.
	await page.keyboard.press("Enter");
	await expect(toolbar.getByRole("button", { name: "Bold" })).toHaveAttribute(
		"aria-pressed",
		"true"
	);
	await toolbar.getByRole("button", { name: "Bold" }).click();
	await expect(toolbar.getByRole("button", { name: "Bold" })).toHaveAttribute(
		"aria-pressed",
		"false"
	);
	await page.keyboard.type("Plain words");
	await expect(editor.locator(":scope > p strong")).toHaveCount(0);
	await expect(toolbar.getByRole("button", { name: "Add link" })).toBeDisabled();

	// Selecting text still shows the floating toolbar, and both reflect the same selection.
	await selectRichText(page, editor);
	const floating = page.getByRole("toolbar", { name: "Text formatting" });
	await expect(floating).toBeVisible();
	await floating.getByRole("button", { name: "Italic" }).click();
	await expect(editor.locator(":scope > p em")).toHaveText("Plain words");
	await expect(toolbar.getByRole("button", { name: "Italic" })).toHaveAttribute(
		"aria-pressed",
		"true"
	);
	await expect(toolbar.getByRole("button", { name: "Add link" })).toBeEnabled();

	// Without a gutter, text starts at the field edge and block handles sit outside it.
	await expect(editor).toHaveCSS("padding-inline-start", "0px");
	await editor.locator(":scope > p").hover();
	const blockActions = field.getByRole("toolbar", { name: "Block actions" });
	await expect(blockActions).toBeVisible();
	const handle = await blockActions.boundingBox();
	const text = await editor.boundingBox();
	expect(handle!.x + handle!.width).toBeLessThanOrEqual(text!.x);

	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("hidden block handles keep keyboard movement and the / menu", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/editor-options/create");
	const field = page.locator('[data-field-path="minimal"]');
	const editor = page.getByRole("textbox", { name: "Minimal" });
	await editor.click();
	await page.keyboard.type("First");
	await page.keyboard.press("Enter");
	await page.keyboard.type("Second");

	await editor.getByText("First").hover();
	await expect(field.getByRole("button", { name: "Drag to move block" })).toHaveCount(0);
	await expect(field.getByRole("button", { name: "Add block" })).toHaveCount(0);
	await expect(field.getByRole("button", { name: "Insert paragraph" })).toHaveCount(0);
	await expect(field.getByRole("toolbar", { name: "Block actions" })).toBeHidden();

	await editor.getByText("Second").click();
	await editor.press("Alt+Shift+ArrowUp");
	await expect(editor.locator("p")).toHaveText(["Second", "First"]);
	await expect(field.getByRole("toolbar", { name: "Editor toolbar" })).toHaveCount(0);
});

test("a fixed toolbar stays in view below the document actions", async ({ page }) => {
	await loginAsEditor(page);
	await page.setViewportSize({ width: 1280, height: 720 });
	await page.goto("/admin/collections/editor-options/create");
	const editor = page.getByRole("textbox", { name: "Pinned toolbar" });
	await editor.click();
	for (let line = 0; line < 40; line++) {
		await page.keyboard.type(`Line ${line}`);
		await page.keyboard.press("Enter");
	}
	await editor.getByText("Line 30", { exact: true }).scrollIntoViewIfNeeded();

	const toolbar = page.getByRole("toolbar", { name: "Editor toolbar" });
	await expect(toolbar).toBeInViewport();
	await expect
		.poll(async () => {
			const bar = await page.locator(".ridu-document-bar").boundingBox();
			const pinned = await toolbar.boundingBox();
			return pinned === null || bar === null ? -1 : Math.round(pinned.y - (bar.y + bar.height));
		})
		.toBe(0);
});
