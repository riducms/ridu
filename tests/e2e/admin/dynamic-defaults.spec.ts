import { expect, test, type Page } from "./fixture";
import { documentSaveButton, loginAsEditor, observePageErrors } from "./helpers";

async function save(page: Page, id?: string) {
	const response = page.waitForResponse(
		(result) =>
			result.request().method() === (id === undefined ? "POST" : "PATCH") &&
			new URL(result.url()).pathname ===
				`/api/collections/dynamic-defaults${id === undefined ? "" : `/${id}`}`
	);
	await documentSaveButton(page).click();
	return response;
}

test("ordinary forms leave dynamic defaults omitted until save and retain explicit clears", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	await page.goto("/admin/collections/dynamic-defaults/create");
	await expect(page.locator('input[name="title"]')).toHaveValue("");
	await expect(page.locator('input[name="localizedTitle"]')).toHaveValue("");
	// Editing and clearing an optional input is explicit empty input, not omission.
	await page.locator('input[name="note"]').fill("Draft note");
	await page.locator('input[name="note"]').fill("");
	await expect(page.locator('input[name="title"]')).not.toHaveAttribute("required");
	const createResponse = page.waitForResponse(
		(result) =>
			result.request().method() === "POST" &&
			new URL(result.url()).pathname === "/api/collections/dynamic-defaults"
	);
	await page.locator('input[name="note"]').press("Enter");
	const created = await createResponse;
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	expect(created.request().postDataJSON()).not.toHaveProperty("title");
	expect(original.title).toBe("Untitled");
	expect(original.localizedTitle).toBe("Untitled");
	expect(original.note).toBe("");
	await expect(page.locator('input[name="title"]')).toHaveValue("Untitled");
	await expect(page.locator('input[name="note"]')).toHaveValue("");
	await expect(page.locator('input[name="title"]')).toHaveAttribute("required");
	await page.locator('input[name="title"]').fill("Saved title");
	expect((await save(page, original.id)).ok()).toBe(true);
	await page.reload();
	await expect(page.locator('input[name="title"]')).toHaveValue("Saved title");
	await expect(page.locator('input[name="note"]')).toHaveValue("");
	expect(errors.pageErrors).toEqual([]);
});

test("embedded rich-text insertion defers required dynamic defaults until the parent save", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/dynamic-defaults", {
		data: {
			body: {
				version: 1,
				root: {
					type: "root",
					version: 1,
					children: [
						{
							type: "paragraph",
							version: 1,
							children: [{ type: "text", version: 1, text: "Insertion anchor" }],
						},
					],
				},
			},
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/dynamic-defaults/${original.id}`);
	const body = page.locator('[data-field-path="body"]').first();
	await body.locator(".ridu-richtext-content p").first().hover();
	await body
		.getByRole("toolbar", { name: "Block actions" })
		.getByRole("button", { name: "Add block", exact: true })
		.click();
	const picker = page.getByRole("dialog", { name: "Insert block", exact: true });
	await picker.getByRole("option", { name: /^Card Structured block/ }).click();
	const drawer = page.getByRole("dialog", { name: "Insert Card", exact: true });
	await expect(drawer.locator('input[name$=".title"]')).toHaveValue("");
	await expect(drawer.locator('input[name$=".note"]')).toHaveValue("");
	await drawer.locator('input[name$=".note"]').fill("Draft");
	await drawer.locator('input[name$=".note"]').fill("");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	await expect(drawer).toBeHidden();
	const saved = await save(page, original.id);
	expect(saved.ok(), await saved.text()).toBe(true);
	const document = (await saved.json()).doc;
	const block = document.body.root.children.find((node: { type: string }) => node.type === "block");
	expect(block.fields.title).toBe("Untitled");
	expect(block.fields.note).toBe("");
	await page.reload();
	await body
		.locator(".ridu-richtext-embedded-card")
		.first()
		.getByRole("button", { name: "Edit", exact: true })
		.click();
	const edit = page.getByRole("dialog", { name: "Edit Card", exact: true });
	await expect(edit.locator('input[name$=".title"]')).toHaveValue("Untitled");
	await expect(edit.locator('input[name$=".note"]')).toHaveValue("");
	await edit.getByRole("button", { name: "Cancel", exact: true }).click();
	expect(errors.pageErrors).toEqual([]);
});

test("relationship quick-create accepts an omitted dynamic-default heading", async ({ page }) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/dynamic-defaults", {
		data: { title: "Parent" },
	});
	expect(created.ok(), await created.text()).toBe(true);
	const parent = (await created.json()).doc;
	await page.goto(`/admin/collections/dynamic-defaults/${parent.id}`);
	await page
		.locator('[data-field-path="related"]')
		.first()
		.getByRole("button", { name: "Browse default examples" })
		.click();
	const select = page.getByRole("dialog", { name: "Select related", exact: true });
	await select.getByRole("button", { name: "Create default example", exact: true }).click();
	const drawer = page.getByRole("dialog", { name: "New default example", exact: true });
	const title = drawer.getByPlaceholder("Untitled default example");
	await expect(title).toHaveValue("");
	await expect(title).not.toHaveAttribute("required");
	const saved = page.waitForResponse(
		(result) =>
			result.request().method() === "POST" &&
			new URL(result.url()).pathname === "/api/collections/dynamic-defaults"
	);
	await title.press("Enter");
	const response = await saved;
	expect(response.ok(), await response.text()).toBe(true);
	expect((await response.json()).doc.title).toBe("Untitled");
	await expect(select.getByRole("radio", { name: /Untitled/ })).toBeChecked();
	await select.getByRole("button", { name: "Select", exact: true }).click();
	const parentSaved = await save(page, parent.id);
	expect(parentSaved.ok(), await parentSaved.text()).toBe(true);
});
