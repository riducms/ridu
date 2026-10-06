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
	// The create response precedes the transition into the saved document.
	// fill() can write into inert outgoing DOM, unlike an actual user.
	await expect(page).toHaveURL((url) => url.pathname.endsWith(`/${original.id}`));
	await expect(page.locator("main")).not.toHaveAttribute("inert", "");
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
	await picker.getByRole("option", { name: "Card", exact: true }).click();
	const card = body
		.locator('.ridu-richtext-embedded > article[data-block-type="default-embedded-card"]')
		.first();
	await expect(card.locator('input[name$=".title"]')).toHaveValue("");
	await expect(card.locator('input[name$=".note"]')).toHaveValue("");
	await card.locator('input[name$=".note"]').fill("Draft");
	await card.locator('input[name$=".note"]').fill("");
	const saved = await save(page, original.id);
	expect(saved.ok(), await saved.text()).toBe(true);
	const document = (await saved.json()).doc;
	const block = document.body.root.children.find((node: { type: string }) => node.type === "block");
	expect(block.fields.title).toBe("Untitled");
	expect(block.fields.note).toBe("");
	await page.reload();
	await expect(card.locator('input[name$=".title"]')).toHaveValue("Untitled");
	await expect(card.locator('input[name$=".note"]')).toHaveValue("");
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
		.getByRole("combobox", { name: "Related", exact: true })
		.click();
	await page.getByRole("button", { name: /^Browse all default examples/ }).click();
	const select = page.getByRole("dialog", { name: "Select related", exact: true });
	await select.getByRole("button", { name: "Create New", exact: true }).click();
	const drawer = page.getByRole("dialog", { name: "New default example", exact: true });
	const title = drawer.getByLabel("Title", { exact: true });
	await expect(title).toHaveValue("");
	await expect(title).not.toHaveAttribute("required");
	const saved = page.waitForResponse(
		(result) =>
			result.request().method() === "POST" &&
			new URL(result.url()).pathname === "/api/collections/dynamic-defaults"
	);
	await drawer.getByRole("button", { name: "Save", exact: true }).click();
	const response = await saved;
	expect(response.ok(), await response.text()).toBe(true);
	expect(response.request().postDataJSON()).not.toHaveProperty("title");
	expect((await response.json()).doc.title).toBe("Untitled");
	const createdRow = select.getByRole("table", { name: "Results" }).getByRole("row").filter({
		hasText: "Untitled",
	});
	await expect(createdRow).toHaveAttribute("data-selected", "true");
	await createdRow.getByRole("button", { name: "Untitled", exact: true }).click();
	await expect(select).toBeHidden();
	const parentSaved = await save(page, parent.id);
	expect(parentSaved.ok(), await parentSaved.text()).toBe(true);
});
