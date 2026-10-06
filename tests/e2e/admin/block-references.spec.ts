import { expect, test } from "./fixture";
import { documentSaveButton, loginAsEditor, observePageErrors } from "./helpers";
import { bodyCards, bodyEditor, insertBlock } from "./rich-text-block-fixture";

for (const mode of ["inline", "reference"] as const) {
	test(`${mode} blocks preserve document-dependent access, refresh, denied saves and inline embeds`, async ({
		page,
	}) => {
		test.setTimeout(90_000);
		await loginAsEditor(page);
		const errors = observePageErrors(page);
		const collection = `${mode}-pages`;
		const response = await page.request.post(`/api/collections/${collection}`, {
			data: {
				tenant: "open",
				layout: [
					{
						_key: "a",
						blockType: "registry-card",
						visibility: "visible",
						secret: "Readable",
						controlled: "Editable",
					},
					{
						_key: "b",
						blockType: "registry-card",
						visibility: "hidden",
						secret: "Redacted",
						controlled: "Protected",
					},
				],
			},
		});
		expect(response.ok(), await response.text()).toBe(true);
		const original = (await response.json()).doc;
		expect(original.layout[0].secret).toBe("Readable");
		expect(original.layout[1].secret).toBeUndefined();
		await page.goto(`/admin/collections/${collection}/${original.id}`);
		await expect(page.locator('input[name="layout.0.controlled"]')).toBeEditable();
		await expect(page.locator('input[name="layout.1.controlled"]')).not.toBeEditable();
		await expect(page.locator('input[name="layout.1.secret"]')).toHaveCount(0);
		await page.locator('input[name="layout.0.controlled"]').fill("Edited");
		await page.locator('input[name="layout.1.visibility"]').fill("visible");
		let saved = page.waitForResponse(
			(r) =>
				r.request().method() === "PATCH" &&
				new URL(r.url()).pathname === `/api/collections/${collection}/${original.id}`
		);
		await documentSaveButton(page).click();
		expect((await saved).ok()).toBe(true);
		await expect(page.locator('input[name="layout.1.controlled"]')).toBeEditable();
		await expect(page.locator('input[name="layout.1.secret"]')).toHaveValue("Redacted");
		const inserted = await insertBlock(page, bodyEditor(page), "Card");
		await inserted.getByRole("textbox", { name: "Visibility", exact: true }).fill("visible");
		await inserted.getByRole("button", { name: "Remove", exact: true }).click();
		await expect(bodyCards(page)).toHaveCount(0);
		const replacement = await insertBlock(page, bodyEditor(page), "Card");
		await replacement.getByRole("textbox", { name: "Visibility", exact: true }).fill("visible");
		await expect(bodyCards(page)).toHaveCount(1);
		saved = page.waitForResponse(
			(r) =>
				r.request().method() === "PATCH" &&
				new URL(r.url()).pathname === `/api/collections/${collection}/${original.id}`
		);
		await documentSaveButton(page).click();
		expect((await saved).ok()).toBe(true);
		await page.locator('input[name="layout.0.controlled"]').fill("invalid");
		const layout = page.locator('[data-field-path="layout"]').first();
		await layout.getByRole("button", { name: "Collapse all", exact: true }).click();
		saved = page.waitForResponse(
			(r) =>
				r.request().method() === "PATCH" &&
				new URL(r.url()).pathname === `/api/collections/${collection}/${original.id}`
		);
		await documentSaveButton(page).click();
		const invalid = await saved;
		expect(invalid.status()).toBe(422);
		const issue = (await invalid.json()).error.issues.find(
			(issue: { code: string }) => issue.code === "controlled_value"
		);
		expect(issue.path).toBe("layout.0.controlled");
		expect(JSON.parse(issue.target)).toEqual(expect.arrayContaining(["registry-card", "a"]));
		await layout.getByRole("button", { name: /1 Error:.*Controlled/i }).click();
		await expect(page.locator('input[name="layout.0.controlled"]')).toBeFocused();
		// Server admission uses the candidate root even when the client presents an editable control.
		await page.route(
			(url) => url.pathname === `/api/collections/${collection}/${original.id}`,
			async (route) => {
				if (route.request().method() !== "PATCH") return route.continue();
				await route.continue({
					postData: JSON.stringify({ ...route.request().postDataJSON(), tenant: "closed" }),
				});
			}
		);
		await page.locator('input[name="layout.0.controlled"]').fill("Rejected");
		saved = page.waitForResponse(
			(r) =>
				r.request().method() === "PATCH" &&
				new URL(r.url()).pathname === `/api/collections/${collection}/${original.id}`
		);
		await documentSaveButton(page).click();
		expect((await saved).status()).toBe(403);
		expect(errors.pageErrors).toEqual([]);
		expect(
			errors.consoleErrors.filter((message) => !message.includes("403") && !message.includes("422"))
		).toEqual([]);
	});
}

test("collection filters reach registered block fields through nested block paths", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const marker = `filter-${Date.now().toString(36)}`;
	for (const [suffix, caption, note] of [
		["alpha", "Alpha caption", "Deep alpha"],
		["beta", "Beta caption", "Deep beta"],
	] as const) {
		const response = await page.request.post("/api/collections/reference-pages", {
			data: {
				tenant: `${marker}-${suffix}`,
				layout: [
					{
						_key: "card",
						blockType: "registry-card",
						visibility: "visible",
						details: { caption },
						children: [{ _key: "note", blockType: "registry-note", text: note }],
					},
				],
			},
		});
		expect(response.ok(), await response.text()).toBe(true);
	}
	const alpha = page.getByText(`${marker}-alpha`, { exact: true });
	const beta = page.getByText(`${marker}-beta`, { exact: true });
	const urlFilters = () => JSON.parse(new URL(page.url()).searchParams.get("filters") ?? "[]");

	await page.goto("/admin/collections/reference-pages");
	await expect(alpha).toBeVisible();
	await page.getByRole("button", { name: /Filters/ }).click();
	await page.getByRole("button", { name: "Add filter", exact: true }).click();
	const field = page.getByLabel("Filter field", { exact: true });
	await field.click();
	await page.getByRole("option", { name: "Layout Blocks", exact: true }).click();
	await page.getByRole("option", { name: "Card Block", exact: true }).click();
	await expect(page.getByRole("option", { name: "Children Blocks", exact: true })).toBeVisible();
	// The read-protected definition field is not queryable at any placement.
	await expect(page.getByRole("option", { name: "Secret", exact: true })).toHaveCount(0);
	await page.getByRole("option", { name: "Details Group", exact: true }).click();
	await page.getByRole("option", { name: "Caption", exact: true }).click();
	await expect(field).toContainText("Layout > Card > Details > Caption");
	await page.getByLabel("Filter value", { exact: true }).fill("Alpha caption");
	await expect.poll(urlFilters).toEqual([
		[
			{
				field: "layout.registry-card.details.caption",
				operator: "equals",
				value: "Alpha caption",
			},
		],
	]);
	await expect(beta).toHaveCount(0);
	await expect(alpha).toBeVisible();

	// Search from the block's level reaches the nested definition at its concrete path.
	await field.click();
	await page.getByRole("button", { name: "Back to Layout > Card", exact: true }).click();
	await page.getByRole("combobox", { name: "Search fields", exact: true }).fill("text");
	await page.getByRole("option", { name: "Children > Note > Text", exact: true }).click();
	await expect(field).toContainText("Layout > Card > Children > Note > Text");
	await page.getByLabel("Filter value", { exact: true }).fill("Deep beta");
	await expect.poll(urlFilters).toEqual([
		[
			{
				field: "layout.registry-card.children.registry-note.text",
				operator: "equals",
				value: "Deep beta",
			},
		],
	]);
	await expect(alpha).toHaveCount(0);
	await expect(beta).toBeVisible();

	await page.reload();
	await expect(beta).toBeVisible();
	await expect(alpha).toHaveCount(0);
	await page.getByRole("button", { name: /Filters/ }).click();
	await expect(page.getByLabel("Filter field", { exact: true })).toContainText(
		"Layout > Card > Children > Note > Text"
	);
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("schema response shares the registered definition across collections and rich text", async ({
	page,
}) => {
	await loginAsEditor(page);
	const { schema } = await (await page.request.get("/api/schema")).json();
	expect(schema.blocks.filter((b: { slug: string }) => b.slug === "registry-card")).toHaveLength(1);
	const pages = schema.collections.find((c: { slug: string }) => c.slug === "reference-pages");
	expect(pages.fields.find((f: { name: string }) => f.name === "layout").blocks).toEqual({
		blockReferences: ["registry-card"],
	});
	expect(
		pages.fields.find((f: { name: string }) => f.name === "body").plugin.embeddedTrees[0].cases[0]
			.blockReferences
	).toEqual(["registry-card"]);
});

for (const mode of ["inline", "reference"] as const) {
	test(`${mode} block pickers and rich text use generated singular labels and explicit overrides`, async ({
		page,
	}) => {
		await loginAsEditor(page);
		const errors = observePageErrors(page);
		const collection = `${mode}-block-labels`;
		const response = await page.request.post(`/api/collections/${collection}`, { data: {} });
		expect(response.ok(), await response.text()).toBe(true);
		const original = (await response.json()).doc;
		await page.goto(`/admin/collections/${collection}/${original.id}`);
		const layout = page.locator('[data-field-path="layout"]').first();
		await layout
			.getByRole("button", { name: "Add block", exact: true })
			.filter({ hasText: "Add block" })
			.click();
		const picker = page.getByRole("dialog");
		await expect(picker.getByRole("button", { name: "CTA", exact: true })).toBeVisible();
		await picker.getByRole("button", { name: "Person", exact: true }).click();
		await expect(layout.locator(".ridu-repeated-row__type").first()).toHaveText("Person");
		await expect(layout.locator('input[name="layout.0.blockName"]')).toHaveValue("");
		await expect(layout.locator('input[name="layout.0.blockName"]')).toHaveAttribute(
			"placeholder",
			"Untitled"
		);
		for (const label of ["Person", "CTA"]) {
			await insertBlock(page, bodyEditor(page), label);
			await expect(bodyCards(page).filter({ hasText: label })).toBeVisible();
		}
		const saved = page.waitForResponse(
			(r) =>
				r.request().method() === "PATCH" &&
				new URL(r.url()).pathname === `/api/collections/${collection}/${original.id}`
		);
		await documentSaveButton(page).click();
		const result = await saved;
		expect(result.ok(), await result.text()).toBe(true);
		const doc = (await result.json()).doc;
		expect(doc.layout[0].blockType).toBe("people");
		expect(doc.layout[0]._key).toEqual(expect.any(String));
		expect(
			doc.body.root.children
				.filter((node: { type: string }) => node.type === "block")
				.map((node: { fields: { blockType: string } }) => node.fields.blockType)
		).toEqual(["people", "promotion"]);
		expect(errors.pageErrors).toEqual([]);
		expect(errors.consoleErrors).toEqual([]);
	});
}
