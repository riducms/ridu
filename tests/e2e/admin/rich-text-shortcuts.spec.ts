import { expect, test } from "./fixture";
import { documentSaveButton, loginAsEditor, observePageErrors } from "./helpers";

test("rich-text shortcuts create editable checklists and persist headings and links", async ({
	page,
}) => {
	const { consoleErrors, pageErrors } = observePageErrors(page);
	await loginAsEditor(page);
	consoleErrors.length = 0;
	await page.goto("/admin/collections/posts/create");
	const editor = page.getByRole("textbox", { name: "Content" });
	await editor.click();
	await page.keyboard.type("###### Sixth heading");
	await expect(editor.locator("h6")).toHaveText("Sixth heading");
	await page.keyboard.press("Enter");
	await page.keyboard.type("[docs](ridu.dev)");
	await expect(editor.locator("a")).toHaveAttribute("href", "https://ridu.dev");
	await page.keyboard.press("ArrowRight");
	await page.keyboard.press("Enter");
	await page.keyboard.type("[ ] Verify the draft");
	const checklistItem = editor.getByRole("checkbox");
	await expect(checklistItem).toHaveAttribute("aria-checked", "false");
	await checklistItem.click({ position: { x: 4, y: 8 } });
	await expect(checklistItem).toHaveAttribute("aria-checked", "true");

	await page.locator('input[name="title"]').fill("Shortcut parity");
	await page.getByLabel("Summary — English", { exact: true }).fill("Persisted shortcut content");
	await documentSaveButton(page).click();
	await expect(page).not.toHaveURL(/\/create$/);
	await page.reload();
	await expect(editor.locator("h6")).toHaveText("Sixth heading");
	await expect(editor.locator("a")).toHaveAttribute("href", "https://ridu.dev");
	await expect(editor.getByRole("checkbox")).toHaveAttribute("aria-checked", "true");
	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});
