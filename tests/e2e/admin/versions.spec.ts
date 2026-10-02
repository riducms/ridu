import { expect, test } from "./fixture";
import {
	documentSaveButton,
	loginAsEditor,
	observePageErrors,
	submitDocumentForm,
} from "./helpers";

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
	const versionsTab = page.getByRole("link", { name: "Versions", exact: true });
	const count = versionsTab.locator(".ridu-document-tab__count");
	await page.goto(`/admin/collections/posts/${id}/api?locale=en`);
	await expect(count).toHaveText("13");
	await expect(versionsTab).toHaveAccessibleDescription("13 total");
	await page.getByRole("link", { name: "Edit", exact: true }).click();
	await expect(count).toHaveText("13");
	const route = `/admin/collections/posts/${id}/versions`;
	await versionsTab.click();
	await expect(page).toHaveURL(new RegExp(`${route}\\?locale=en$`));
	const history = page.getByRole("table", { name: "Version history" });
	await expect(count).toHaveText("13");
	await expect(history.locator("tbody tr")).toHaveCount(10);
	await expect(page.getByText("1–10 of 13", { exact: true })).toBeVisible();
	await page.getByRole("button", { name: "Next page", exact: true }).click();
	await expect(page).toHaveURL(/page=2/);
	await expect(history.locator("tbody tr")).toHaveCount(3);
	await expect(count).toHaveText("13");
	await page.reload();
	await expect(history.locator("tbody tr")).toHaveCount(3);
	await history.getByRole("link").last().click();
	await expect(page).toHaveURL(new RegExp(`/versions/${firstRevision}\\?locale=en$`));
	await expect(page.getByRole("heading", { name: "Compare Versions" })).toBeVisible();
	await expect(count).toHaveText("13");
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
	await expect(count).toHaveText("14");
	expect(errors.pageErrors).toEqual([]);
});

test("document header refreshes the retained count after saves and publication changes", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/posts?locale=en&draft=true", {
		data: { title: "Version badge", slug: "version-badge", summary: "Header count" },
	});
	expect(created.ok(), await created.text()).toBe(true);
	const id = (await created.json()).doc.id;
	await page.goto(`/admin/collections/posts/${id}?locale=en`);
	const count = page
		.getByRole("link", { name: "Versions", exact: true })
		.locator(".ridu-document-tab__count");
	await expect(count).toHaveText("1");
	await page.locator('input[name="title"]').fill("Updated version badge");
	await submitDocumentForm(page);
	await expect(count).toHaveText("2");
	await documentSaveButton(page).click();
	await expect(count).toHaveText("3");
	await page.getByRole("button", { name: "More actions", exact: true }).click();
	await page.getByRole("button", { name: "Unpublish", exact: true }).click();
	await expect(count).toHaveText("4");
	expect(errors.pageErrors).toEqual([]);
});

test("empty or unavailable history hides the badge without blocking the document", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	let unavailable = false;
	let fullHistoryRequests = 0;
	await page.route("**/api/collections/posts/posts_9/versions?locale=en", (route) => {
		fullHistoryRequests++;
		return route.fulfill({ status: 500, json: { error: "Unexpected full history read" } });
	});
	await page.route("**/api/collections/posts/posts_9/versions/count?locale=en", (route) =>
		route.fulfill({
			status: unavailable ? 503 : 200,
			json: unavailable ? { error: "Count unavailable" } : { totalDocs: 0 },
		})
	);
	const versionsTab = page.getByRole("link", { name: "Versions", exact: true });
	const count = versionsTab.locator(".ridu-document-tab__count");
	for (const failure of [false, true]) {
		unavailable = failure;
		const response = page.waitForResponse((response) =>
			response.url().endsWith("/api/collections/posts/posts_9/versions/count?locale=en")
		);
		await page.goto("/admin/collections/posts/posts_9?locale=en");
		await response;
		await expect(versionsTab).toBeVisible();
		await expect(count).toHaveCount(0);
		await expect(page.locator('input[name="title"]')).toHaveValue("Welcome to Ridu");
		await expect(page.locator('input[name="title"]')).toBeEditable();
		expect(fullHistoryRequests).toBe(0);
	}
	expect(errors.pageErrors).toEqual([]);
});
