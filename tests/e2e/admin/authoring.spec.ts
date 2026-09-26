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
	await page.getByRole("button", { name: "More", exact: true }).click();
	await page.getByLabel("Saved view name", { exact: true }).fill("Editorial hierarchy");
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect(
		page.getByRole("button", { name: "Editorial hierarchy", exact: true })
	).toBeVisible();
	await page.reload();
	await page.getByRole("button", { name: "More", exact: true }).click();
	await expectRiduSelectValue(page.getByLabel("Folder", { exact: true }), "Editorial");
	await page.keyboard.press("Escape");
	await page.getByRole("button", { name: "More", exact: true }).click();
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
	const unpublishDialog = page.getByRole("alertdialog", { name: "Confirm unpublish" });
	await expect(unpublishDialog).toContainText(
		"You are about to unpublish all Posts in the selection. Are you sure?"
	);
	await unpublishDialog.getByRole("button", { name: "Confirm", exact: true }).click();
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
	const bulkEditDialog = page.getByRole("dialog", { name: "Editing 1 Post" });
	const fieldPicker = bulkEditDialog.getByRole("combobox", { name: "Select fields to edit" });
	await fieldPicker.click();
	await page.getByRole("option", { name: "Reading time (minutes)", exact: true }).click();
	await fieldPicker.click();
	await page.getByRole("option", { name: "Featured", exact: true }).click();
	const readingMinutes = bulkEditDialog.getByLabel("Reading time (minutes)", { exact: true });
	await readingMinutes.fill("8");
	await bulkEditDialog.getByRole("checkbox", { name: "Featured", exact: true }).check();
	await page.route(
		/\/api\/collections\/posts\/bulk(?:\?.*)?$/,
		(route) =>
			route.fulfill({
				status: 422,
				contentType: "application/json",
				body: JSON.stringify({
					error: {
						code: "validation",
						status: 422,
						message: "Reading time is invalid",
						issues: [
							{
								code: "reserved",
								path: "readingMinutes",
								message: "Choose another reading time",
							},
						],
					},
				}),
			}),
		{ times: 1 }
	);
	const applyChanges = bulkEditDialog.getByRole("button", { name: "Apply changes" });
	await applyChanges.click();
	await expect(
		bulkEditDialog.getByText("Choose another reading time", { exact: true })
	).toBeVisible();
	await expect(readingMinutes).toBeFocused();
	expect(consoleErrors).toEqual([
		"Failed to load resource: the server responded with a status of 422 (Unprocessable Entity)",
	]);
	consoleErrors.length = 0;
	await readingMinutes.fill("9");
	await expect(
		bulkEditDialog.getByText("Choose another reading time", { exact: true })
	).toBeHidden();
	const bulkRequest = page.waitForRequest(
		(request) =>
			new URL(request.url()).pathname === "/api/collections/posts/bulk" &&
			request.method() === "POST"
	);
	await applyChanges.click();
	const request = await bulkRequest;
	expect(request.postDataJSON()).toMatchObject({
		action: "update",
		data: { featured: true, readingMinutes: 9 },
	});
	await expect(page.getByRole("region", { name: "Bulk actions" })).toBeHidden();
	await page.getByRole("checkbox", { name: "Select Welcome to Ridu" }).click();
	await page
		.getByRole("region", { name: "Bulk actions" })
		.getByRole("button", { name: "Publish", exact: true })
		.click();
	const publishDialog = page.getByRole("alertdialog", { name: "Confirm publish" });
	await expect(publishDialog).toContainText(
		"You are about to publish all Posts in the selection. Are you sure?"
	);
	await publishDialog.getByRole("button", { name: "Confirm", exact: true }).click();
	await expect(
		page
			.locator("[data-sonner-toast][data-front='true']")
			.filter({ hasText: "1 document published" })
	).toBeVisible();
	await page.getByRole("searchbox", { name: "Search by Title" }).fill("missing");
	await expect(page.getByRole("heading", { name: "Nothing matches" })).toBeVisible();
	await page.getByRole("button", { name: "Clear filters" }).click();
	await expect(page.getByText("Welcome to Ridu")).toBeVisible();
	await page.getByRole("button", { name: "Filters", exact: true }).click();
	await page.getByRole("button", { name: "Add filter", exact: true }).click();
	await chooseRiduSelect(page, page.getByLabel("Filter field", { exact: true }), "Status");
	await chooseRiduSelect(page, page.getByLabel("Filter value", { exact: true }), "Published");
	await expect(page.getByText("Welcome to Ridu", { exact: true })).toBeVisible();
	await expect(page.getByText("Relationship field notes", { exact: true })).toHaveCount(0);
	await page.getByRole("button", { name: "Remove filter 1", exact: true }).click();
	await expect(page.getByText("Relationship field notes", { exact: true })).toBeVisible();

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
	const relatedPostsInput = relatedPosts.getByRole("combobox", {
		name: "Related posts",
		exact: true,
	});
	await relatedPostsInput.click();
	await expect(page.getByRole("option", { name: "Welcome to Ridu", exact: true })).toBeVisible();
	await expect(
		page.getByRole("option", { name: "Relationship field notes", exact: true })
	).toHaveCount(0);
	await page.getByRole("option", { name: "Welcome to Ridu", exact: true }).click();
	await expect(relatedPostsInput).toHaveValue("");
	await expect(relatedPostsInput).toBeFocused();
	await expect(page.getByRole("option")).toHaveCount(0);
	await relatedPosts.getByRole("button", { name: "Remove Welcome to Ridu", exact: true }).click();
	await relatedPostsInput.click();
	await expect(page.getByRole("option", { name: "Welcome to Ridu", exact: true })).toBeVisible();
	await page.keyboard.press("Escape");
	const relatedContent = page.locator('[data-field-path="relatedContent"]');
	let relationDialog = page.getByRole("dialog", { name: "Select related content" });
	await relatedContent.getByRole("combobox", { name: "Related content", exact: true }).click();
	const relatedContentSearch = page.getByRole("combobox", { name: "Related content", exact: true });
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
	await publishedPage.getByRole("combobox", { name: "Published page", exact: true }).click();
	await expect(page.getByRole("option", { name: "About Ridu", exact: true })).toBeVisible();
	await expect(page.getByRole("option", { name: "Unreleased page", exact: true })).toHaveCount(0);
	await page.keyboard.press("Escape");
	const authorField = page.locator('[data-field-path="author"]');
	await expect(authorField.getByRole("button", { name: "Edit Ridu Editor" }).first()).toBeVisible();
	await authorField.getByRole("combobox", { name: "Author", exact: true }).click();
	const authorCombobox = authorField.getByRole("combobox", { name: "Author", exact: true });
	await expect(authorCombobox).toBeFocused();
	await authorCombobox.fill("Demo");
	const demoAuthorOption = page.getByRole("option", { name: "Demo Author" });
	await expect(demoAuthorOption).toBeVisible();
	await authorCombobox.press("ArrowDown");
	await expect(demoAuthorOption).toHaveAttribute("data-highlighted");
	await page.keyboard.press("Enter");
	await expect(authorField.getByRole("button", { name: "Edit Demo Author" }).first()).toBeVisible();

	await authorCombobox.click();
	await page.getByRole("button", { name: /^Browse all users/ }).click();
	relationDialog = page.getByRole("dialog", { name: "Select author" });
	const authorResults = relationDialog.getByRole("table", { name: "Results" });
	const riduEditorRow = authorResults.getByRole("row").filter({ hasText: "Ridu Editor" });
	const demoAuthorRow = authorResults.getByRole("row").filter({ hasText: "Demo Author" });
	await expect(demoAuthorRow).toHaveAttribute("data-selected", "true");
	await expect(riduEditorRow).toHaveAttribute("data-selected", "false");
	await expect(authorResults.locator("tbody tr")).not.toHaveCount(0);
	const authorSearch = relationDialog.getByRole("searchbox", { name: "Search Users" });
	await expect(authorSearch).toBeFocused();
	await authorSearch.fill("Demo");
	await expect(riduEditorRow).toHaveCount(0);
	const demoAuthorButton = authorResults.getByRole("button", {
		name: "Demo Author",
		exact: true,
	});
	await expect(demoAuthorButton).toBeVisible();
	await expect(authorSearch).toBeFocused();
	await demoAuthorButton.focus();
	await expect(demoAuthorButton).toBeFocused();
	await demoAuthorButton.press("Enter");
	await expect(relationDialog).toBeHidden();
	await expect(authorField.getByRole("button", { name: "Edit Demo Author" }).first()).toBeVisible();

	await authorCombobox.click();
	await page.getByRole("button", { name: /^Browse all users/ }).click();
	relationDialog = page.getByRole("dialog", { name: "Select author" });
	await relationDialog
		.getByRole("table", { name: "Results" })
		.getByRole("button", {
			name: "Ridu Editor",
			exact: true,
		})
		.click();
	await expect(relationDialog).toBeHidden();
	await expect(authorField.getByRole("button", { name: "Edit Ridu Editor" }).first()).toBeVisible();

	await authorCombobox.click();
	await page.getByRole("button", { name: /^Browse all users/ }).click();
	relationDialog = page.getByRole("dialog", { name: "Select author" });
	await relationDialog.getByRole("button", { name: "Create New" }).click();
	let documentDialog = page.getByRole("dialog", { name: "New user" });
	await documentDialog.getByLabel("Name", { exact: true }).fill("Inline Author");
	await documentDialog.getByLabel("Email", { exact: true }).fill("inline@riducms.test");
	await documentDialog.getByLabel("Password", { exact: true }).fill("inline-secret");
	await documentDialog.getByLabel("Confirm password", { exact: true }).fill("inline-secret");
	await documentDialog.getByRole("button", { name: "Save", exact: true }).click();
	relationDialog = page.getByRole("dialog", { name: "Select author" });
	const inlineAuthorRow = relationDialog
		.getByRole("table", { name: "Results" })
		.getByRole("row")
		.filter({ hasText: "Inline Author" });
	await expect(inlineAuthorRow).toHaveAttribute("data-selected", "true");
	await inlineAuthorRow.getByRole("button", { name: "Inline Author", exact: true }).click();
	await expect(relationDialog).toBeHidden();
	await authorField.getByRole("button", { name: "Edit Inline Author" }).first().click();
	documentDialog = page.getByRole("dialog", { name: "Inline Author" });
	await documentDialog.getByLabel("Name", { exact: true }).fill("Inline Author Edited");
	await documentDialog.getByRole("button", { name: "Save", exact: true }).click();
	documentDialog = page.getByRole("dialog", { name: "Inline Author Edited" });
	await expect(documentDialog).toBeVisible();
	await documentDialog.getByRole("button", { name: "Close relationship browser" }).click();
	await expect(
		authorField.getByRole("button", { name: "Edit Inline Author Edited" }).first()
	).toBeVisible();

	await page
		.locator('[data-field-path="cover"]')
		.getByRole("button", { name: "Remove ridu-cover.png", exact: true })
		.click();
	await page
		.locator('[data-field-path="cover"]')
		.getByRole("button", { name: "Choose from existing", exact: true })
		.click();
	relationDialog = page.getByRole("dialog", { name: "Select cover" });
	await relationDialog.getByRole("searchbox", { name: "Search Media" }).fill("field-notes");
	const mediaResults = relationDialog.getByRole("table", { name: "Results" });
	const fieldNotesRow = mediaResults.getByRole("row").filter({ hasText: "field-notes.png" });
	await expect(
		fieldNotesRow.getByRole("button", { name: "field-notes.png", exact: true })
	).toBeVisible();
	await expect(fieldNotesRow.locator(".ridu-reference-list__thumbnail")).toBeVisible();
	await relationDialog.getByRole("button", { name: "Edit field-notes.png" }).click();
	documentDialog = page.getByRole("dialog", { name: "field-notes.png" });
	const fieldNotesPreview = documentDialog.getByRole("region", { name: "Asset preview" });
	await expect(fieldNotesPreview).toContainText("24 × 16");
	await expect(fieldNotesPreview).toContainText("image/png");
	await expect(documentDialog.getByLabel("Alt text", { exact: true })).toHaveValue(
		"Green field notes cover"
	);
	await documentDialog.getByRole("button", { name: "Back to results" }).click();
	relationDialog = page.getByRole("dialog", { name: "Select cover" });
	await relationDialog.getByRole("searchbox", { name: "Search Media" }).fill("");
	await relationDialog.getByRole("button", { name: "Create New" }).click();
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
	const discardFileDialog = page.getByRole("alertdialog", { name: "Discard changes?" });
	await expect(discardFileDialog).toBeVisible();
	await discardFileDialog.getByRole("button", { name: "Keep editing" }).click();
	await documentDialog.getByLabel("Alt text", { exact: true }).fill("Inline cover");
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
	await documentDialog.getByRole("button", { name: "Save", exact: true }).click();
	await expect(documentDialog.getByRole("button", { name: "Back to results" })).toBeDisabled();
	await expect(
		documentDialog.getByRole("button", { name: "Close relationship browser" })
	).toBeDisabled();
	releaseInlineUpload();
	relationDialog = page.getByRole("dialog", { name: "Select cover" });
	const inlineCoverRow = relationDialog
		.getByRole("table", { name: "Results" })
		.getByRole("row")
		.filter({ hasText: "inline-cover.png" });
	await expect(inlineCoverRow).toHaveAttribute("data-selected", "true");
	await page.unroute(inlineUploadURL);
	await inlineCoverRow.getByRole("button", { name: "inline-cover.png", exact: true }).click();
	await expect(relationDialog).toBeHidden();
	await expect(page.getByText("inline-cover.png", { exact: true })).toBeVisible();

	await page
		.locator('[data-field-path="relatedPosts"]')
		.getByRole("combobox", { name: "Related posts", exact: true })
		.click();
	await page.getByRole("button", { name: /^Browse all posts/ }).click();
	relationDialog = page.getByRole("dialog", { name: "Add related posts" });
	await expect(
		relationDialog.getByRole("checkbox", { name: "Select Relationship field notes" })
	).toBeChecked();
	await relationDialog.getByRole("button", { name: "Create New" }).click();
	documentDialog = page.getByRole("dialog", { name: "New post" });
	await documentDialog.getByLabel("Title — English", { exact: true }).fill("Inline related post");
	await documentDialog.getByRole("button", { name: "Save", exact: true }).click();
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
	await documentDialog.getByRole("button", { name: "Save", exact: true }).click();
	relationDialog = page.getByRole("dialog", { name: "Add related posts" });
	await expect(
		relationDialog.getByRole("checkbox", { name: "Select Inline related post" })
	).toBeChecked();
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
	await page.getByRole("link", { name: /^New / }).click();
	await expect(page.getByRole("navigation", { name: "Document views" })).toHaveCount(0);
	await page.goto("/admin/collections/posts/create/api?locale=en");
	await expect(page).toHaveURL(/\/admin\/collections\/posts\/create\/api(?:\?locale=en)?$/);
	await expect(page.getByRole("region", { name: "API", exact: true })).toBeVisible();
	await expect(
		page
			.getByRole("status")
			.filter({ hasText: "This endpoint becomes runnable after the document is saved." })
	).toBeVisible();
	const jsonData = page.getByLabel("JSON data");
	await expect(jsonData).toBeVisible();
	const jsonRoot = jsonData.getByRole("button", { name: "JSON", exact: true });
	await jsonRoot.focus();
	await jsonRoot.press("Space");
	await expect(jsonRoot).toHaveAttribute("aria-expanded", "false");
	await jsonRoot.press("Enter");
	await expect(jsonRoot).toHaveAttribute("aria-expanded", "true");
	await expect(page.getByRole("button", { name: "Copy JSON" })).toBeVisible();
	await page.reload();
	await expect(page.getByRole("region", { name: "API", exact: true })).toBeVisible();
	await expect(page.getByLabel("JSON data")).toBeVisible();
	await page.getByRole("link", { name: "Edit", exact: true }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts\/create(?:\?locale=en)?$/);

	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});
