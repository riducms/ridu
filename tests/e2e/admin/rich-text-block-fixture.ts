import { expect, type Locator, type Page } from "@playwright/test";

export async function insertBlock(page: Page, editor: Locator, type: string) {
	await editor.focus();
	await editor.press("ControlOrMeta+End");
	await editor.press("Enter");
	await page.keyboard.type(`/${type}`);
	const option = page.getByRole("option", { name: new RegExp(`^${type} Structured block`) });
	await expect(option).toBeVisible();
	await page.keyboard.press("Enter");
	const drawer = page.getByRole("dialog", { name: `Insert ${type}`, exact: true });
	await expect(drawer).toBeVisible();
	return drawer;
}

export function bodyEditor(page: Page) {
	return page.getByRole("textbox", { name: "Body", exact: true });
}
export function bodyCards(page: Page) {
	return page.locator('[data-field-path="body"] .ridu-richtext-embedded-card');
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
