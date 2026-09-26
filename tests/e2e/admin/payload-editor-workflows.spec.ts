import { expect, test } from "./fixture";
import { loginAsEditor, selectRichText } from "./helpers";

test("one relationship command selects a Post and replaces it with a Category", async ({
	page,
}) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts/create");
	const editor = page.getByRole("textbox", { name: "Content" });
	await editor.click();
	await page.keyboard.type("/rela");
	const menu = page.getByRole("listbox", { name: "Insert block" });
	await expect(menu.getByRole("option")).toHaveCount(1);
	await expect(menu.getByRole("option")).toHaveText("Relationship");
	await page.keyboard.press("Enter");
	const drawer = page.getByRole("dialog");
	const collections = drawer.getByRole("combobox", { name: "Select a Collection to Browse" });
	await collections.click();
	await expect(page.getByRole("option", { name: "Asset", exact: true })).toBeVisible();
	await collections.fill("Post");
	await expect(page.getByRole("option", { name: "Post", exact: true })).toBeVisible();
	await collections.press("ArrowDown");
	await expect(page.getByRole("option", { name: "Post", exact: true })).toHaveAttribute(
		"data-highlighted",
		""
	);
	await collections.press("Enter");
	await expect(drawer.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();
	await drawer.getByRole("searchbox").fill("Relationship field notes");
	await drawer.getByRole("button", { name: "Relationship field notes", exact: true }).click();
	const post = editor.getByRole("group", { name: "Post: Relationship field notes" });
	await expect(post).toBeVisible();
	await page.setViewportSize({ width: 390, height: 844 });
	await post.getByRole("button", { name: "Replace", exact: true }).click();
	await expect
		.poll(() => drawer.evaluate((element) => element.scrollWidth - element.clientWidth))
		.toBeLessThanOrEqual(1);
	await collections.fill("Category");
	await page.getByRole("option", { name: "Category", exact: true }).click();
	await expect(drawer.getByRole("heading", { name: "Categories", exact: true })).toBeVisible();
	await expect(drawer.getByRole("searchbox")).toHaveValue("");
	await drawer.getByRole("button", { name: "Guides", exact: true }).click();
	const category = editor.getByRole("group", { name: "Category: Guides" });
	const relationships = editor.locator(".ridu-richtext-relationship-card");
	await expect(category).toBeVisible();
	await expect(post).toHaveCount(0);
	await expect(relationships).toHaveCount(1);
	await editor.press("ControlOrMeta+z");
	await expect(post).toBeVisible();
	await expect(category).toHaveCount(0);
	await expect(relationships).toHaveCount(1);
	await post.getByRole("button", { name: "Replace", exact: true }).click();
	await drawer.getByRole("button", { name: "Close relationship browser" }).click();
	await expect(post).toBeVisible();
	await expect(editor).toBeFocused();
});

test("slash search follows Payload's query boundary and matches compact labels", async ({
	page,
}) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts/create");

	const editor = page.getByRole("textbox", { name: "Content" });
	await editor.click();
	await page.keyboard.type("/");
	const menu = page.getByRole("listbox", { name: "Insert block" });
	await expect(menu).toBeVisible();
	await expect(menu.locator(".ridu-richtext-menu__heading").first()).toHaveText("Lists");
	await expect(menu.locator(".ridu-richtext-menu__heading").nth(1)).toHaveText("Basic");
	await expect(menu.getByRole("option").first()).toContainText("Checklist");

	await page.keyboard.type("numberedlist");
	await expect(menu.getByRole("option", { name: "Numbered list" })).toBeVisible();
	await page.keyboard.type(" ");
	await expect(menu).toBeHidden();
	await expect.poll(() => editor.evaluate((element) => element.textContent)).toBe("/numberedlist ");
	await expect(editor.locator("ol, ul")).toHaveCount(0);

	await editor.press("ControlOrMeta+a");
	await editor.press("Backspace");
	await expect(editor).toHaveText("");
	await page.keyboard.type("/heading");
	await expect(menu).toBeVisible();
	await page.keyboard.type(".");
	await expect(menu).toBeHidden();
	await expect.poll(() => editor.evaluate((element) => element.textContent)).toBe("/heading.");
	await expect(editor.locator("h1, h2, h3, h4, h5, h6")).toHaveCount(0);
});

test("inline toolbar removes an active link", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts/create");

	const editor = page.getByRole("textbox", { name: "Content" });
	await editor.fill("Payload link action");
	await selectRichText(page, editor);
	await page.getByRole("button", { name: "Add link" }).click();
	const drawer = page.getByRole("dialog", { name: "Edit link" });
	await drawer.getByRole("textbox", { name: "Link URL" }).fill("example.com");
	await drawer.getByRole("button", { name: "Save changes" }).click();
	await expect(editor.locator("a")).toHaveAttribute("href", "https://example.com");
	await expect(drawer).toBeHidden();
	await editor.scrollIntoViewIfNeeded();

	await selectRichText(page, editor);
	const removeLink = page.getByRole("toolbar", { name: "Text formatting" }).getByRole("button", {
		name: "Remove link",
	});
	await expect(removeLink).toHaveAttribute("aria-pressed", "true");
	await removeLink.click();
	await expect(editor.locator("a")).toHaveCount(0);
	await expect(editor).toHaveText("Payload link action");
});
