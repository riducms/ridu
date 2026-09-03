import { expect, test } from "./fixture";

import { documentSaveButton, loginAsEditor, observePageErrors } from "./helpers";

test("SEO plugin renders, generates from the unsaved draft, previews, and switches locale", async ({
	page,
}) => {
	test.setTimeout(45_000);
	const { consoleErrors, pageErrors } = observePageErrors(page);
	await loginAsEditor(page);
	consoleErrors.length = 0;
	await page.goto("/admin/collections/pages/create");

	await page.getByLabel("Title", { exact: true }).fill("A current unsaved SEO draft");
	const slug = page.getByLabel("Slug", { exact: true });
	await expect(slug).toHaveValue("a-current-unsaved-seo-draft");
	await expect(slug).toHaveAttribute("readonly", "");
	const seoTab = page.getByRole("tab", { name: "SEO", exact: true });
	await seoTab.click();
	await expect(seoTab).toHaveAttribute("aria-selected", "true");

	const overview = page.locator("[data-seo-overview]");
	await expect(overview).toContainText("0/3 checks are passing");
	const title = page.getByLabel("Title — English", { exact: true });
	const description = page.getByLabel("Description — English", { exact: true });
	await expect(title).toBeVisible();
	await expect(description).toBeVisible();
	await page
		.locator('[data-field-path="meta.title"]')
		.getByRole("button", { name: "Auto-generate" })
		.click();
	await expect(title).toHaveValue("A current unsaved SEO draft | Ridu");
	await page
		.locator('[data-field-path="meta.description"]')
		.getByRole("button", { name: "Auto-generate" })
		.click();
	await expect(description).toHaveValue(/A current unsaved SEO draft/);
	await expect(page.locator('[data-length-status="tooShort"]')).toHaveCount(1);

	const preview = page.locator("[data-seo-preview]");
	await expect(preview).toContainText("A current unsaved SEO draft | Ridu");
	await expect(preview).toContainText("https://riducms.test/en/pages/a-current-unsaved-seo-draft");
	const selectImage = page.getByRole("button", { name: "Select image" });
	await expect(selectImage).toBeEnabled();
	await selectImage.click();
	const cover = page.getByRole("radio", { name: "Select ridu-cover.png", exact: true });
	await cover.click();
	await page.getByRole("button", { name: "Select", exact: true }).click();
	await expect(page.getByRole("button", { name: "Inspect image" })).toBeVisible();

	await page.goto("/admin/collections/pages");
	await page.getByRole("link", { name: "About Ridu", exact: true }).click();
	const existingSlug = page.getByLabel("Slug", { exact: true });
	await page.getByRole("button", { name: "Unlock slug", exact: true }).click();
	await existingSlug.fill("manual-about-path");
	await documentSaveButton(page).click();
	await expect(existingSlug).toHaveValue("manual-about-path");
	await expect(existingSlug).not.toHaveAttribute("readonly", "");
	await page.getByRole("tab", { name: "SEO", exact: true }).click();
	await page.getByLabel("Content locale", { exact: true }).click();
	await page.getByRole("menuitem", { name: "French (fr)", exact: true }).click();
	await page.getByRole("tab", { name: "SEO", exact: true }).click();
	await expect(page.locator("[data-seo-preview]")).toContainText(
		"https://riducms.test/fr/pages/manual-about-path"
	);
	await expect(page.getByRole("link", { name: "Best practices" }).first()).toBeVisible();

	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});
