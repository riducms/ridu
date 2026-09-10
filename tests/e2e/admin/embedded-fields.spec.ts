import { expect, test } from "./fixture";
import { documentSaveButton, loginAsEditor, observePageErrors } from "./helpers";

test("a public non-Lexical plugin hosts ordinary fields through the parent form", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/outlines", {
		data: {
			title: "Embedded schema contract",
			body: {
				outline: [
					{ kind: "widget", items: null, content: { schema: "card", title: "Editable" } },
					{ kind: "widget", content: { schema: "card", title: "Protected", locked: true } },
				],
			},
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	expect(original.body.outline[0].content.uid).toEqual(expect.any(String));
	await page.goto(`/admin/collections/outlines/${original.id}`);
	const editable = page.locator('input[name="body.outline.0.content.title"]');
	await expect(editable).toBeEditable();
	await expect(page.locator('input[name="body.outline.1.content.title"]')).not.toBeEditable();
	await expect(page.locator('input[name="body.outline.0.content.settings.caption"]')).toHaveValue(
		"Default caption"
	);
	await editable.fill("");
	await documentSaveButton(page).click();
	await expect(editable).toHaveAttribute("aria-invalid", "true");
	await expect(editable).toBeFocused();
	await editable.fill("Edited before reorder");
	await page.locator('input[name="body.outline.0.content.settings.caption"]').fill("Nested edit");
	await page.getByRole("button", { name: "Reverse outline cards", exact: true }).click();
	await expect(page.locator('input[name="body.outline.1.content.title"]')).toHaveValue(
		"Edited before reorder"
	);
	await expect(page.locator('input[name="body.outline.0.content.title"]')).not.toBeEditable();
	await expect(page.locator('input[name="body.outline.1.content.title"]')).toBeEditable();
	const saved = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/outlines/${original.id}/publish`
	);
	await documentSaveButton(page).click();
	expect((await saved).ok()).toBe(true);
	const stored = (await (await page.request.get(`/api/collections/outlines/${original.id}`)).json())
		.doc;
	expect(stored.body.outline.map((node: { content: { uid: string } }) => node.content.uid)).toEqual(
		[original.body.outline[1].content.uid, original.body.outline[0].content.uid]
	);
	expect(stored.body.outline[0].content.title).toBe("Protected");
	expect(stored.body.outline[1].content.title).toBe("Edited before reorder");
	expect(stored.body.outline[1].content.settings.caption).toBe("Nested edit");
	await page.reload();
	await expect(page.locator('input[name="body.outline.1.content.title"]')).toHaveValue(
		"Edited before reorder"
	);
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("the embedded host enforces parent denial when the plugin omits readOnly", async ({
	page,
}) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/outlines", {
		data: {
			title: "Parent protected outline",
			bodyLocked: true,
			body: {
				outline: [
					{
						kind: "widget",
						content: { schema: "card", title: "Parent protected card" },
					},
				],
			},
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/outlines/${original.id}`);
	await expect(page.locator('input[name="body.outline.0.content.title"]')).not.toBeEditable();
	await expect(
		page.locator('input[name="body.outline.0.content.settings.caption"]')
	).not.toBeEditable();
	await expect(page.getByRole("button", { name: "Add outline card", exact: true })).toBeDisabled();
	await page.locator('input[name="title"]').fill("Edited ordinary title");
	const saved = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/outlines/${original.id}/publish`
	);
	await documentSaveButton(page).click();
	expect((await saved).ok()).toBe(true);
	const stored = (await (await page.request.get(`/api/collections/outlines/${original.id}`)).json())
		.doc;
	expect(stored.title).toBe("Edited ordinary title");
	expect(stored.body).toEqual(original.body);
});

test("inserting an embedded card shows nested defaults before save and retains edits", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const response = await page.request.post("/api/collections/outlines", {
		data: { title: "Inserted outline defaults", body: { outline: [] } },
	});
	expect(response.ok(), await response.text()).toBe(true);
	const original = (await response.json()).doc;
	await page.goto(`/admin/collections/outlines/${original.id}`);
	await page.getByRole("button", { name: "Add outline card", exact: true }).click();
	const caption = page.locator('input[name="body.outline.0.content.settings.caption"]');
	await expect(caption).toHaveValue("Default caption");
	await caption.fill("Edited before first save");
	await page.getByRole("button", { name: "Add outline card", exact: true }).click();
	await page.getByRole("button", { name: "Reverse outline cards", exact: true }).click();
	await expect(caption).toHaveValue("Default caption");
	await expect(page.locator('input[name="body.outline.1.content.settings.caption"]')).toHaveValue(
		"Edited before first save"
	);
	const saved = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/outlines/${original.id}/publish`
	);
	await documentSaveButton(page).click();
	expect((await saved).ok()).toBe(true);
	await page.reload();
	await expect(caption).toHaveValue("Default caption");
	await expect(page.locator('input[name="body.outline.1.content.settings.caption"]')).toHaveValue(
		"Edited before first save"
	);
	const stored = (await (await page.request.get(`/api/collections/outlines/${original.id}`)).json())
		.doc;
	expect(
		new Set(stored.body.outline.map((node: { content: { uid: string } }) => node.content.uid)).size
	).toBe(2);
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});
