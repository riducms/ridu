import { expect, test } from "./fixture";

import { chooseRiduSelect, expectRiduSelectValue } from "./helpers";

test("collection list workspace is schema-driven and persisted", async ({ page }) => {
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in" }).click();
	const collectionNavigation = page.getByRole("navigation", { name: "Admin navigation" });
	await collectionNavigation.waitFor({ state: "visible" });
	await page.goto("/admin/collections/posts");
	await expect(page.getByText("Welcome to Ridu")).toBeVisible();
	const welcomeRow = page.getByRole("row").filter({ hasText: "Welcome to Ridu" });
	await expect(welcomeRow.getByText("Editorial", { exact: true })).toBeVisible();
	await expect(welcomeRow.getByText(/^folders_/)).toHaveCount(0);
	await collectionNavigation.getByRole("link", { name: "Pages", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Pages", exact: true })).toBeVisible();
	await page.getByRole("button", { name: /Filters/ }).click();
	await chooseRiduSelect(page, page.getByLabel("Filter field", { exact: true }), "Title");
	await page.getByLabel("Filter value", { exact: true }).fill("draft that must not cross routes");
	await collectionNavigation.getByRole("link", { name: "Editorial notes", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Editorial notes", exact: true })).toBeVisible();
	await page.getByRole("button", { name: /Filters/ }).click();
	await expectRiduSelectValue(page.getByLabel("Filter field", { exact: true }), "Choose a field");
	await page.keyboard.press("Escape");
	await page.goBack();
	await expect(page.getByRole("heading", { name: "Pages", exact: true })).toBeVisible();
	await page.getByRole("button", { name: /Filters/ }).click();
	await expectRiduSelectValue(page.getByLabel("Filter field", { exact: true }), "Choose a field");
	await page.keyboard.press("Escape");
	await collectionNavigation.getByRole("link", { name: "Posts", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: "Reading time" })).toBeVisible();
	await expect(welcomeRow.getByText(/^\d+ min$/)).toBeVisible();
	const postSearch = page.getByPlaceholder(/^Search by /);
	const failedSearch = "no document can match this extraction proof";
	let failFilteredList = true;
	const postListEndpoint = (url: URL) => url.pathname === "/api/collections/posts";
	await page.route(postListEndpoint, async (route) => {
		const url = new URL(route.request().url());
		if (route.request().method() === "GET" && url.searchParams.has("where") && failFilteredList) {
			await route.fulfill({ status: 503 });
			return;
		}
		await route.fallback();
	});
	const failedListResponse = page.waitForResponse(
		(response) =>
			new URL(response.url()).pathname === "/api/collections/posts" &&
			response.request().method() === "GET" &&
			new URL(response.url()).searchParams.has("where")
	);
	await postSearch.fill(failedSearch);
	expect((await failedListResponse).status()).toBe(503);
	await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
	await expect(page.getByText("Welcome to Ridu", { exact: true })).toBeVisible();
	failFilteredList = false;
	await page.getByRole("button", { name: "Retry" }).click();
	await expect(page.getByRole("heading", { name: "Nothing matches" })).toBeVisible();
	await page.getByRole("button", { name: "Clear filters" }).click();
	await expect(postSearch).toBeFocused();
	await expect(page.getByText("Welcome to Ridu", { exact: true })).toBeVisible();
	await page.unroute(postListEndpoint);

	await page.getByRole("button", { name: "Columns" }).click();
	await page.getByRole("checkbox", { name: "Show Summary column" }).click();
	await page.getByRole("checkbox", { name: "Show ID column" }).click();
	await page.getByRole("checkbox", { name: "Show created column" }).click();
	await page.keyboard.press("Escape");
	await expect(page.getByRole("columnheader", { name: "Summary" })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: "ID", exact: true })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: "Created", exact: true })).toBeVisible();
	await expect(welcomeRow.getByText(/^posts_/)).toBeVisible();

	let releaseSortedList!: () => void;
	let markSortedListStarted!: () => void;
	const sortedListRelease = new Promise<void>((resolve) => (releaseSortedList = resolve));
	const sortedListStarted = new Promise<void>((resolve) => (markSortedListStarted = resolve));
	await page.route(/\/api\/collections\/posts(?:\?|$)/, async (route) => {
		const url = new URL(route.request().url());
		if (route.request().method() !== "GET" || url.searchParams.get("sort") !== "title") {
			await route.fallback();
			return;
		}
		markSortedListStarted();
		await sortedListRelease;
		await route.continue();
	});
	const sortByTitle = page.getByRole("button", { name: "Sort by Title" });
	await sortByTitle.focus();
	await sortByTitle.press("Enter");
	await sortedListStarted;
	const listResults = page.locator('[data-slot="collection-list-results"]');
	await expect(listResults).toHaveAttribute("aria-busy", "true");
	await expect(page.getByText("Welcome to Ridu", { exact: true })).toBeVisible();
	await expect(welcomeRow.getByRole("checkbox")).toBeDisabled();
	await expect(sortByTitle).toBeFocused();
	releaseSortedList();
	await expect(page).toHaveURL(/sort=title/);
	await expect(listResults).toHaveAttribute("aria-busy", "false");
	await expect(page.getByRole("columnheader", { name: "Sort by Title" })).toHaveAttribute(
		"aria-sort",
		"ascending"
	);
	await expect(sortByTitle).toBeFocused();
	await page.unroute(/\/api\/collections\/posts(?:\?|$)/);

	await page.getByRole("button", { name: /Filters/ }).click();
	await chooseRiduSelect(
		page,
		page.getByLabel("Filter field", { exact: true }),
		"Reading time (minutes)"
	);
	await chooseRiduSelect(
		page,
		page.getByLabel("Filter operator", { exact: true }),
		"is greater than"
	);
	await page.getByLabel("Filter value", { exact: true }).fill("5");
	await page.getByRole("button", { name: "Add filter" }).click();
	await page.keyboard.press("Escape");
	await expect(page.getByText("Welcome to Ridu")).toBeVisible();
	await expect(page.getByText("Relationship field notes")).toHaveCount(0);
	await expect(page).toHaveURL(/filters=/);
	const filteredURL = page.url();
	await page.goBack();
	await expect(page).not.toHaveURL(/filters=/);
	await expect(page.getByText("Relationship field notes")).toBeVisible();
	await page.goForward();
	await expect(page).toHaveURL(filteredURL);
	await expect(page.getByText("Relationship field notes")).toHaveCount(0);

	await page.getByRole("button", { name: "More", exact: true }).click();
	await chooseRiduSelect(page, page.getByLabel("Documents per page"), "10 per page");
	await page.keyboard.press("Escape");
	await page.getByRole("button", { name: "Saved views" }).click();
	await page.getByLabel("Saved view name").fill("Long-form posts");
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect(page.getByRole("button", { name: "Long-form posts", exact: true })).toBeVisible();

	await page.goto("/admin/collections/posts");
	await expect(page.getByRole("columnheader", { name: "Summary" })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: "ID", exact: true })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: "Created", exact: true })).toBeVisible();
	await page.getByRole("button", { name: "More", exact: true }).click();
	await expectRiduSelectValue(page.getByLabel("Documents per page"), "10 per page");
	await page.keyboard.press("Escape");
	await page.getByRole("button", { name: "Saved views" }).click();
	await page.getByRole("button", { name: "Long-form posts", exact: true }).click();
	await expect(page).toHaveURL(/filters=/);
	await expect(page).toHaveURL(/sort=title/);
	await expect(page).toHaveURL(/limit=10/);

	await page.goto("/admin/collections/posts");
	await page.getByRole("button", { name: "Columns" }).click();
	await expect(page.getByRole("checkbox", { name: "Show Seo > Description column" })).toBeVisible();
	await page.getByRole("checkbox", { name: "Show Seo > Description column" }).click();
	await page.keyboard.press("Escape");
	await expect(page.getByRole("columnheader", { name: "Sort by Seo > Description" })).toBeVisible();

	await page.getByRole("button", { name: /Filters/ }).click();
	await chooseRiduSelect(page, page.getByLabel("Filter field", { exact: true }), "Links > Label");
	await chooseRiduSelect(page, page.getByLabel("Filter operator", { exact: true }), "equals");
	await page.getByLabel("Filter value", { exact: true }).fill("Payload parity roadmap");
	await page.getByRole("button", { name: "Add filter" }).click();
	await page.keyboard.press("Escape");
	await expect(page.getByText("Relationship field notes")).toBeVisible();
	await expect(page.getByText("Welcome to Ridu")).toHaveCount(0);

	const postsWorkspacePattern = "**/api/preferences/collection%3Aposts%3Aworkspace";
	let workspaceWriteCount = 0;
	let releaseFirstWorkspaceWrite!: () => void;
	let markFirstWorkspaceWriteStarted!: () => void;
	let markSecondWorkspaceWriteStarted!: (value: Record<string, unknown>) => void;
	const firstWorkspaceWriteRelease = new Promise<void>(
		(resolve) => (releaseFirstWorkspaceWrite = resolve)
	);
	const firstWorkspaceWriteStarted = new Promise<void>(
		(resolve) => (markFirstWorkspaceWriteStarted = resolve)
	);
	const secondWorkspaceWriteStarted = new Promise<Record<string, unknown>>(
		(resolve) => (markSecondWorkspaceWriteStarted = resolve)
	);
	await page.route(postsWorkspacePattern, async (route) => {
		if (route.request().method() !== "PUT") {
			await route.continue();
			return;
		}
		workspaceWriteCount += 1;
		if (workspaceWriteCount === 1) {
			markFirstWorkspaceWriteStarted();
			await firstWorkspaceWriteRelease;
		} else {
			markSecondWorkspaceWriteStarted(
				(route.request().postDataJSON() as { value: Record<string, unknown> }).value
			);
		}
		await route.continue();
	});
	await page.getByRole("button", { name: "Columns" }).click();
	await page.getByRole("checkbox", { name: "Show updated column" }).click();
	await firstWorkspaceWriteStarted;
	await page.getByRole("checkbox", { name: "Show created column" }).click();
	await page.waitForTimeout(100);
	expect(workspaceWriteCount).toBe(1);
	releaseFirstWorkspaceWrite();
	const latestWorkspace = await secondWorkspaceWriteStarted;
	expect(latestWorkspace).toMatchObject({ showCreated: false, showUpdated: false });
	await page.keyboard.press("Escape");
	await page.unroute(postsWorkspacePattern);

	const eventsWorkspacePattern = "**/api/preferences/collection%3Aevents%3Aworkspace";
	let releaseOldWorkspace!: () => void;
	let markOldWorkspaceStarted!: () => void;
	let markOldWorkspaceFinished!: () => void;
	const oldWorkspaceRelease = new Promise<void>((resolve) => (releaseOldWorkspace = resolve));
	const oldWorkspaceStarted = new Promise<void>((resolve) => (markOldWorkspaceStarted = resolve));
	const oldWorkspaceFinished = new Promise<void>((resolve) => (markOldWorkspaceFinished = resolve));
	await page.route(eventsWorkspacePattern, async (route) => {
		if (route.request().method() !== "PUT") {
			await route.continue();
			return;
		}
		markOldWorkspaceStarted();
		await oldWorkspaceRelease;
		const response = await route.fetch();
		await route.fulfill({ response });
		markOldWorkspaceFinished();
	});
	await collectionNavigation.getByRole("link", { name: "Events", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	await page.getByRole("button", { name: "Columns" }).click();
	await page.getByRole("checkbox", { name: "Show ID column" }).click();
	await oldWorkspaceStarted;
	await collectionNavigation.getByRole("link", { name: "Editorial notes", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Editorial notes", exact: true })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: "ID", exact: true })).toHaveCount(0);
	await page.getByRole("button", { name: "More", exact: true }).click();
	await expectRiduSelectValue(page.getByLabel("Documents per page"), "25 per page");
	releaseOldWorkspace();
	await oldWorkspaceFinished;
	await expect(page.getByRole("columnheader", { name: "ID", exact: true })).toHaveCount(0);
	await expectRiduSelectValue(page.getByLabel("Documents per page"), "25 per page");
	await collectionNavigation.getByRole("link", { name: "Events", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: "ID", exact: true })).toBeVisible();
	await page.unroute(eventsWorkspacePattern);

	let resetWorkspaceWriteCount = 0;
	let releaseResetWorkspaceWrite!: () => void;
	let markResetWorkspaceWriteStarted!: () => void;
	let markResetWorkspaceWritesFinished!: () => void;
	const resetWorkspaceWriteRelease = new Promise<void>(
		(resolve) => (releaseResetWorkspaceWrite = resolve)
	);
	const resetWorkspaceWriteStarted = new Promise<void>(
		(resolve) => (markResetWorkspaceWriteStarted = resolve)
	);
	const resetWorkspaceWritesFinished = new Promise<void>(
		(resolve) => (markResetWorkspaceWritesFinished = resolve)
	);
	await page.route(eventsWorkspacePattern, async (route) => {
		if (route.request().method() !== "PUT") {
			await route.continue();
			return;
		}
		const writeNumber = ++resetWorkspaceWriteCount;
		if (writeNumber === 1) {
			markResetWorkspaceWriteStarted();
			await resetWorkspaceWriteRelease;
		}
		const response = await route.fetch();
		await route.fulfill({ response });
		if (writeNumber === 2) markResetWorkspaceWritesFinished();
	});
	await page.getByRole("button", { name: "Columns" }).click();
	await page.getByRole("checkbox", { name: "Show created column" }).click();
	await resetWorkspaceWriteStarted;
	await page.getByRole("checkbox", { name: "Show updated column" }).click();
	await page.waitForTimeout(100);
	expect(resetWorkspaceWriteCount).toBe(1);
	await page.keyboard.press("Escape");
	await page.getByRole("button", { name: /Open account menu for Ridu Editor/ }).click();
	await page.getByRole("link", { name: "Profile & preferences" }).click();
	await expect(page).toHaveURL(/\/admin\/account(?:\?locale=en)?$/);

	let preferenceResetCount = 0;
	let markPreferenceResetStarted!: () => void;
	const preferenceResetStarted = new Promise<void>(
		(resolve) => (markPreferenceResetStarted = resolve)
	);
	await page.route("**/api/preferences", async (route) => {
		if (route.request().method() !== "DELETE") {
			await route.fallback();
			return;
		}
		preferenceResetCount += 1;
		markPreferenceResetStarted();
		await route.continue();
	});
	await page.getByRole("button", { name: "Reset all preferences" }).click();
	await page.waitForTimeout(100);
	expect(preferenceResetCount).toBe(0);
	releaseResetWorkspaceWrite();
	await resetWorkspaceWritesFinished;
	await preferenceResetStarted;
	await expect(page.getByText("Admin preferences reset.")).toBeVisible();
	expect(resetWorkspaceWriteCount).toBe(2);
	await page.unroute("**/api/preferences");
	await page.unroute(eventsWorkspacePattern);

	await collectionNavigation.getByRole("link", { name: "Events", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: "ID", exact: true })).toHaveCount(0);
	await expect(page.getByRole("columnheader", { name: "Created", exact: true })).toHaveCount(0);
	await expect(page.getByRole("columnheader", { name: "Updated", exact: true })).toBeVisible();
	await collectionNavigation.getByRole("link", { name: "Posts", exact: true }).click();
	await page.getByRole("button", { name: "Saved views" }).click();
	await expect(page.getByRole("button", { name: "Long-form posts", exact: true })).toHaveCount(0);
	await page.keyboard.press("Escape");
	await page.setViewportSize({ width: 390, height: 844 });
	const tableContainer = page.locator(
		'[data-slot="collection-list-results"] [data-slot="table-container"]'
	);
	await expect(tableContainer).toBeVisible();
	expect(
		await tableContainer.evaluate((element) => element.scrollWidth > element.clientWidth)
	).toBe(true);
	expect(
		await page.evaluate(
			() => document.documentElement.scrollWidth <= document.documentElement.clientWidth
		)
	).toBe(true);
});

test("collection selection can expand from the visible page to every filtered document", async ({
	page,
}) => {
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in" }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor({ state: "visible" });

	const total = await page.evaluate(async () => {
		const listResponse = await fetch("/api/collections/posts?limit=100");
		if (!listResponse.ok) throw new Error(`list posts: ${listResponse.status}`);
		const list = (await listResponse.json()) as {
			docs: { id: string; title?: string }[];
			pagination: { totalDocs: number };
		};
		const source =
			list.docs.find((document) => document.title === "Welcome to Ridu") ?? list.docs[0];
		if (source === undefined) throw new Error("a source post is required");
		const required = Math.max(0, 11 - list.pagination.totalDocs);
		for (let index = 0; index < required; index += 1) {
			const response = await fetch(`/api/collections/posts/${source.id}/duplicate`, {
				method: "POST",
				headers: { "content-type": "application/json" },
				body: JSON.stringify({
					title: `Selection fixture ${index + 1}`,
					slug: `selection-fixture-${index + 1}`,
				}),
			});
			if (!response.ok) throw new Error(`duplicate post: ${response.status}`);
		}
		return list.pagination.totalDocs + required;
	});

	await page.goto("/admin/collections/posts?limit=10");
	await expect(page.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();
	await page.waitForLoadState("networkidle");
	await page.getByRole("checkbox", { name: "Select all rows" }).click();
	const bulkActions = page.getByRole("region", { name: "Bulk actions" });
	await expect(bulkActions).toContainText("10 selected");
	const promotionRequests: string[] = [];
	const ordinarySelectionWork: string[] = [];
	const observePromotion = (request: import("@playwright/test").Request) => {
		const url = new URL(request.url());
		if (url.pathname === "/api/access/collections/posts/selection") {
			promotionRequests.push(url.pathname);
		} else if (
			url.pathname === "/api/access/collections/posts" ||
			(url.pathname === "/api/collections/posts" && request.method() === "GET")
		) {
			ordinarySelectionWork.push(url.pathname);
		}
	};
	page.on("request", observePromotion);
	const promotion = page.waitForResponse(
		(response) =>
			new URL(response.url()).pathname === "/api/access/collections/posts/selection" &&
			response.request().method() === "POST"
	);
	const promotionButton = bulkActions.getByRole("button", { name: `Select all (${total})` });
	await promotionButton.focus();
	await promotionButton.press("Enter");
	await promotion;
	await expect(bulkActions).toContainText(`${total} selected`);
	await expect(bulkActions.getByRole("status")).toHaveText(`${total} selected`);
	await expect(bulkActions.getByRole("button", { name: `All ${total} selected` })).toBeFocused();
	expect(promotionRequests).toEqual(["/api/access/collections/posts/selection"]);
	expect(ordinarySelectionWork).toEqual([]);
	page.off("request", observePromotion);
	await page.getByRole("button", { name: "Next page" }).click();
	await expect(bulkActions).toContainText(`${total} selected`);
	await expect(bulkActions.getByRole("button", { name: "Edit" })).toBeEnabled();
	await bulkActions.getByRole("button", { name: "Clear selection" }).click();
	await expect(bulkActions).toBeHidden();

	await page.getByRole("checkbox", { name: "Select all rows" }).click();
	let releaseManualSelection!: () => void;
	const manualSelectionHeld = new Promise<void>((resolve) => {
		releaseManualSelection = resolve;
	});
	await page.route("**/api/access/collections/posts/selection*", async (route) => {
		await manualSelectionHeld;
		try {
			await route.continue();
		} catch {
			// The controller intentionally aborted the superseded request.
		}
	});
	const manuallyCanceledPromotion = page.waitForEvent("requestfailed", {
		predicate: (request) =>
			new URL(request.url()).pathname === "/api/access/collections/posts/selection",
	});
	await bulkActions.getByRole("button", { name: `Select all (${total})` }).click();
	await expect(bulkActions).toContainText("Selecting…");
	await page.getByRole("checkbox", { name: "Select all rows" }).click();
	await manuallyCanceledPromotion;
	await expect(bulkActions).toBeHidden();
	releaseManualSelection();
	await page.unroute("**/api/access/collections/posts/selection*");

	await page.getByRole("checkbox", { name: "Select all rows" }).click();
	let releaseSelection!: () => void;
	const selectionHeld = new Promise<void>((resolve) => {
		releaseSelection = resolve;
	});
	await page.route("**/api/access/collections/posts/selection*", async (route) => {
		await selectionHeld;
		try {
			await route.continue();
		} catch {
			// The controller intentionally aborted the superseded request.
		}
	});
	const canceledPromotion = page.waitForEvent("requestfailed", {
		predicate: (request) =>
			new URL(request.url()).pathname === "/api/access/collections/posts/selection",
	});
	await bulkActions.getByRole("button", { name: `Select all (${total})` }).click();
	await expect(bulkActions).toContainText("Selecting…");
	let releaseList!: () => void;
	const listHeld = new Promise<void>((resolve) => {
		releaseList = resolve;
	});
	await page.route(/\/api\/collections\/posts(?:\?|$)/, async (route) => {
		if (route.request().method() === "GET") await listHeld;
		try {
			await route.continue();
		} catch {
			// The superseded list request may be aborted while the route is held.
		}
	});
	await page.getByPlaceholder(/^Search by /).fill("query that supersedes selection");
	await expect(bulkActions).toBeHidden();
	const stalePageSelection = page.getByRole("checkbox", { name: "Select all rows" });
	await expect(stalePageSelection).toBeDisabled();
	await stalePageSelection.evaluate((element) => (element as HTMLElement).click());
	await expect(bulkActions).toBeHidden();
	releaseList();
	releaseSelection();
	await canceledPromotion;
	await page.unroute(/\/api\/collections\/posts(?:\?|$)/);
	await page.unroute("**/api/access/collections/posts/selection*");
	await expect(bulkActions).toBeHidden();
});
