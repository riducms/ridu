import type { Locator, Page } from "@playwright/test";

import { expect, test } from "./fixture";
import { loginAsEditor, observePageErrors } from "./helpers";

async function insertEmbed(page: Page, editor: Locator, query: string, option: RegExp) {
	await editor.locator("p").first().hover();
	await page.getByRole("button", { name: "Add block" }).click();
	const picker = page.getByRole("dialog", { name: "Insert block" });
	await picker.getByRole("combobox", { name: "Filter blocks" }).fill(query);
	await picker.getByRole("option", { name: option }).click();
}

async function trailingParagraphs(editor: Locator, nodeClass: string) {
	return editor.evaluate((root, className) => {
		const embed = Array.from(root.children).find((child) => child.classList.contains(className));
		if (embed === undefined) return -1;
		let count = 0;
		for (let next = embed.nextElementSibling; next?.tagName === "P"; next = next.nextElementSibling)
			count += 1;
		return count;
	}, nodeClass);
}

test("upload insertion, editing, swap, and removal keep one usable paragraph", async ({ page }) => {
	const { consoleErrors, pageErrors } = observePageErrors(page);
	await loginAsEditor(page);
	consoleErrors.length = 0;
	await page.goto("/admin/collections/posts/create");
	const editor = page.getByRole("textbox", { name: "Content" });
	await insertEmbed(page, editor, "asset", /^Asset$/);
	const browser = page.getByRole("dialog", { name: /Select asset/i });
	await browser
		.getByRole("table", { name: "Results" })
		.getByRole("button", { name: "ridu-cover.png", exact: true })
		.click();
	const card = editor.getByRole("group", { name: "Asset: ridu-cover.png" });
	await expect(card).toBeVisible();
	await expect(editor.locator(":scope > p")).toHaveCount(1);
	await expect.poll(() => trailingParagraphs(editor, "ridu-richtext-upload")).toBe(1);
	await expect(card.getByRole("button", { name: "Edit ridu-cover.png" })).toHaveCount(1);
	await card.locator(".ridu-richtext-upload-card__image").click();
	await expect(page.getByRole("dialog", { name: "ridu-cover.png" })).toBeHidden();
	await card.getByRole("textbox", { name: "Caption for ridu-cover.png" }).fill("Old asset caption");

	await card.getByRole("button", { name: "Edit ridu-cover.png" }).click();
	const documentDrawer = page.getByRole("dialog", { name: "ridu-cover.png" });
	await expect(documentDrawer).toBeVisible();
	await expect(documentDrawer.getByRole("textbox", { name: "Alt text" })).toHaveValue(
		"Warm orange Ridu cover"
	);
	await documentDrawer.getByRole("button", { name: "Close relationship browser" }).click();
	await expect(documentDrawer).toBeHidden();

	await card.hover();
	await card.getByRole("button", { name: "Replace ridu-cover.png" }).click();
	await browser
		.getByRole("table", { name: "Results" })
		.getByRole("button", { name: "field-notes.png", exact: true })
		.click();
	const replacement = editor.getByRole("group", { name: "Asset: field-notes.png" });
	await expect(replacement).toBeVisible();
	await expect(card).toHaveCount(0);
	await expect(editor.getByRole("group", { name: /^Asset: / })).toHaveCount(1);
	await expect(
		replacement.getByRole("textbox", { name: "Caption for field-notes.png" })
	).toHaveValue("");
	await replacement.hover();
	await replacement.getByRole("button", { name: "Remove field-notes.png" }).click();
	await expect(replacement).toBeHidden();
	await expect(editor).toBeFocused();
	await page.keyboard.type("Continued after asset");
	await expect(editor.locator(":scope > p")).toHaveText("Continued after asset");
	expect(pageErrors).toEqual([]);
	expect(consoleErrors).toEqual([]);
});

test("relationship insertion, editing, swap, and removal keep one usable paragraph", async ({
	page,
}) => {
	const { consoleErrors, pageErrors } = observePageErrors(page);
	await loginAsEditor(page);
	consoleErrors.length = 0;
	await page.goto("/admin/collections/posts/create");
	const editor = page.getByRole("textbox", { name: "Content" });
	await editor.fill("Relationship workflow");
	await insertEmbed(page, editor, "relation", /^Relationship$/);
	await page
		.getByRole("dialog")
		.getByRole("combobox", { name: "Select a Collection to Browse" })
		.fill("Post");
	await page.getByRole("option", { name: "Post", exact: true }).click();
	const browser = page.getByRole("dialog", { name: "Select post" });
	await browser.getByRole("searchbox").fill("Relationship field notes");
	await browser
		.getByRole("table", { name: "Results" })
		.getByRole("button", { name: "Relationship field notes", exact: true })
		.click();
	const card = editor.getByRole("group", { name: /Post: Relationship field notes/ });
	await expect(card).toBeVisible();
	await expect.poll(() => trailingParagraphs(editor, "ridu-richtext-relationship")).toBe(1);

	await card.getByRole("button", { name: "Relationship field notes", exact: true }).click();
	const documentDrawer = page.getByRole("dialog", { name: "Relationship field notes" });
	await expect(documentDrawer).toBeVisible();
	await expect(documentDrawer.getByRole("textbox", { name: "Content" })).toBeVisible();
	await documentDrawer.getByRole("button", { name: "Close relationship browser" }).click();
	await expect(documentDrawer).toBeHidden();

	await card.getByRole("button", { name: "Replace" }).click();
	await browser.getByRole("searchbox").fill("Welcome to Ridu");
	await browser
		.getByRole("table", { name: "Results" })
		.getByRole("button", { name: "Welcome to Ridu", exact: true })
		.click();
	const replacement = editor.getByRole("group", { name: /Post: Welcome to Ridu/ });
	await expect(replacement).toBeVisible();
	await expect(card).toHaveCount(0);
	await expect(editor.getByRole("group", { name: /^Post: / })).toHaveCount(1);
	await replacement.getByRole("button", { name: "Remove" }).click();
	await expect(replacement).toBeHidden();
	await expect(editor).toBeFocused();
	await page.keyboard.type("Continued after relationship");
	await expect(editor.locator(":scope > p")).toHaveText([
		"Relationship workflow",
		"Continued after relationship",
	]);
	expect(pageErrors).toEqual([]);
	expect(consoleErrors).toEqual([]);
});
