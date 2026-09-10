import type { SchemaField } from "@riducms/protocol";
import { expect, test } from "./fixture";
import { documentSaveButton, loginAsEditor, observePageErrors } from "./helpers";

test("API keyless blocks survive admin editing, reorder, nested duplication and save", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/pages?draft=true", {
		data: {
			title: "Block identity round trip",
			layout: [
				{
					blockType: "hero",
					heading: "First hero",
					links: [{ label: "First link" }, { label: "Second link" }],
				},
				{ blockType: "hero", heading: "Second hero" },
			],
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	expect(original.layout[0]._key).toEqual(expect.any(String));
	expect(original.layout[1]._key).toEqual(expect.any(String));
	expect(original.layout[0]._key).not.toBe(original.layout[1]._key);
	const originalLinks = original.layout[0].links;
	expect(originalLinks[0]._key).toEqual(expect.any(String));
	expect(originalLinks[1]._key).toEqual(expect.any(String));
	expect(originalLinks[0]._key).not.toBe(originalLinks[1]._key);
	await page.goto(`/admin/collections/pages/${original.id}`);
	const layout = page.locator('[data-field-path="layout"]').first();
	await expect(layout.getByText("First hero", { exact: true })).toBeVisible();
	await page.locator('input[name="layout.0.heading"]').fill("First hero edited");
	await page.locator('input[name="layout.0.links.0.label"]').fill("First link edited");
	const links = page.locator('[data-field-path="layout.0.links"]').first();
	await links.getByRole("button", { name: "Open Row 1 actions", exact: true }).click();
	await page.getByRole("menuitem", { name: "Move down", exact: true }).click();
	await expect(page.locator('input[name="layout.0.links.1.label"]')).toHaveValue(
		"First link edited"
	);
	await layout.getByRole("button", { name: "Open First hero edited actions", exact: true }).click();
	await page.getByRole("menuitem", { name: "Move down", exact: true }).click();
	await expect(page.locator('input[name="layout.1.heading"]')).toHaveValue("First hero edited");
	await expect(page.locator('input[name="layout.1.links.1.label"]')).toHaveValue(
		"First link edited"
	);
	await layout.getByRole("button", { name: "Open First hero edited actions", exact: true }).click();
	await page.getByRole("menuitem", { name: "Duplicate", exact: true }).click();
	await expect(page.locator('input[name="layout.2.links.1.label"]')).toHaveValue(
		"First link edited"
	);
	await page.locator('input[name="layout.2.heading"]').fill("Copied hero");
	const saved = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/pages/${original.id}`
	);
	await documentSaveButton(page).click();
	expect((await saved).ok()).toBe(true);
	const stored = (await (await page.request.get(`/api/collections/pages/${original.id}`)).json())
		.doc;
	expect(stored.layout.map((row: { heading: string }) => row.heading)).toEqual([
		"Second hero",
		"First hero edited",
		"Copied hero",
	]);
	expect(stored.layout[0]._key).toBe(original.layout[1]._key);
	expect(stored.layout[1]._key).toBe(original.layout[0]._key);
	expect(stored.layout[2]._key).not.toBe(stored.layout[1]._key);
	expect(stored.layout[2].links[0]._key).not.toBe(stored.layout[1].links[0]._key);
	expect(stored.layout[1].links).toEqual([
		originalLinks[1],
		{ ...originalLinks[0], label: "First link edited" },
	]);
	expect(stored.layout[2].links[1]._key).not.toBe(stored.layout[1].links[1]._key);
	await page.reload();
	await expect(page.locator('input[name="layout.1.heading"]')).toHaveValue("First hero edited");
	await expect(page.locator('input[name="layout.1.links.1.label"]')).toHaveValue(
		"First link edited"
	);
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("reordering preserves an unsaved edit beside a protected block heading", async ({ page }) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/pages?draft=true", {
		data: {
			title: "Mixed block permissions",
			layout: [
				{ blockType: "hero", heading: "Original A" },
				{ blockType: "hero", heading: "Protected B", headingLocked: true },
			],
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/pages/${original.id}`);
	await expect(page.locator('input[name="layout.0.heading"]')).toBeEditable();
	await expect(page.locator('input[name="layout.1.heading"]')).not.toBeEditable();
	await page.locator('input[name="layout.0.heading"]').fill("Edited A");
	const layout = page.locator('[data-field-path="layout"]').first();
	await layout.getByRole("button", { name: "Open Edited A actions", exact: true }).click();
	await page.getByRole("menuitem", { name: "Move down", exact: true }).click();
	await expect(page.locator('input[name="layout.1.heading"]')).toBeEditable();
	await expect(page.locator('input[name="layout.1.heading"]')).toHaveValue("Edited A");
	await expect(page.locator('input[name="layout.0.heading"]')).not.toBeEditable();
	const saved = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/pages/${original.id}`
	);
	await documentSaveButton(page).click();
	const response = await saved;
	expect(response.ok(), await response.text()).toBe(true);
	const submitted = response.request().postDataJSON().layout;
	expect(submitted[1]).toMatchObject({ _key: original.layout[0]._key, heading: "Edited A" });
	expect(submitted[0]._key).toBe(original.layout[1]._key);
	expect(submitted[0]).not.toHaveProperty("heading");
	await page.reload();
	await expect(page.locator('input[name="layout.1.heading"]')).toHaveValue("Edited A");
	await expect(page.locator('input[name="layout.0.heading"]')).toHaveValue("Protected B");
	await expect(page.locator('input[name="layout.0.heading"]')).not.toBeEditable();
	const denied = await page.request.patch(`/api/collections/pages/${original.id}`, {
		data: {
			layout: [
				{ ...original.layout[1], heading: "Unauthorized B" },
				{ ...original.layout[0], heading: "Edited A" },
			],
		},
	});
	expect(denied.status()).toBe(403);
	const stored = (await (await page.request.get(`/api/collections/pages/${original.id}`)).json())
		.doc;
	expect(stored.layout.map((row: { heading: string }) => row.heading)).toEqual([
		"Protected B",
		"Edited A",
	]);
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("block insertion chooses a variant at the intended position and reveals collapsed errors", async ({
	page,
}) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/pages?draft=true", {
		data: { title: "Block insertion", layout: [{ blockType: "hero", heading: "Original hero" }] },
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/pages/${original.id}`);
	const layout = page.locator('[data-field-path="layout"]').first();
	await layout.getByRole("button", { name: "Open Original hero actions", exact: true }).click();
	await page.getByRole("menuitem", { name: "Add below", exact: true }).click();
	const picker = page.getByRole("dialog");
	await picker.getByRole("button", { name: "Rich Text", exact: true }).click();
	await expect(page.locator('[data-field-path="layout.1.body"]')).toBeVisible();
	await layout
		.getByRole("button", { name: "Add block", exact: true })
		.filter({ hasText: "Add block" })
		.click();
	await page.getByRole("dialog").getByRole("button", { name: "Hero", exact: true }).click();
	await layout.getByRole("button", { name: "Collapse all", exact: true }).click();
	await documentSaveButton(page).click();
	const summary = layout.getByRole("button", { name: /validation errors:.*Heading/i });
	await expect(summary).toBeVisible();
	await summary.click();
	await expect(page.locator('input[name="layout.2.heading"]')).toBeFocused();
	await page.locator('input[name="layout.2.heading"]').fill("Final hero");
	await layout
		.getByRole("button", { name: "Add block", exact: true })
		.filter({ hasText: "Add block" })
		.click();
	await page.getByRole("dialog").getByRole("button", { name: "Hero", exact: true }).click();
	await expect(
		layout.getByRole("button", { name: "Add block", exact: true }).filter({ hasText: "Add block" })
	).toBeDisabled();
});

test("block errors reveal invalid fields inside collapsed sections", async ({ page }) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/pages?draft=true", {
		data: { title: "Nested disclosure", layout: [{ blockType: "hero", heading: "Original" }] },
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	// Exercise supported presentation metadata without changing the stored field schema.
	await page.route("**/api/schema", async (route) => {
		const response = await route.fetch();
		const body = (await response.json()) as {
			schema: { collections: { slug: string; fields: SchemaField[] }[] };
		};
		const heading = body.schema.collections
			.find((collection) => collection.slug === "pages")
			?.fields.find((field) => field.name === "layout")
			?.blocks?.types?.find((block) => block.slug === "hero")
			?.fields.find((field) => field.name === "heading");
		if (heading === undefined) throw new Error("Hero heading fixture is missing");
		heading.admin.collapsible = {
			id: "hero-details",
			label: "Hero details",
			initiallyCollapsed: true,
		};
		await route.fulfill({ response, json: body });
	});
	await page.goto(`/admin/collections/pages/${original.id}`);
	const layout = page.locator('[data-field-path="layout"]').first();
	const section = layout.locator("details[data-field-collapsible]");
	const heading = page.locator('input[name="layout.0.heading"]');
	await expect(section).not.toHaveAttribute("open");
	await section.locator("summary").click();
	await heading.fill("");
	await section.locator("summary").click();
	await layout.getByRole("button", { name: "Collapse all", exact: true }).click();
	await documentSaveButton(page).click();
	await layout.getByRole("button", { name: /validation errors:.*Heading/i }).click();
	await expect(section).toHaveAttribute("open");
	await expect(heading).toBeFocused();
	await heading.fill("Corrected heading");
	const saved = page.waitForResponse(
		(response) => response.request().method() === "PATCH" && response.url().includes(original.id)
	);
	await documentSaveButton(page).click();
	expect((await saved).ok()).toBe(true);
	await page.reload();
	await section.locator("summary").click();
	await expect(heading).toHaveValue("Corrected heading");
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("removed block schemas preserve content and prevent destructive admin saving", async ({
	page,
}) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/pages?draft=true", {
		data: {
			title: "Recover removed schema",
			layout: [{ blockType: "hero", heading: "Keep this stored content" }],
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.route("**/api/schema", async (route) => {
		const response = await route.fetch();
		const body = (await response.json()) as {
			schema: { collections: { slug: string; fields: SchemaField[] }[] };
		};
		const layout = body.schema.collections
			.find((collection) => collection.slug === "pages")
			?.fields.find((field) => field.name === "layout");
		if (layout?.blocks)
			layout.blocks.types = layout.blocks.types!.filter((block) => block.slug !== "hero");
		await route.fulfill({ response, json: body });
	});
	let updates = 0;
	page.on("request", (request) => {
		if (
			request.method() === "PATCH" &&
			new URL(request.url()).pathname === `/api/collections/pages/${original.id}`
		)
			updates++;
	});
	await page.goto(`/admin/collections/pages/${original.id}`);
	await expect(
		page.getByRole("alert").filter({ hasText: "Its content is preserved" })
	).toBeVisible();
	await page.locator('input[name="title"]').fill("Other edit must not drop blocks");
	await documentSaveButton(page).click();
	await expect(
		page
			.getByText(
				"This document contains a block type that is no longer configured. Its content is preserved. Restore the block schema or migrate the document before saving."
			)
			.first()
	).toBeVisible();
	expect(updates).toBe(0);
	const stored = (await (await page.request.get(`/api/collections/pages/${original.id}`)).json())
		.doc;
	expect(stored.layout).toEqual(original.layout);
	expect(stored.title).toBe(original.title);
});

test("server schema removal rejects reads and destructive updates until recovery", async ({
	page,
}) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/pages?draft=true", {
		data: {
			title: "Actual server schema recovery",
			layout: [{ blockType: "hero", heading: "Undeclared payload must stay private" }],
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	const unauthorized = await page.request.post("/__ridu-test/blocks-schema", {
		data: { retireHero: true },
	});
	expect(unauthorized.status()).toBe(401);
	const schemaHeaders = { "X-Ridu-Test-Reset-Token": process.env.RIDU_BROWSER_RESET_TOKEN! };
	const removed = await page.request.post("/__ridu-test/blocks-schema", {
		headers: schemaHeaders,
		data: { retireHero: true },
	});
	expect(removed.status(), await removed.text()).toBe(204);
	const schema = (await (await page.request.get("/api/schema")).json()).schema;
	const layout = schema.collections
		.find((collection: { slug: string }) => collection.slug === "pages")
		.fields.find((field: SchemaField) => field.name === "layout");
	expect(layout.blocks.types.some((block: { slug: string }) => block.slug === "hero")).toBe(false);
	for (const response of [
		await page.request.get(`/api/collections/pages/${original.id}`),
		await page.request.patch(`/api/collections/pages/${original.id}`, { data: { layout: [] } }),
	]) {
		expect(response.status()).toBe(409);
		const text = await response.text();
		expect(JSON.parse(text).error.code).toBe("conflict");
		expect(text).toContain("unknown_block_schema");
		expect(text).toContain("layout.0.blockType");
		expect(text).not.toContain("Undeclared payload must stay private");
		expect(text).not.toContain('"doc":');
	}
	await page.goto(`/admin/collections/pages/${original.id}`);
	await expect(
		page.getByText(/restore.*block schema|block.*recovery|block.*no longer configured/i).first()
	).toBeVisible();
	await expect(page.locator('input[name="title"]')).toHaveCount(0);
	await expect(page.getByText("Undeclared payload must stay private", { exact: true })).toHaveCount(
		0
	);
	const restored = await page.request.post("/__ridu-test/blocks-schema", {
		headers: schemaHeaders,
		data: { retireHero: false },
	});
	expect(restored.status(), await restored.text()).toBe(204);
	const read = await page.request.get(`/api/collections/pages/${original.id}`);
	expect(read.ok(), await read.text()).toBe(true);
	const stored = (await read.json()).doc;
	expect(stored.layout).toEqual(original.layout);
	expect(stored.title).toBe(original.title);
	expect(stored._revision).toEqual(expect.any(Number));
	expect(stored._revision).toBe(original._revision);
	await page.reload();
	await expect(page.locator('input[name="layout.0.heading"]')).toHaveValue(
		"Undeclared payload must stay private"
	);
});
