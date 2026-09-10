import { expect, test } from "./fixture";

import {
	chooseRiduSelect,
	expectRiduSelectValue,
	loginAsEditor,
	observePageErrors,
} from "./helpers";

test("command navigation, saved views, and bulk actions", async ({ page }) => {
	const { consoleErrors, pageErrors } = observePageErrors(page);
	const collectionsNavigation = await loginAsEditor(page);
	consoleErrors.length = 0;
	await collectionsNavigation.getByRole("link", { name: "Posts", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Posts" })).toBeVisible();
	await expect(page.getByText("Welcome to Ridu")).toBeVisible();
	const commandTrigger = page.getByRole("button", { name: "Search and navigate" });
	await commandTrigger.focus();
	await expect(commandTrigger).toBeFocused();
	await expect
		.poll(async () => commandTrigger.evaluate((element) => getComputedStyle(element).outlineWidth))
		.not.toBe("0px");
	await commandTrigger.click();
	await expect(page.getByRole("dialog", { name: "Navigate Ridu" })).toBeVisible();
	const commandInput = page.getByRole("combobox", {
		name: "Search documents, collections, actions",
	});
	await expect(commandInput).toBeFocused();
	const selectedCommand = page.locator('[data-slot="command-item"][data-selected]');
	await expect(selectedCommand).toHaveCount(1);
	await expect
		.poll(async () =>
			selectedCommand.evaluate((element) => getComputedStyle(element).backgroundColor)
		)
		.not.toBe("rgba(0, 0, 0, 0)");
	await commandInput.fill("new post");
	await expect(page.getByRole("option", { name: /New post/ })).toBeVisible();
	await page.keyboard.press("Escape");

	await page.getByRole("link", { name: "Posts", exact: true }).click();
	await expect(page.getByText("Welcome to Ridu")).toBeVisible();
	await page.getByRole("button", { name: "More", exact: true }).click();
	await chooseRiduSelect(page, page.getByLabel("Folder", { exact: true }), "Editorial");
	await page.getByRole("button", { name: "Hierarchy", exact: true }).click();
	await expect(page).toHaveURL(/folder=.*&view=hierarchy/);
	await page.keyboard.press("Escape");
	await page.getByRole("button", { name: "Saved views", exact: true }).click();
	await page.getByLabel("Saved view name", { exact: true }).fill("Editorial hierarchy");
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect(
		page.getByRole("button", { name: "Editorial hierarchy", exact: true })
	).toBeVisible();
	await page.reload();
	await page.getByRole("button", { name: "More", exact: true }).click();
	await expectRiduSelectValue(page.getByLabel("Folder", { exact: true }), "Editorial");
	await page.keyboard.press("Escape");
	await page.getByRole("button", { name: "Saved views", exact: true }).click();
	await expect(
		page.getByRole("button", { name: "Editorial hierarchy", exact: true })
	).toBeVisible();
	await page.keyboard.press("Escape");
	await page.getByRole("button", { name: "More", exact: true }).click();
	await chooseRiduSelect(page, page.getByLabel("Folder", { exact: true }), "All folders");
	await page.getByRole("button", { name: "List", exact: true }).click();
	await page.keyboard.press("Escape");
	await page.getByRole("button", { name: "Columns" }).click();
	await expect(page.getByText("Columns", { exact: true }).last()).toBeVisible();
	await page.keyboard.press("Escape");
	await page.getByRole("checkbox", { name: "Select Welcome to Ridu" }).click();
	await expect(page.getByRole("region", { name: "Bulk actions" })).toContainText("1 selected");
	await page
		.getByRole("region", { name: "Bulk actions" })
		.getByRole("button", { name: "Unpublish" })
		.click();
	await expect(
		page
			.locator("[data-sonner-toast][data-front='true']")
			.filter({ hasText: "1 document unpublished" })
	).toBeVisible();
	await page.getByRole("checkbox", { name: "Select Welcome to Ridu" }).click();
	await page
		.getByRole("region", { name: "Bulk actions" })
		.getByRole("button", { name: "Edit", exact: true })
		.click();
	const bulkEditDialog = page.getByRole("dialog", { name: "Edit 1 selected document" });
	await chooseRiduSelect(page, bulkEditDialog.getByLabel("Field"), "Reading time (minutes)");
	await bulkEditDialog.getByLabel("Value").fill("9");
	await bulkEditDialog.getByRole("button", { name: "Apply change" }).click();
	await expect(page.getByRole("region", { name: "Bulk actions" })).toBeHidden();
	await page.getByRole("checkbox", { name: "Select Welcome to Ridu" }).click();
	await page
		.getByRole("region", { name: "Bulk actions" })
		.getByRole("button", { name: "Publish", exact: true })
		.click();
	await expect(
		page
			.locator("[data-sonner-toast][data-front='true']")
			.filter({ hasText: "1 document published" })
	).toBeVisible();
	await page.getByRole("searchbox", { name: "Search by Title" }).fill("missing");
	await expect(page.getByRole("heading", { name: "Nothing matches" })).toBeVisible();
	await page.getByRole("button", { name: "Clear filters" }).click();
	await expect(page.getByText("Welcome to Ridu")).toBeVisible();
	await page.getByRole("button", { name: "Published 1" }).click();
	await expect(page.getByText("Welcome to Ridu")).toBeVisible();
	await expect(page.getByRole("button", { name: "Published 1" })).toHaveAttribute(
		"aria-pressed",
		"true"
	);
	await page.getByRole("button", { name: "All 2" }).click();
	await expect(page.getByRole("columnheader", { name: "Reading time" })).toBeVisible();
	await expect(page.getByText("9 min", { exact: true })).toBeVisible();

	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});

test("relationship browsers, inline documents, uploads, and the document API view", async ({
	page,
}) => {
	test.setTimeout(45_000);
	const { consoleErrors, pageErrors } = observePageErrors(page);
	await loginAsEditor(page);
	consoleErrors.length = 0;
	await page.goto("/admin/collections/posts");
	await page.getByRole("link", { name: "Welcome to Ridu", exact: true }).click();
	await page.getByRole("button", { name: "Request review", exact: true }).click();
	await expect(page.getByText("Review requested", { exact: true })).toBeVisible();
	await page.getByRole("button", { name: "Insights", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Editorial insights" })).toBeVisible();
	await page.getByRole("link", { name: "Edit", exact: true }).click();
	const seoTabs = page.getByRole("tablist", { name: "SEO section" });
	const seoTab = seoTabs.getByRole("tab", { name: "SEO", exact: true });
	await expect(seoTab).toHaveAttribute("aria-selected", "true");
	const seoPanelID = await seoTab.getAttribute("aria-controls");
	expect(seoPanelID).toBeTruthy();
	await expect(page.locator(`#${seoPanelID}`)).toHaveRole("tabpanel");
	await expect(page.locator(`#${seoPanelID}`)).toBeVisible();
	const namedSEOTab = page.locator('[data-field-path="seo"]');
	await expect(namedSEOTab.getByLabel("Canonical URL", { exact: true })).toHaveValue(
		"https://example.test/welcome-to-ridu"
	);
	await expect(namedSEOTab.locator("legend").filter({ hasText: "SEO" })).toHaveCount(0);
	const layoutTabs = page.getByRole("tablist", {
		name: "Related, Layout, and Advanced sections",
	});
	const relatedTab = layoutTabs.getByRole("tab", { name: "Related", exact: true });
	const layoutTab = layoutTabs.getByRole("tab", { name: "Layout", exact: true });
	await relatedTab.focus();
	await relatedTab.press("ArrowRight");
	await expect(layoutTab).toBeFocused();
	await expect(layoutTab).toHaveAttribute("aria-selected", "true");
	await expect(namedSEOTab.getByLabel("Canonical URL", { exact: true })).toBeVisible();
	await layoutTabs.getByRole("tab", { name: "Advanced", exact: true }).click();
	await expect(page.locator('[data-field-path="metadata"]')).toBeVisible();
	const relatedPosts = page.locator('[data-field-path="relatedPosts"]');
	await expect(
		relatedPosts.getByRole("button", { name: "Browse posts", exact: true })
	).toBeVisible();
	const relatedContent = page.locator('[data-field-path="relatedContent"]');
	let relationDialog = page.getByRole("dialog", { name: "Select related content" });
	await relatedContent.getByRole("button", { name: "Related content", exact: true }).click();
	const relatedContentSearch = page.getByRole("combobox", { name: "Search Related content" });
	await expect(relatedContentSearch).toBeFocused();
	const relatedPostsGroup = page.getByRole("group", { name: "Posts" });
	const relatedPagesGroup = page.getByRole("group", { name: "Pages" });
	await expect(relatedPostsGroup.getByRole("option", { name: "Welcome to Ridu" })).toBeVisible();
	await expect(relatedPagesGroup.getByRole("option", { name: "About Ridu" })).toBeVisible();
	await relatedPagesGroup.getByRole("button", { name: /^Browse all pages/ }).click();
	relationDialog = page.getByRole("dialog", { name: "Select related content" });
	await expect(relationDialog.getByText("About Ridu", { exact: true })).toBeVisible();
	await relationDialog.getByRole("button", { name: "Close relationship browser" }).click();
	const publishedPage = page.locator('[data-field-path="publishedPage"]');
	await publishedPage.getByRole("button", { name: "Published page", exact: true }).click();
	await expect(page.getByRole("option", { name: "About Ridu", exact: true })).toBeVisible();
	await expect(page.getByRole("option", { name: "Unreleased page", exact: true })).toHaveCount(0);
	await page.keyboard.press("Escape");
	const authorField = page.locator('[data-field-path="author"]');
	await expect(authorField.getByRole("button", { name: "Edit Ridu Editor" }).first()).toBeVisible();
	await expect(authorField.getByRole("button", { name: "Browse users" })).toBeVisible();
	await authorField.getByRole("button", { name: "Author", exact: true }).click();
	const authorCombobox = page.getByRole("combobox", { name: "Search Users" });
	await expect(authorCombobox).toBeFocused();
	await authorCombobox.fill("Demo");
	const demoAuthorOption = page.getByRole("option", { name: "Demo Author" });
	await expect(demoAuthorOption).toBeVisible();
	await authorCombobox.press("ArrowDown");
	await expect(demoAuthorOption).toHaveAttribute("data-highlighted");
	await page.keyboard.press("Enter");
	await expect(authorField.getByRole("button", { name: "Edit Demo Author" }).first()).toBeVisible();

	await authorField.getByRole("button", { name: "Browse users" }).click();
	relationDialog = page.getByRole("dialog", { name: "Select author" });
	const riduEditorRadio = relationDialog.getByRole("radio", { name: /Ridu Editor/ });
	await expect(riduEditorRadio).toBeVisible();
	const authorRadios = relationDialog.getByRole("radio");
	expect(await authorRadios.count()).toBeGreaterThan(1);
	expect(
		(await authorRadios.evaluateAll((radios) => radios.map((radio) => radio.tabIndex))).filter(
			(tabIndex) => tabIndex === 0
		)
	).toHaveLength(1);
	await authorRadios.first().focus();
	await authorRadios.first().press("ArrowDown");
	await expect(authorRadios.nth(1)).toBeFocused();
	await expect(authorRadios.nth(1)).toBeChecked();
	await riduEditorRadio.click();
	await riduEditorRadio.press("Space");
	await expect(riduEditorRadio).toBeChecked();
	const authorSearch = relationDialog.getByRole("searchbox", { name: "Search Users" });
	await authorSearch.fill("Demo");
	await expect(riduEditorRadio).toBeHidden();
	const offscreenDemoRadio = relationDialog.getByRole("radio", { name: /Demo Author/ });
	await expect(offscreenDemoRadio).toBeVisible();
	expect(
		(
			await relationDialog
				.getByRole("radio")
				.evaluateAll((radios) => radios.map((radio) => radio.tabIndex))
		).filter((tabIndex) => tabIndex === 0)
	).toHaveLength(1);
	await expect(authorSearch).toBeFocused();
	for (let index = 0; index < 12; index += 1) {
		await page.keyboard.press("Tab");
		if (await offscreenDemoRadio.evaluate((radio) => document.activeElement === radio)) break;
	}
	await expect(offscreenDemoRadio).toBeFocused();
	await expect(offscreenDemoRadio).not.toBeChecked();
	await authorSearch.fill("");
	await expect(riduEditorRadio).toBeVisible();
	await expect(riduEditorRadio).toBeChecked();
	await relationDialog.getByRole("button", { name: "Select", exact: true }).click();
	await expect(authorField.getByRole("button", { name: "Edit Ridu Editor" }).first()).toBeVisible();

	await authorField.getByRole("button", { name: "Browse users" }).click();
	relationDialog = page.getByRole("dialog", { name: "Select author" });
	await relationDialog.getByRole("button", { name: "Create user" }).click();
	let documentDialog = page.getByRole("dialog", { name: "New user" });
	await documentDialog.getByLabel("Name", { exact: true }).fill("Inline Author");
	await documentDialog.getByLabel("Email", { exact: true }).fill("inline@riducms.test");
	await documentDialog.getByLabel("Password", { exact: true }).fill("inline-secret");
	await documentDialog.getByLabel("Confirm password", { exact: true }).fill("inline-secret");
	await documentDialog.getByRole("button", { name: "Create user" }).first().click();
	relationDialog = page.getByRole("dialog", { name: "Select author" });
	await expect(relationDialog.getByRole("radio", { name: /Inline Author/ })).toBeChecked();
	await relationDialog.getByRole("button", { name: "Select", exact: true }).click();
	await authorField.getByRole("button", { name: "Edit Inline Author" }).first().click();
	documentDialog = page.getByRole("dialog", { name: "Inline Author" });
	await documentDialog.getByLabel("Name", { exact: true }).fill("Inline Author Edited");
	await documentDialog.getByRole("button", { name: "Save user" }).first().click();
	documentDialog = page.getByRole("dialog", { name: "Inline Author Edited" });
	await expect(documentDialog).toBeVisible();
	await documentDialog.getByRole("button", { name: "Close relationship browser" }).click();
	await expect(
		authorField.getByRole("button", { name: "Edit Inline Author Edited" }).first()
	).toBeVisible();

	await page
		.locator('[data-field-path="cover"]')
		.getByRole("button", { name: "Replace", exact: true })
		.click();
	relationDialog = page.getByRole("dialog", { name: "Select cover" });
	await relationDialog.getByRole("button", { name: "Images", exact: true }).click();
	await relationDialog.getByRole("searchbox", { name: "Search Media" }).fill("field-notes");
	await expect(relationDialog.getByRole("radio", { name: "Select field-notes.png" })).toBeVisible();
	await relationDialog.getByRole("button", { name: "Edit field-notes.png" }).click();
	documentDialog = page.getByRole("dialog", { name: "Green field notes cover" });
	await expect(documentDialog.getByText("24 × 16")).toBeVisible();
	await expect(documentDialog.getByText("image/png")).toBeVisible();
	await documentDialog.getByRole("button", { name: "Back to results" }).click();
	relationDialog = page.getByRole("dialog", { name: "Select cover" });
	await relationDialog.getByRole("searchbox", { name: "Search Media" }).fill("");
	await relationDialog.getByRole("button", { name: "Upload new" }).click();
	documentDialog = page.getByRole("dialog", { name: "New asset" });
	await documentDialog.locator('input[type="file"]').setInputFiles({
		name: "inline-cover.png",
		mimeType: "image/png",
		buffer: Buffer.from(
			"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABAQMAAAAl21bKAAAAA1BMVEV8Ou0Bg+xSAAAACklEQVQI12NgAAAAAgAB4iG8MwAAAABJRU5ErkJggg==",
			"base64"
		),
	});
	await documentDialog.getByRole("button", { name: "Back to results" }).click();
	const discardFileDialog = page.getByRole("dialog", { name: "Discard changes?" });
	await expect(discardFileDialog).toBeVisible();
	await discardFileDialog.getByRole("button", { name: "Keep editing" }).click();
	await documentDialog.getByPlaceholder("Untitled asset").fill("Inline cover");
	let releaseInlineUpload!: () => void;
	const inlineUploadGate = new Promise<void>((resolve) => {
		releaseInlineUpload = resolve;
	});
	const inlineUploadURL = (url: URL) => url.pathname === "/api/collections/media";
	await page.route(inlineUploadURL, async (route) => {
		if (route.request().method() !== "POST") {
			await route.continue();
			return;
		}
		await inlineUploadGate;
		await route.continue();
	});
	await documentDialog.getByRole("button", { name: "Create asset" }).first().click();
	await expect(documentDialog.getByRole("button", { name: "Back to results" })).toBeDisabled();
	await expect(
		documentDialog.getByRole("button", { name: "Close relationship browser" })
	).toBeDisabled();
	releaseInlineUpload();
	relationDialog = page.getByRole("dialog", { name: "Select cover" });
	await expect(
		relationDialog.getByRole("radio", { name: "Select inline-cover.png" })
	).toBeChecked();
	await page.unroute(inlineUploadURL);
	await relationDialog.getByRole("button", { name: "Select", exact: true }).click();
	await expect(page.getByText("inline-cover.png", { exact: true })).toBeVisible();

	await page
		.locator('[data-field-path="relatedPosts"]')
		.getByRole("button", { name: "Browse posts", exact: true })
		.click();
	relationDialog = page.getByRole("dialog", { name: "Add related posts" });
	await expect(
		relationDialog.getByRole("checkbox", { name: /Relationship field notes/ })
	).toBeChecked();
	await relationDialog.getByRole("button", { name: "Create post" }).click();
	documentDialog = page.getByRole("dialog", { name: "New post" });
	await documentDialog.getByPlaceholder("Untitled post").fill("Inline related post");
	await documentDialog.getByRole("button", { name: "Create post" }).first().click();
	await expect(documentDialog.getByText("Summary is required")).toBeVisible();
	await expect(documentDialog.getByLabel("Summary — English", { exact: true })).toBeFocused();
	await expect(
		page
			.locator("[data-sonner-toast][data-front='true']")
			.filter({ hasText: "The following field is invalid: Summary" })
	).toBeVisible();
	await documentDialog
		.getByLabel("Summary — English", { exact: true })
		.fill("Created from the related-post browser.");
	await documentDialog.getByRole("button", { name: "Create post" }).first().click();
	relationDialog = page.getByRole("dialog", { name: "Add related posts" });
	await expect(relationDialog.getByRole("checkbox", { name: /Inline related post/ })).toBeChecked();
	await relationDialog.getByRole("button", { name: "Edit Inline related post" }).click();
	documentDialog = page.getByRole("dialog", { name: "Inline related post" });
	await expect(documentDialog.getByRole("textbox", { name: "Content — English" })).toBeVisible();
	await documentDialog.getByRole("button", { name: "Close relationship browser" }).click();
	await expect(documentDialog).toBeHidden();

	await page
		.getByRole("navigation", { name: "Breadcrumb" })
		.getByRole("link", { name: "Posts", exact: true })
		.click();
	await page
		.getByRole("dialog", { name: "Leave without saving?" })
		.getByRole("button", { name: "Leave without saving" })
		.click();
	await expect(page.getByRole("heading", { name: "Posts" })).toBeVisible();
	await page.getByRole("link", { name: "Create", exact: true }).click();
	await page.getByRole("link", { name: "API" }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts\/create\/api(?:\?locale=en)?$/);
	await expect(page.getByRole("heading", { name: "API" })).toBeVisible();
	await expect(
		page
			.getByRole("status")
			.filter({ hasText: "This endpoint becomes runnable after the document is saved." })
	).toBeVisible();
	const apiActions = page.getByRole("navigation", { name: "API actions" });
	await apiActions.getByRole("button", { name: "POST Create" }).click();
	await expect(
		page.getByRole("status").filter({ hasText: "Mutation examples are not executed here" })
	).toBeVisible();
	await apiActions.getByRole("button", { name: "GET View" }).click();
	await expect(
		page
			.getByRole("status")
			.filter({ hasText: "This endpoint becomes runnable after the document is saved." })
	).toBeVisible();
	const jsonData = page.getByLabel("JSON data");
	await expect(jsonData).toBeVisible();
	const jsonRoot = jsonData.locator("details").first();
	const jsonRootSummary = jsonRoot.locator("summary");
	await jsonRootSummary.focus();
	await jsonRootSummary.press("Space");
	await expect(jsonRoot).not.toHaveAttribute("open");
	await jsonRootSummary.press("Enter");
	await expect(jsonRoot).toHaveAttribute("open", "");
	await expect(page.getByRole("button", { name: "Copy JSON" })).toBeVisible();
	await page.reload();
	await expect(page.getByRole("heading", { name: "API" })).toBeVisible();
	await expect(page.getByLabel("JSON data")).toBeVisible();
	await page.getByRole("link", { name: "Edit", exact: true }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts\/create(?:\?locale=en)?$/);

	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});
