import { expect, test } from "./fixture";
import { loginAsEditor, observePageErrors } from "./helpers";

test("version history compares exact snapshots, filters locales and confirms restoration", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const administrator = await page.request.post("/api/auth/users/login", {
		data: { email: "admin@riducms.test", password: "ridu-admin" },
	});
	expect(administrator.ok()).toBe(true);
	const base = "/api/collections/posts";
	const created = await page.request.post(`${base}?locale=en&draft=true`, {
		data: {
			title: "Original English",
			slug: "version-comparison",
			summary: "Version comparison",
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	let document = (await created.json()).doc;
	const firstRevision = document._revision;
	const id = document.id;

	// Enough real revisions to exercise both table pages and the full comparison picker.
	for (let index = 0; index < 11; index++) {
		const response = await page.request.patch(`${base}/${id}?locale=en&draft=true`, {
			headers: { "If-Match": `"${document._revision}"` },
			data: { title: `English revision ${index}` },
		});
		expect(response.ok(), await response.text()).toBe(true);
		document = (await response.json()).doc;
	}
	const translated = await page.request.patch(`${base}/${id}?locale=fr&draft=true`, {
		headers: { "If-Match": `"${document._revision}"` },
		data: { title: "Version française", summary: "Comparaison" },
	});
	expect(translated.ok(), await translated.text()).toBe(true);
	document = (await translated.json()).doc;
	const route = `/admin/collections/posts/${id}/versions`;
	await page.goto(`${route}?locale=en`);
	const history = page.getByRole("table", { name: "Version history" });
	await expect(history.locator("tbody tr")).toHaveCount(10);
	await expect(page.getByText("1–10 of 13", { exact: true })).toBeVisible();
	await page.getByRole("button", { name: "Next page", exact: true }).click();
	await expect(page).toHaveURL(/page=2/);
	await expect(history.locator("tbody tr")).toHaveCount(3);
	await page.reload();
	await expect(history.locator("tbody tr")).toHaveCount(3);
	await history.getByRole("link").last().click();
	await expect(page).toHaveURL(new RegExp(`/versions/${firstRevision}\\?locale=en$`));
	await expect(page.getByRole("heading", { name: "Compare Versions" })).toBeVisible();
	await page.getByRole("button", { name: "Comparison revision" }).click();
	await page.getByRole("option", { name: "More versions…" }).click();
	const picker = page.getByRole("dialog", { name: "Select a version to compare" });
	await expect(picker).toBeVisible();
	await picker.getByRole("table").locator("tbody tr").first().getByRole("button").click();
	await expect(picker).toBeHidden();
	await expect(page).toHaveURL(new RegExp(`compare=${document._revision}`));
	const french = page.locator('[data-field-path="title:fr"]');
	const english = page.locator('[data-field-path="title:en"]');
	await expect(french.locator(".ridu-version-diff__value").first()).toHaveText("Version française");
	await expect(french.locator(".ridu-version-diff__value").last()).toHaveText("");
	await expect(english.locator(".ridu-version-diff__value").last()).toHaveText("Original English");
	await page.getByRole("button", { name: "Locales: en, fr, ar" }).click();
	await page.getByRole("checkbox", { name: "French", exact: true }).uncheck();
	await page.keyboard.press("Escape");
	await expect(french).toHaveCount(0);
	await expect(english).toBeVisible();
	await page.goBack();
	await expect(french).toBeVisible();

	// A dialog must precede mutation, and cancellation must preserve the current document.
	await page.getByRole("button", { name: "Restore this version", exact: true }).click();
	const confirmation = page.getByRole("alertdialog");
	await expect(confirmation).toContainText("Confirm Version Restoration");
	await page.getByRole("button", { name: "Cancel", exact: true }).click();
	await expect(confirmation).toBeHidden();
	const current = (await (await page.request.get(`${base}/${id}?locale=en`)).json()).doc;
	expect(current._revision).toBe(document._revision);
	await page.getByRole("button", { name: "Restore this version", exact: true }).click();
	await page.getByRole("button", { name: "Confirm", exact: true }).click();
	await expect(page).toHaveURL(new RegExp(`/admin/collections/posts/${id}\\?locale=en$`));
	await expect(page.locator('input[name="title"]')).toHaveValue("Original English");
	expect(errors.pageErrors).toEqual([]);
});
