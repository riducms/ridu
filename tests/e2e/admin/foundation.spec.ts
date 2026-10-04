import { expect, test } from "./fixture";

import { documentSaveButton, loginAsEditor, observePageErrors } from "./helpers";

test("dashboard cards and the plugin route use the configured application extensions", async ({
	page,
}) => {
	const errors = observePageErrors(page);
	await loginAsEditor(page);
	await expect(page).toHaveURL(/\/admin\/?$/);
	await expect(page.getByRole("navigation", { name: "Breadcrumb" })).toContainText("Dashboard");
	await expect(page.getByRole("heading", { name: "Content", exact: true })).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "Comparison reference", exact: true })
	).toBeVisible();
	const postsCard = page
		.getByRole("article")
		.filter({ has: page.getByRole("heading", { name: "Posts", exact: true }) });
	await expect(postsCard.getByRole("link", { name: "Open Posts", exact: true })).toHaveAttribute(
		"href",
		"/admin/collections/posts"
	);
	await expect(postsCard.getByRole("link", { name: "Create post", exact: true })).toHaveAttribute(
		"href",
		"/admin/collections/posts/create"
	);
	await expect(page.getByRole("heading", { name: "Editorial pulse" })).toBeVisible();
	await page.getByRole("link", { name: "Plugin contract", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Plugin route contract" })).toBeVisible();
	expect(errors.consoleErrors).toEqual([]);
	expect(errors.pageErrors).toEqual([]);
});

test("global authoring preserves published values and global-only actions", async ({ page }) => {
	const errors = observePageErrors(page);
	await loginAsEditor(page);
	const history = await page.request.get("/api/globals/site-settings/versions?locale=en");
	expect(history.ok(), await history.text()).toBe(true);
	const initialCount = (await history.json()).versions.length;
	await page.goto("/admin/globals/site-settings?locale=en");
	const versionsTab = page.getByRole("link", { name: "Versions", exact: true });
	const count = versionsTab.locator(".ridu-document-tab__count");
	await expect(count).toHaveText(String(initialCount));
	await expect(page).toHaveURL(/\/admin\/globals\/site-settings(?:\?locale=en)?$/);
	await expect(page.getByRole("heading", { name: "Site settings", exact: true })).toBeVisible();
	await expect(page.getByLabel("Site name", { exact: true })).toHaveValue(
		"Ridu editorial kitchen sink"
	);
	await expect(page.getByLabel("Announcement — English", { exact: true })).toHaveValue(
		"Globals now use their own admin and API routes."
	);
	await page.getByRole("button", { name: "More actions" }).click();
	await expect(page.getByRole("button", { name: "Unpublish" })).toBeVisible();
	await page.keyboard.press("Escape");
	await expect(page.getByRole("button", { name: "Delete document" })).toHaveCount(0);
	await page
		.getByLabel("Announcement — English", { exact: true })
		.fill("Globals have drafts, hooks, access, versions, and generated types.");
	await page.getByRole("button", { name: "Publish changes", exact: true }).click();
	await expect(
		page.getByLabel("Notifications alt+T").getByText("Published", { exact: true })
	).toBeVisible();
	await expect(count).toHaveText(String(initialCount + 1));
	await page.reload();
	await expect(page.getByLabel("Announcement — English", { exact: true })).toHaveValue(
		"Globals have drafts, hooks, access, versions, and generated types."
	);
	await expect(count).toHaveText(String(initialCount + 1));
	await page.getByRole("link", { name: "API", exact: true }).click();
	await expect(count).toHaveText(String(initialCount + 1));
	await versionsTab.click();
	await expect(page.getByRole("table", { name: "Version history" })).toBeVisible();
	await expect(count).toHaveText(String(initialCount + 1));
	expect(errors.consoleErrors).toEqual([]);
	expect(errors.pageErrors).toEqual([]);
});

test("field layouts, clipboard actions, generated rows, and responsive placement stay integrated", async ({
	page,
}) => {
	test.setTimeout(60_000);
	const errors = observePageErrors(page);
	const collectionsNavigation = await loginAsEditor(page);
	await collectionsNavigation.getByRole("link", { name: "Field showcase", exact: true }).click();
	await page.getByRole("link", { name: "Field and array parity", exact: true }).click();
	await expect(page.getByLabel("Title", { exact: true })).toHaveAttribute(
		"placeholder",
		"Name this field showcase"
	);
	await expect(page.getByLabel("Code", { exact: true })).toHaveText("export const parity = true;");
	const audiences = page.locator('[data-field-path="audiences"]');
	await expect(audiences.getByRole("button", { name: "Remove Editors" })).toBeVisible();
	await expect(audiences.getByRole("button", { name: "Remove Reviewers" })).toBeVisible();
	await audiences.getByRole("button", { name: "Remove Editors" }).click();
	await audiences.getByRole("button", { name: "Remove Reviewers" }).click();
	await audiences.getByLabel("Audiences", { exact: true }).click();
	await page.getByRole("option", { name: "Reviewers", exact: true }).click();
	await page.getByRole("option", { name: "Administrators", exact: true }).click();
	await page.keyboard.press("Escape");
	await expect(audiences.locator(".ridu-combobox-tag")).toHaveText(["Reviewers", "Administrators"]);
	const documentSidebar = page.locator("[data-document-sidebar-fields]");
	await expect(documentSidebar.getByRole("radio", { name: "High", exact: true })).toBeChecked();
	await expect(
		documentSidebar.getByRole("spinbutton", { name: "Location - Longitude", exact: true })
	).toHaveValue("-0.1276");
	await expect(
		documentSidebar.getByRole("spinbutton", { name: "Location - Latitude", exact: true })
	).toHaveValue("51.5072");
	await expect(page.getByText("This content is presentation-only")).toBeVisible();
	const advancedSettings = page.locator("[data-field-collapsible]");
	await expect(advancedSettings).not.toHaveAttribute("open");
	await advancedSettings.getByText("Advanced Settings", { exact: true }).click();
	await expect(page.getByLabel("Internal name", { exact: true })).toHaveCount(0);
	const teamField = page.locator('[data-field-path="team"]');
	await expect(teamField.getByRole("button", { name: "Drag Rhea" })).toBeVisible();
	await teamField.getByRole("button", { name: "Collapse all" }).click();
	await expect(teamField.getByLabel("Display Name", { exact: true }).first()).toBeHidden();
	await teamField.getByRole("button", { name: "Show all" }).click();
	await expect(teamField.getByLabel("Display Name", { exact: true }).first()).toBeVisible();
	await teamField.getByRole("button", { name: "Open Rhea actions" }).click();
	await page.getByRole("menuitem", { name: "Copy row" }).click();
	await teamField.getByRole("button", { name: "Open Rhea actions" }).click();
	await page.getByRole("menuitem", { name: "Paste row" }).click();
	await expect(teamField.getByRole("button", { name: "Add row" })).toBeDisabled();
	await expect(page.getByText("3 rows · minimum 1 · maximum 3")).toBeVisible();
	await teamField.getByRole("button", { name: "Open Team actions" }).click();
	await page.getByRole("menuitem", { name: "Copy field" }).click();
	await page.getByLabel("Display Name", { exact: true }).first().fill("Temporary name");
	await teamField.getByRole("button", { name: "Open Team actions" }).click();
	await page.getByRole("menuitem", { name: "Paste field" }).click();
	await expect(page.getByLabel("Display Name", { exact: true }).first()).toHaveValue("Rhea");
	await expect(
		page.locator('[data-field-path="curriculumNodes"] [data-contract-row-label]')
	).toHaveText(["Lesson: lesson-intro (10)", "Question: question-check (20)"]);
	await expect(
		page.locator('[data-field-path="questionParts"] [data-contract-row-label]')
	).toHaveText(["A: Alpha", "B: Beta"]);
	expect(
		await page
			.locator("[data-contract-row-label]")
			.evaluateAll((labels) => labels.every((label) => label.closest("button") === null))
	).toBe(true);
	await expect(page.locator('[data-virtual-field="fieldSummary"]')).toHaveText(
		"Field and array parity · high"
	);
	const documentID = new URL(page.url()).pathname.split("/").at(-1);
	expect(documentID).toBeTruthy();
	const updateRequest = page.waitForRequest(
		(request) =>
			request.method() === "PATCH" &&
			new URL(request.url()).pathname === `/api/collections/payload-only-capabilities/${documentID}`
	);
	const updateResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname ===
				`/api/collections/payload-only-capabilities/${documentID}`
	);
	await documentSaveButton(page).click();
	const submitted = (await updateRequest).postDataJSON();
	expect(submitted).not.toHaveProperty("internalName");
	expect(submitted.audiences).toEqual(["reviewers", "administrators"]);
	expect((await updateResponse).ok()).toBe(true);
	const storedResponse = await page.request.get(
		`/api/collections/payload-only-capabilities/${documentID}`
	);
	expect(storedResponse.ok()).toBe(true);
	const stored = (await storedResponse.json()).doc;
	expect(stored.internalName).toBe("field-array-showcase");
	expect(stored.audiences).toEqual(["reviewers", "administrators"]);

	const documentMain = page.locator("[data-document-main-fields]");
	const wideMainBox = await documentMain.boundingBox();
	const wideSidebarBox = await documentSidebar.boundingBox();
	expect(wideMainBox).not.toBeNull();
	expect(wideSidebarBox).not.toBeNull();
	expect(wideSidebarBox!.x).toBeGreaterThan(wideMainBox!.x + wideMainBox!.width);
	await page.setViewportSize({ width: 760, height: 900 });
	await expect
		.poll(async () => {
			const mainBox = await documentMain.boundingBox();
			const sidebarBox = await documentSidebar.boundingBox();
			if (mainBox === null || sidebarBox === null) return false;
			return sidebarBox.y >= mainBox.y + mainBox.height;
		})
		.toBe(true);
	expect(errors.consoleErrors).toEqual([]);
	expect(errors.pageErrors).toEqual([]);
});
