import type { ValidationIssue } from "@riducms/protocol";
import { expect, test, type Page } from "./fixture";
import {
	chooseContentLocale,
	documentSaveButton,
	loginAsEditor,
	observePageErrors,
} from "./helpers";

async function save(page: Page, id: string) {
	const response = page.waitForResponse(
		(result) =>
			result.request().method() === "PATCH" &&
			new URL(result.url()).pathname === `/api/collections/issue-targets/${id}`
	);
	await documentSaveButton(page).click();
	return response;
}

async function rowAction(page: Page, path: string, index: number, action: string) {
	await page
		.locator(`[data-field-path="${path}"]`)
		.first()
		.getByRole("button", { name: `Open Row ${index + 1} actions`, exact: true })
		.first()
		.click();
	await page.getByRole("menuitem", { name: action, exact: true }).click();
}

for (const field of ["sections", "content"] as const)
	test(`aggregate ${field} validator targets child and nested array controls through reorder and removal`, async ({
		page,
	}) => {
		await loginAsEditor(page);
		const errors = observePageErrors(page);
		const created = await page.request.post("/api/collections/issue-targets", {
			data: {
				title: "Aggregate descendants",
				[field]: [
					{
						...(field === "content" ? { blockType: "card" } : {}),
						heading: "First",
						links: [{ url: "First URL" }, { url: "Other URL" }],
					},
					{ ...(field === "content" ? { blockType: "note" } : {}), heading: "Other" },
				],
			},
		});
		expect(created.ok(), await created.text()).toBe(true);
		const original = (await created.json()).doc;
		await page.goto(`/admin/collections/issue-targets/${original.id}`);
		await page.locator(`input[name="${field}.0.heading"]`).fill("invalid");
		await page.locator(`input[name="${field}.0.links.0.url"]`).fill("invalid");
		const rejected = await save(page, original.id);
		expect(rejected.status()).toBe(422);
		const issues: ValidationIssue[] = (await rejected.json()).error.issues;
		const nested = issues.find((issue) => issue.code === "url")!;
		expect(nested.path).toBe(`${field}.0.links.0.url`);
		expect(JSON.parse(nested.target!)).toEqual(
			expect.arrayContaining([original[field][0]._key, original[field][0].links[0]._key])
		);
		if (field === "content") expect(JSON.parse(nested.target!)).toContain("card");
		await expect(page.locator(`input[name="${field}.0.heading"]`)).toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await expect(page.locator(`input[name="${field}.0.links.0.url"]`)).toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await rowAction(page, `${field}.0.links`, 0, "Move down");
		await rowAction(page, field, 0, "Move down");
		await expect(page.locator(`input[name="${field}.1.links.1.url"]`)).toHaveValue("invalid");
		await expect(page.locator(`input[name="${field}.1.links.1.url"]`)).toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await expect(page.locator(`input[name="${field}.0.heading"]`)).not.toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await rowAction(page, `${field}.1.links`, 1, "Remove");
		await expect(page.locator(`input[name="${field}.1.links.0.url"]`)).toHaveValue("Other URL");
		await expect(page.locator(`input[name="${field}.1.links.0.url"]`)).not.toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await expect(page.locator(`input[name="${field}.1.heading"]`)).toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await page.locator(`input[name="${field}.1.heading"]`).fill("Corrected");
		expect((await save(page, original.id)).ok()).toBe(true);
		await page.reload();
		await expect(page.locator(`input[name="${field}.1.heading"]`)).toHaveValue("Corrected");
		await expect(page.locator(`input[name="${field}.1.links.1.url"]`)).toHaveCount(0);
		expect(errors.pageErrors).toEqual([]);
	});

test("localized aggregate issues identify the active translation and clear after correction", async ({
	page,
}) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/issue-targets?locale=en", {
		data: {
			title: "Localized aggregate",
			localizedSections: [{ heading: "English", links: [{ url: "Original URL" }] }],
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/issue-targets/${original.id}`);
	for (const locale of ["en", "fr"] as const) {
		if (locale === "fr") await chooseContentLocale(page, "French", "fr");
		const url = page.locator('input[name="localizedSections.0.links.0.url"]');
		await expect(url).toHaveValue(locale === "en" ? "Original URL" : "English URL");
		await url.fill("invalid");
		const rejected = await save(page, original.id);
		expect(rejected.status()).toBe(422);
		const issues: ValidationIssue[] = (await rejected.json()).error.issues;
		expect(issues.find((issue) => issue.code === "url")).toMatchObject({
			path: "localizedSections.0.links.0.url",
			locale,
		});
		await expect(url).toHaveAttribute("aria-invalid", "true");
		await url.fill(locale === "en" ? "English URL" : "French URL");
		expect((await save(page, original.id)).ok()).toBe(true);
		await expect(url).not.toHaveAttribute("aria-invalid", "true");
	}
	await chooseContentLocale(page, "English", "en");
	await expect(page.locator('input[name="localizedSections.0.links.0.url"]')).toHaveValue(
		"English URL"
	);
});

for (const field of ["body", "localizedBody"] as const)
	test(`aggregate embedded ${field} issue reaches its detached child editor and follows nested reorder`, async ({
		page,
	}) => {
		await loginAsEditor(page);
		const errors = observePageErrors(page);
		const created = await page.request.post("/api/collections/issue-targets?locale=en", {
			data: {
				title: "Embedded aggregate",
				[field]: {
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
									heading: "Embedded",
									links: [{ url: "Original URL" }, { url: "Other URL" }],
								},
							},
						],
					},
				},
			},
		});
		expect(created.ok(), await created.text()).toBe(true);
		const original = (await created.json()).doc;
		await page.goto(`/admin/collections/issue-targets/${original.id}`);
		if (field === "localizedBody") await chooseContentLocale(page, "French", "fr");
		const card = page.locator(`[data-field-path="${field}"] .ridu-richtext-embedded-card`).first();
		await card.getByRole("button", { name: "Edit", exact: true }).click();
		const drawer = page.getByRole("dialog", { name: "Edit Card", exact: true });
		await drawer.locator('input[name$=".links.0.url"]').fill("invalid");
		await drawer.getByRole("button", { name: "Apply", exact: true }).click();
		const rejected = await save(page, original.id);
		expect(rejected.status()).toBe(422);
		const issues: ValidationIssue[] = (await rejected.json()).error.issues;
		const issue = issues.find((candidate) => candidate.code === "url")!;
		expect(issue.path).toBe(`${field}.root.children.0.fields.links.0.url`);
		expect(issue.target).toEqual(expect.any(String));
		if (field === "localizedBody") expect(issue.locale).toBe("fr");
		if (!(await drawer.isVisible()))
			await card.getByRole("button", { name: "Edit", exact: true }).click();
		await expect(drawer.locator('input[name$=".links.0.url"]')).toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await drawer.getByRole("button", { name: "Open Row 1 actions", exact: true }).click();
		await page.getByRole("menuitem", { name: "Move down", exact: true }).click();
		await expect(drawer.locator('input[name$=".links.1.url"]')).toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await expect(drawer.locator('input[name$=".links.0.url"]')).not.toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await drawer.locator('input[name$=".links.1.url"]').fill("Corrected URL");
		await drawer.getByRole("button", { name: "Apply", exact: true }).click();
		expect((await save(page, original.id)).ok()).toBe(true);
		await page.reload();
		await card.getByRole("button", { name: "Edit", exact: true }).click();
		await expect(drawer.locator('input[name$=".links.1.url"]')).toHaveValue("Corrected URL");
		expect(errors.pageErrors).toEqual([]);
	});
