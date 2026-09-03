import { expect, test } from "./fixture";

import {
	chooseRiduSelect,
	documentSaveButton,
	expectRiduDateTimeControl,
	selectRiduCalendarDay,
} from "./helpers";

test("document locks offer read-only inspection and explicit takeover", async ({ browser }) => {
	test.setTimeout(60_000);
	const adminContext = await browser.newContext();
	const editorContext = await browser.newContext();
	const adminPage = await adminContext.newPage();
	const editorPage = await editorContext.newPage();
	try {
		await adminPage.goto("/admin/login");
		await adminPage.getByLabel("Email address").fill("admin@riducms.test");
		await adminPage.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-admin");
		await adminPage.getByRole("button", { name: "Sign in" }).click();
		await adminPage
			.getByRole("navigation", { name: "Admin navigation" })
			.waitFor({ state: "visible" });
		const postID = await adminPage.evaluate(async () => {
			const response = await fetch("/api/collections/posts?limit=100");
			if (!response.ok) throw new Error(`list posts: ${response.status}`);
			const result = (await response.json()) as { docs: { id: string; title?: string }[] };
			return result.docs.find((document) => document.title === "Relationship field notes")?.id;
		});
		expect(postID).toBeDefined();
		const adminLockAcquired = adminPage.waitForResponse(
			(response) =>
				response.request().method() === "POST" &&
				response.url().endsWith(`/api/collections/posts/${postID}/lock`)
		);
		await adminPage.goto(`/admin/collections/posts/${postID}`);
		await adminLockAcquired;
		await expect(adminPage.locator('input[name="title"]')).toHaveValue("Relationship field notes");
		const existingTakeover = adminPage.getByRole("button", { name: "Take over", exact: true });
		if (await existingTakeover.isVisible()) {
			await existingTakeover.click();
			await expect(adminPage.getByText("Document lock taken over.")).toBeVisible();
		}

		await editorPage.goto("/admin/login");
		await editorPage.getByLabel("Email address").fill("editor@riducms.test");
		await editorPage.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
		await editorPage.getByRole("button", { name: "Sign in" }).click();
		await editorPage
			.getByRole("navigation", { name: "Admin navigation" })
			.waitFor({ state: "visible" });
		await editorPage.goto(`/admin/collections/posts/${postID}`);
		const lockDialog = editorPage.getByRole("dialog");
		await expect(lockDialog.getByText("Document locked", { exact: true })).toBeVisible();
		await expect(lockDialog).toContainText("admin@riducms.test is currently editing");
		await lockDialog.getByRole("button", { name: "View read-only" }).click();
		await expect(
			editorPage.getByText(/admin@riducms\.test is editing this document/)
		).toBeVisible();
		await expect(editorPage.locator('input[name="title"]')).toHaveAttribute("readonly");
		const readOnlyAuthor = editorPage.locator('[data-field-path="author"]');
		await readOnlyAuthor.getByRole("button", { name: "Browse users" }).dispatchEvent("click");
		const readOnlyBrowser = editorPage.getByRole("dialog", { name: "Select author" });
		const readOnlyGroup = readOnlyBrowser.getByRole("radiogroup", { name: "Results" });
		await expect(readOnlyGroup).toHaveAttribute("aria-readonly", "true");
		const readOnlyChoice = readOnlyBrowser.getByRole("radio", { checked: true });
		await expect(readOnlyChoice).toBeVisible();
		await expect(readOnlyChoice).toBeEnabled();
		await readOnlyChoice.press("Space");
		await expect(readOnlyChoice).toBeChecked();
		await expect(readOnlyBrowser.getByRole("button", { name: /Edit / }).first()).toBeEnabled();
		await readOnlyBrowser.getByRole("button", { name: "Close relationship browser" }).click();
		await editorPage.getByRole("tab", { name: "Related", exact: true }).click();
		const linksField = editorPage.locator('[data-field-path="links"]');
		await expect(linksField.getByRole("button", { name: "Drag Row 1" })).toBeDisabled();
		await expect(linksField.getByRole("button", { name: "Add row" })).toBeDisabled();
		await expect(linksField.getByLabel("Label", { exact: true })).toHaveAttribute("readonly");
		await expect(documentSaveButton(editorPage)).toBeDisabled();
		await editorPage.getByRole("button", { name: "Take over", exact: true }).first().click();
		await expect(editorPage.getByText("Document lock taken over.")).toBeVisible();
		await expect(editorPage.locator('input[name="title"]')).not.toHaveAttribute("readonly");
		await expect(linksField.getByRole("button", { name: "Drag Row 1" })).toBeEnabled();
		await expect(linksField.getByLabel("Label", { exact: true })).not.toHaveAttribute("readonly");
		await expect(editorPage.getByRole("button", { name: "Take over", exact: true })).toHaveCount(0);
	} finally {
		await adminContext.close();
		await editorContext.close();
	}
});

test("admin presentation follows collection, document, and field access", async ({ page }) => {
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in" }).click();
	const navigation = page.getByRole("navigation", { name: "Admin navigation" });
	await expect(navigation).toBeVisible();

	await navigation.getByRole("link", { name: "Editorial notes", exact: true }).click();
	await page.getByRole("link", { name: /Administrator-only launch notes/ }).click();
	await expect(page.getByLabel("Confidential details", { exact: true })).toHaveCount(0);
	await expect(page.getByRole("button", { name: "Delete document", exact: true })).toHaveCount(0);

	await navigation.getByRole("link", { name: "Redirects", exact: true }).click();
	await page.getByRole("link", { name: /\/old-about/ }).click();
	await expect(page.getByRole("button", { name: "Delete document", exact: true })).toHaveCount(0);
	await page.getByRole("button", { name: "More actions" }).click();
	await expect(page.getByRole("button", { name: "Duplicate", exact: true })).toBeVisible();

	await page.evaluate(async () => {
		await fetch("/api/auth/logout", { method: "POST" });
	});
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("api@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-api");
	await page.getByRole("button", { name: "Sign in" }).click();
	await expect(page.getByRole("alert")).toContainText(
		"This account does not have access to the admin."
	);
	await page.getByLabel("Email address").fill("demo@riducms.local");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-demo");
	await page.getByRole("button", { name: "Sign in" }).click();
	await expect(navigation).toBeVisible();
	await expect(navigation.getByRole("link", { name: "Redirects", exact: true })).toHaveCount(0);
	await expect(navigation.getByRole("link", { name: "Media", exact: true })).toBeVisible();
	await navigation.getByRole("link", { name: "Pages", exact: true }).click();
	await page.getByRole("link", { name: "About Ridu", exact: true }).click();
	await expect(page.getByLabel("Title", { exact: true })).toHaveValue("About Ridu");
	await expect(page.getByRole("link", { name: "Versions", exact: true })).toHaveCount(0);
	await page.goto("/admin/collections/pages/pages_10/versions");
	await expect(page.getByText("operation is not permitted")).toBeVisible();
});

test("date controls preserve configured time-of-day authoring and list presentation", async ({
	page,
}) => {
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in" }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor({ state: "visible" });
	await page.goto("/admin/collections/events");
	await expect(page.getByText("Ridu contributor workshop", { exact: true })).toBeVisible();

	await page.getByRole("button", { name: "Columns" }).click();
	const startsColumn = page.getByRole("checkbox", { name: "Show Starts at column" });
	const endsColumn = page.getByRole("checkbox", { name: "Show Ends at column" });
	if ((await startsColumn.getAttribute("aria-checked")) !== "true") await startsColumn.click();
	if ((await endsColumn.getAttribute("aria-checked")) !== "true") await endsColumn.click();
	await page.keyboard.press("Escape");
	await expect(page.getByRole("columnheader", { name: "Sort by Starts at" })).toBeVisible();
	const eventRow = page.getByRole("row").filter({ hasText: "Ridu contributor workshop" });
	await expect(eventRow).toContainText(/\d{1,2}:\d{2}/);

	await page.getByRole("link", { name: "Ridu contributor workshop", exact: true }).click();
	const startsAt = page.getByLabel("Starts at", { exact: true });
	const endsAt = page.getByLabel("Ends at", { exact: true });
	await expectRiduDateTimeControl(startsAt);
	await expectRiduDateTimeControl(endsAt);
	await expectRiduDateTimeControl(page.getByLabel("Time", { exact: true }).first());
	const originalStartsAtText = await startsAt.textContent();
	await startsAt.locator('[data-segment="minute"]').press("ArrowUp");
	const nextStartsAtText = await startsAt.textContent();
	expect(nextStartsAtText).not.toBe(originalStartsAtText);
	const saveRequest = page.waitForRequest(
		(request) =>
			new URL(request.url()).pathname.startsWith("/api/collections/events/") &&
			request.method() === "PATCH"
	);
	await documentSaveButton(page).click();
	const savedRequest = await saveRequest;
	expect((savedRequest.postDataJSON() as { startsAt?: unknown }).startsAt).toMatch(
		/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/
	);
	await expect(page.getByText("Updated successfully.")).toBeVisible();
	await page.reload();
	await expect(startsAt).toHaveText(nextStartsAtText ?? "");

	await page
		.getByRole("navigation", { name: "Breadcrumb" })
		.getByRole("link", { name: "Events", exact: true })
		.click();
	await expect(
		page.getByRole("row").filter({ hasText: "Ridu contributor workshop" })
	).toContainText(/\d{1,2}:\d{2}/);
	await page.getByRole("button", { name: "Filters" }).click();
	await chooseRiduSelect(page, page.getByLabel("Filter field", { exact: true }), "Starts at");
	const filterDateValue = page.getByLabel("Filter value", { exact: true });
	await expectRiduDateTimeControl(filterDateValue);
	await selectRiduCalendarDay(page, filterDateValue);
	await page.keyboard.press("Escape");
	await expect(page.getByText("Ridu contributor workshop", { exact: true })).toBeVisible();

	await page
		.getByRole("checkbox", { name: "Select Ridu contributor workshop", exact: true })
		.check();
	await page
		.getByRole("region", { name: "Bulk actions" })
		.getByRole("button", { name: "Edit", exact: true })
		.click();
	const bulkEditDialog = page.getByRole("dialog", { name: "Edit 1 selected document" });
	await chooseRiduSelect(page, bulkEditDialog.getByLabel("Field"), "Starts at");
	const bulkDateValue = bulkEditDialog.getByLabel("Value");
	await expectRiduDateTimeControl(bulkDateValue);
	await selectRiduCalendarDay(page, bulkDateValue);
	const bulkRequest = page.waitForRequest(
		(request) =>
			new URL(request.url()).pathname === "/api/collections/events/bulk" &&
			request.method() === "POST"
	);
	await bulkEditDialog.getByRole("button", { name: "Apply change" }).click();
	const request = await bulkRequest;
	expect(request.postDataJSON()).toMatchObject({
		action: "update",
		data: { startsAt: expect.stringMatching(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/) },
	});
	await expect(bulkEditDialog).toBeHidden();
	await expect(page.getByRole("region", { name: "Bulk actions" })).toBeHidden();
});

test("inverse joins expose their own table controls and access-checked mutations", async ({
	page,
}) => {
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in" }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor({ state: "visible" });
	await page.goto("/admin/collections/categories");
	await page.getByRole("link", { name: "News", exact: true }).click();

	const join = page.locator('[data-join-field="posts"]');
	await expect(join.getByRole("columnheader", { name: "Sort by Title" })).toBeVisible();
	await expect(join.getByRole("columnheader", { name: "Sort by Status" })).toBeVisible();
	await expect(join.getByRole("columnheader", { name: "Sort by Author" })).toBeVisible();
	await expect(join.getByRole("columnheader", { name: "Sort by Updated At" })).toBeVisible();
	await expect(join).toContainText("Welcome to Ridu");
	await join.getByRole("button", { name: "Sort by Title" }).click();
	await join.getByRole("button", { name: "Sort by Title" }).click();

	await join.getByRole("button", { name: "Manage relationships" }).click();
	const relationshipNotes = page
		.getByRole("group", { name: "Results" })
		.getByRole("checkbox")
		.filter({ hasText: "Relationship field notes" });
	if ((await relationshipNotes.getAttribute("aria-checked")) !== "true") {
		await relationshipNotes.click();
	}
	let releaseJoin: (() => void) | undefined;
	const joinGate = new Promise<void>((resolve) => {
		releaseJoin = resolve;
	});
	await page.route(/\/joins\/posts(?:\?|$)/, async (route) => {
		await joinGate;
		await route.continue();
	});
	const additionRequest = page.waitForRequest(
		(request) => request.url().includes("/joins/posts") && request.method() === "PATCH"
	);
	const additionResponse = page.waitForResponse(
		(response) => response.url().includes("/joins/posts") && response.request().method() === "PATCH"
	);
	await page.getByRole("button", { name: "Add 2 selected" }).click();
	await expect(page.getByRole("button", { name: "Applying…" })).toBeDisabled();
	await expect(page.getByRole("button", { name: "Cancel", exact: true })).toBeDisabled();
	await expect(relationshipNotes).toBeDisabled();
	releaseJoin?.();
	const addition = await additionRequest;
	expect(addition.postDataJSON()).toMatchObject({ additions: [expect.any(String)], removals: [] });
	await additionResponse;
	await expect(join).toContainText("2 related Posts");
	await expect(join).toContainText("Relationship field notes");

	await join.getByRole("button", { name: "Manage relationships" }).click();
	const linkedNotes = page
		.getByRole("group", { name: "Results" })
		.getByRole("checkbox")
		.filter({ hasText: "Relationship field notes" });
	if ((await linkedNotes.getAttribute("aria-checked")) === "true") await linkedNotes.click();
	const removalRequest = page.waitForRequest(
		(request) => request.url().includes("/joins/posts") && request.method() === "PATCH"
	);
	await page.getByRole("button", { name: "Add 1 selected" }).click();
	const removal = await removalRequest;
	expect(removal.postDataJSON()).toMatchObject({ additions: [], removals: [expect.any(String)] });
	await expect(join).toContainText("1 related Posts");
	await expect(join).not.toContainText("Relationship field notes");

	await join.getByRole("button", { name: "Manage relationships" }).click();
	const finalRelationship = page
		.getByRole("group", { name: "Results" })
		.getByRole("checkbox")
		.filter({ hasText: "Welcome to Ridu" });
	await finalRelationship.click();
	const finalRemovalRequest = page.waitForRequest(
		(request) => request.url().includes("/joins/posts") && request.method() === "PATCH"
	);
	await page.getByRole("button", { name: "Add 0 selected" }).click();
	const finalRemoval = await finalRemovalRequest;
	expect(finalRemoval.postDataJSON()).toMatchObject({
		additions: [],
		removals: [expect.any(String)],
	});
	await expect(join).toContainText("Showing 0 related Posts");

	await join.getByRole("button", { name: "Manage relationships" }).click();
	const restoredWelcome = page
		.getByRole("group", { name: "Results" })
		.getByRole("checkbox")
		.filter({ hasText: "Welcome to Ridu" });
	if ((await restoredWelcome.getAttribute("aria-checked")) !== "true") {
		await restoredWelcome.click();
	}
	await page.getByRole("button", { name: /^Add(?: 1)? selected$/ }).click();
	await expect(join).toContainText("Welcome to Ridu");
});
