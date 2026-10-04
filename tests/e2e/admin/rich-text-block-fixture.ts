import { expect, type Locator, type Page } from "@playwright/test";

export async function insertBlock(page: Page, editor: Locator, type: string) {
	const blocks = editor.locator(":scope > .ridu-richtext-embedded > article[data-block-type]");
	const before = await blocks.evaluateAll((elements) =>
		elements.map((element) => element.getAttribute("data-block-key"))
	);
	const trailingParagraph = editor.locator(":scope > p").last();
	if (await trailingParagraph.count()) {
		await trailingParagraph.click();
		await page.keyboard.press("End");
		if ((await trailingParagraph.textContent())?.trim()) await page.keyboard.press("Enter");
	} else {
		await editor.focus();
		await editor.press("ControlOrMeta+End");
		await editor.press("Enter");
	}
	// Place the caret in the committed empty paragraph before opening the menu.
	// Typing during Enter's native caret scroll can dismiss the upstream typeahead anchor.
	const insertionParagraph = editor.locator(":scope > p").last();
	await expect(insertionParagraph).toHaveText("");
	await insertionParagraph.click();
	await page.keyboard.type(`/${type}`);
	const option = page.getByRole("option", { name: type, exact: true });
	await expect(option).toBeVisible();
	await expect(option).toHaveAttribute("aria-selected", "true");
	await page.keyboard.press("Enter");
	await expect(blocks).toHaveCount(before.length + 1);
	await expect
		.poll(async () => {
			const keys = await blocks.evaluateAll((elements) =>
				elements.map((element) => element.getAttribute("data-block-key"))
			);
			return keys.find((candidate) => candidate && !before.includes(candidate));
		})
		.toBeTruthy();
	const newKey = await blocks.evaluateAll(
		(elements, oldKeys) =>
			elements
				.map((element) => element.getAttribute("data-block-key"))
				.find((candidate) => candidate && !oldKeys.includes(candidate)),
		before
	);
	if (!newKey) throw new Error(`Could not identify the inserted ${type} block.`);
	const block = editor.locator(
		`:scope > .ridu-richtext-embedded > article[data-block-key="${newKey}"]`
	);
	await expect(block).toBeVisible();
	return block;
}

export function bodyEditor(page: Page) {
	return page.getByRole("textbox", { name: "Body", exact: true });
}
export function bodyCards(page: Page) {
	return page.locator('[data-field-path="body"] article.ridu-richtext-block');
}
export const richDocument = (children: unknown[]) => ({
	version: 1,
	root: { type: "root", version: 1, children },
});
export const block = (blockType: string, fields: Record<string, unknown>) => ({
	type: "block",
	version: 1,
	fields: { blockType, ...fields },
});
