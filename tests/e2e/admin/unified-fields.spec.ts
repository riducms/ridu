import { expect, test, type Page } from "./fixture";
import {
	chooseContentLocale,
	documentSaveButton,
	loginAsEditor,
	observePageErrors,
} from "./helpers";

async function capture(page: Page, path: string) {
	await page.evaluate(() => {
		window.addEventListener(
			"ridu-test-editor-capture",
			(event) => {
				(window as unknown as { unifiedEditor: unknown }).unifiedEditor = (
					event as CustomEvent
				).detail;
			},
			{ once: true }
		);
	});
	await page
		.locator(`[data-field-path="${path}"]`)
		.getByRole("button", { name: "Capture editor" })
		.click();
}
async function writeCaptured(page: Page, value: string) {
	return page.evaluate((value) => {
		try {
			(window as unknown as { unifiedEditor: { set(value: string): void } }).unifiedEditor.set(
				value
			);
			return "written";
		} catch (error) {
			return String(error);
		}
	}, value);
}
async function save(page: Page, id: string) {
	const response = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/unified-articles/${id}`
	);
	await documentSaveButton(page).click();
	return response;
}

test("unified fields preserve generated editor settings, nested issue identities and hook behavior through the real admin", async ({
	page,
}) => {
	test.setTimeout(60_000);
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const response = await page.request.post("/api/collections/unified-articles", {
		data: {
			title: "Unified ordinary fields",
			sku: " sku-root ",
			accent: "Root",
			localizedAccent: "English",
			meta: { description: "Group", accent: "Group accent" },
			sections: [
				{
					label: "First section",
					sku: "SKU-FIRST",
					accent: "First accent",
					products: [{ label: "Product A", sku: "SKU-A", accent: "Product accent" }],
				},
				{ label: "Second section", sku: "SKU-SECOND", accent: "Second accent" },
			],
			content: [{ blockType: "card", label: "Card one", accent: "Block accent" }],
		},
	});
	expect(response.ok(), await response.text()).toBe(true);
	const original = (await response.json()).doc;
	expect(original.sku).toBe("SKU-ROOT");
	await page.goto(`/admin/collections/unified-articles/${original.id}`);
	for (const [path, value] of [
		["accent", "Root"],
		["meta.accent", "Group accent"],
		["sections.0.accent", "First accent"],
		["sections.0.products.0.accent", "Product accent"],
		["content.0.accent", "Block accent"],
		["localizedAccent", "English"],
	]) {
		const input = page.locator(`input[name="${path}"]`);
		await expect(input).toHaveAttribute("data-local-editor", "text");
		await expect(input).toHaveValue(value!);
	}
	await expect(page.locator('input[name="privateNote"]')).toHaveCount(0);
	await expect(page.locator('input[name="presentationHidden"]')).toHaveCount(0);
	await capture(page, "sections.0.accent");
	await page.locator('input[name="sections.0.products.0.sku"]').fill("invalid");
	const invalid = await save(page, original.id);
	expect(invalid.status()).toBe(422);
	const issue = (await invalid.json()).error.issues.find(
		(issue: { code: string }) => issue.code === "sku"
	);
	expect(issue.path).toBe("sections.0.products.0.sku");
	expect(JSON.parse(issue.target)).toContain(original.sections[0]._key);
	expect(JSON.parse(issue.target)).toContain(original.sections[0].products[0]._key);
	await expect(page.locator('input[name="sections.0.products.0.sku"]')).toHaveAttribute(
		"aria-invalid",
		"true"
	);
	await page
		.locator('[data-field-path="sections"]')
		.getByRole("button", { name: "Open Row 1 actions", exact: true })
		.first()
		.click();
	await page.getByRole("menuitem", { name: "Move down", exact: true }).click();
	expect(await writeCaptured(page, "Moved accent")).toBe("written");
	await expect(page.locator('input[name="sections.1.accent"]')).toHaveValue("Moved accent");
	await expect(page.locator('input[name="sections.1.products.0.sku"]')).toHaveAttribute(
		"aria-invalid",
		"true"
	);
	await expect(page.locator(`[data-unified-row-key="${original.sections[0]._key}"]`)).toContainText(
		"Row 2"
	);
	await page.locator('input[name="sections.1.products.0.sku"]').fill(" sku-fixed ");
	expect((await save(page, original.id)).ok()).toBe(true);
	expect(await writeCaptured(page, "Stale save")).toContain("stale");
	const stored = (
		await (await page.request.get(`/api/collections/unified-articles/${original.id}`)).json()
	).doc;
	expect(stored.sections[1]._key).toBe(original.sections[0]._key);
	expect(stored.sections[1].products[0].sku).toBe("SKU-FIXED");
	await capture(page, "localizedAccent");
	await chooseContentLocale(page, "French", "fr");
	expect(await writeCaptured(page, "Wrong locale")).toContain("stale");
	await expect(page.locator('input[name="localizedAccent"]')).toHaveValue("English");
	await page.locator('input[name="localizedAccent"]').fill("Français");
	expect((await save(page, original.id)).ok()).toBe(true);
	expect(errors.pageErrors).toEqual([]);
});

test("unified rich-text fields host local editors and labels with authoritative save issues and Apply/Cancel", async ({
	page,
}) => {
	test.setTimeout(60_000);
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const response = await page.request.post("/api/collections/unified-articles", {
		data: {
			title: "Unified embedded fields",
			body: {
				version: 1,
				root: {
					type: "root",
					version: 1,
					children: [
						{
							type: "block",
							version: 1,
							fields: {
								blockType: "card",
								label: "Embedded card",
								sku: "SKU-CARD",
								accent: "Original",
								products: [
									{ label: "Nested product", sku: "SKU-PRODUCT", accent: "Nested original" },
								],
							},
						},
					],
				},
			},
		},
	});
	expect(response.ok(), await response.text()).toBe(true);
	const original = (await response.json()).doc;
	await page.goto(`/admin/collections/unified-articles/${original.id}`);
	const card = page.locator('[data-field-path="body"] .ridu-richtext-embedded-card').first();
	await card.getByRole("button", { name: "Edit", exact: true }).click();
	let drawer = page.getByRole("dialog", { name: "Edit Card", exact: true });
	const accent = drawer.locator('input[name$=".accent"]').first();
	await expect(accent).toHaveAttribute("data-local-editor", "text");
	await expect(drawer.locator('input[name$=".label"]').first()).toHaveAttribute(
		"data-local-editor",
		"text"
	);
	await expect(drawer.locator('input[name$=".products.0.label"]')).toHaveAttribute(
		"data-local-editor",
		"text"
	);
	await drawer.locator('input[name$=".products.0.label"]').fill("Discarded label");
	await accent.fill("Discarded");
	await drawer.getByRole("button", { name: "Cancel", exact: true }).click();
	await card.getByRole("button", { name: "Edit", exact: true }).click();
	drawer = page.getByRole("dialog", { name: "Edit Card", exact: true });
	await expect(drawer.locator('input[name$=".accent"]').first()).toHaveValue("Original");
	await expect(drawer.locator('input[name$=".products.0.label"]')).toHaveValue("Nested product");
	await drawer.locator('input[name$=".accent"]').first().fill("Applied");
	await drawer.locator('input[name$=".products.0.sku"]').fill("invalid");
	await expect(drawer.locator("[data-unified-row-key]")).toContainText("Nested product");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	const failed = await save(page, original.id);
	expect(failed.status()).toBe(422);
	const issues = (await failed.json()).error.issues;
	expect(issues).toEqual(
		expect.arrayContaining([
			expect.objectContaining({
				code: "sku",
				path: "body.root.children.0.fields.products.0.sku",
				target: expect.any(String),
			}),
		])
	);
	// Error focusing may open the containing drawer automatically.
	if (!(await drawer.isVisible()))
		await card.getByRole("button", { name: "Edit", exact: true }).click();
	await expect(drawer.locator('input[name$=".products.0.sku"]')).toHaveAttribute(
		"aria-invalid",
		"true"
	);
	await expect(drawer.locator('input[name$=".accent"]').first()).toHaveValue("Applied");
	await drawer.locator('input[name$=".products.0.label"]').fill("Applied label");
	await drawer.locator('input[name$=".products.0.sku"]').fill(" sku-fixed ");
	await drawer.getByRole("button", { name: "Apply", exact: true }).click();
	expect((await save(page, original.id)).ok()).toBe(true);
	await page.reload();
	await card.getByRole("button", { name: "Edit", exact: true }).click();
	drawer = page.getByRole("dialog", { name: "Edit Card", exact: true });
	await expect(drawer.locator('input[name$=".accent"]').first()).toHaveValue("Applied");
	await expect(drawer.locator('input[name$=".products.0.sku"]')).toHaveValue("SKU-FIXED");
	await expect(drawer.locator('input[name$=".products.0.label"]')).toHaveValue("Applied label");
	await drawer.getByRole("button", { name: "Cancel", exact: true }).click();
	expect(errors.pageErrors).toEqual([]);
});
