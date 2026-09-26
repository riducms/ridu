import { expect, test } from "./fixture";
import type { AdminPreparedRouteStateV1 } from "@riducms/protocol";

import { chooseRiduSelect, expectRiduSelectValue, loginAsEditor } from "./helpers";

test("collection search keeps focus and caret while superseding pending results", async ({
	page,
}) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	await page.getByRole("checkbox", { name: "Select Welcome to Ridu", exact: true }).click();

	const search = page.getByRole("searchbox", { name: "Search by Title", exact: true });
	const results = page.locator('[data-slot="collection-list-results"]');
	const held = Promise.withResolvers<void>();
	const started = Promise.withResolvers<void>();
	const endpoint = (url: URL) => url.pathname === "/admin/collections/posts";

	await page.route(endpoint, async (route) => {
		if (new URL(route.request().url()).searchParams.get("q") === "W") {
			started.resolve();
			await held.promise;
		}

		try {
			await route.continue();
		} catch {
			// Further typing can abort the held navigation before it reaches the server.
		}
	});

	try {
		await search.click();
		await page.keyboard.insertText("W");
		await started.promise;
		await expect(search).toBeFocused();
		await expect(page.locator("main")).not.toHaveAttribute("inert", "");
		await expect(page.locator("main")).toHaveAttribute("aria-busy", "true");
		await expect(results).toHaveAttribute("inert", "");
		await expect(page.locator(".ridu-list__header")).toHaveAttribute("inert", "");

		// Keyboard input must reach the existing focus; locator.fill would conceal a blur.
		await page.keyboard.insertText("elcom");
		await expect(page).toHaveURL(/q=Welcom(?:&|$)/);
		await expect(search).toHaveValue("Welcom");
		await expect(search).toBeFocused();
		await expect(results).not.toHaveAttribute("inert", "");
		await expect(page.getByRole("link", { name: "Welcome to Ridu", exact: true })).toBeVisible();
		await expect(
			page.getByRole("link", { name: "Relationship field notes", exact: true })
		).toHaveCount(0);

		await page.keyboard.press("Home");
		await page.keyboard.press("ArrowRight");
		await page.keyboard.insertText("X");
		await expect(page).toHaveURL(/q=WXelcom(?:&|$)/);
		await expect(search).toBeFocused();
		await expect
			.poll(() => search.evaluate((input: HTMLInputElement) => input.selectionStart))
			.toBe(2);

		await page.keyboard.press("Backspace");
		await expect(page).toHaveURL(/q=Welcom(?:&|$)/);
		await page.keyboard.press("End");
		await page.keyboard.insertText("e");
		await expect(page).toHaveURL(/q=Welcome(?:&|$)/);
		await expect(search).toBeFocused();

		await page.getByRole("button", { name: "Clear search", exact: true }).click();
		await expect(page).not.toHaveURL(/[?&]q=/);
		await expect(search).toHaveValue("");
		await expect(search).toBeFocused();
	} finally {
		held.resolve();
		await page.unroute(endpoint);
	}
});

test("collection controls compose changes while an earlier query is pending", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	await page.getByRole("button", { name: "More", exact: true }).click();

	const held = Promise.withResolvers<void>();
	const started = Promise.withResolvers<string>();
	const endpoint = (url: URL) => url.pathname === "/admin/collections/posts";

	await page.route(endpoint, async (route) => {
		const search = new URL(route.request().url()).searchParams;
		const folder = search.get("folder");
		if (folder && !search.has("view")) {
			started.resolve(folder);
			await held.promise;
		}

		try {
			await route.continue();
		} catch {
			// The combined query supersedes the held folder-only navigation.
		}
	});

	try {
		await chooseRiduSelect(page, page.getByLabel("Folder", { exact: true }), "Editorial");
		const folder = await started.promise;
		await page.getByRole("button", { name: "Hierarchy", exact: true }).click();
		await expect(page).toHaveURL(
			(url) =>
				url.searchParams.get("folder") === folder && url.searchParams.get("view") === "hierarchy"
		);
		await expectRiduSelectValue(page.getByLabel("Folder", { exact: true }), "Editorial");
		await expect(page.getByRole("button", { name: "Hierarchy", exact: true })).toHaveAttribute(
			"aria-pressed",
			"true"
		);
	} finally {
		held.resolve();
		await page.unroute(endpoint);
	}

	await page.reload();
	await page.getByRole("button", { name: "More", exact: true }).click();
	await expectRiduSelectValue(page.getByLabel("Folder", { exact: true }), "Editorial");
	await expect(page.getByRole("button", { name: "Hierarchy", exact: true })).toHaveAttribute(
		"aria-pressed",
		"true"
	);
});

test("column choices retain rapid toggles while results are pending", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	await page.getByRole("button", { name: "Columns", exact: true }).click();

	const held = Promise.withResolvers<void>();
	const started = Promise.withResolvers<void>();
	const endpoint = (url: URL) => url.pathname === "/admin/collections/posts";
	await page.route(endpoint, async (route) => {
		const columns = new URL(route.request().url()).searchParams.get("columns")?.split(",") ?? [];
		if (columns.includes("summary") && !columns.includes("createdAt")) {
			started.resolve();
			await held.promise;
		}
		try {
			await route.continue();
		} catch {
			// The second toggle supersedes the held navigation.
		}
	});

	try {
		await page.getByRole("button", { name: "Summary", exact: true }).click();
		await started.promise;
		await page.getByRole("button", { name: "Created At", exact: true }).click();
		await expect(page.getByRole("columnheader", { name: /^Summary\b/ })).toBeVisible();
		await expect(page.getByRole("columnheader", { name: /^Created At\b/ })).toBeVisible();
		await expect(page).toHaveURL((url) => {
			const columns = url.searchParams.get("columns")?.split(",") ?? [];
			return columns.includes("summary") && columns.includes("createdAt");
		});
	} finally {
		held.resolve();
		await page.unroute(endpoint);
	}
});

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
	await page.getByRole("button", { name: "Add filter", exact: true }).click();
	await chooseRiduSelect(page, page.getByLabel("Filter field", { exact: true }), "Title");

	await collectionNavigation.getByRole("link", { name: "Editorial notes", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Editorial notes", exact: true })).toBeVisible();
	await page.getByRole("button", { name: /Filters/ }).click();
	await expect(page.getByText("No filters set", { exact: true })).toBeVisible();
	await page.keyboard.press("Escape");
	await page.goBack();
	await expect(page.getByRole("heading", { name: "Pages", exact: true })).toBeVisible();
	await page.getByRole("button", { name: /Filters/ }).click();
	await expect(page.getByText("No filters set", { exact: true })).toBeVisible();
	await page.keyboard.press("Escape");
	await collectionNavigation.getByRole("link", { name: "Posts", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: "Reading time" })).toBeVisible();
	await expect(welcomeRow.getByText(/^\d+ min$/)).toBeVisible();
	const postSearch = page.getByPlaceholder(/^Search by /);
	const failedSearch = "no document can match this extraction proof";
	const postStateEndpoint = (url: URL) => url.pathname === "/admin/collections/posts";
	await page.route(postStateEndpoint, async (route) => {
		const url = new URL(route.request().url());
		if (url.searchParams.get("q") === failedSearch) {
			const response = await route.fetch();
			const state = (await response.json()) as Extract<
				AdminPreparedRouteStateV1,
				{ outcome: "prepared" }
			>;
			expect(state.outcome).toBe("prepared");
			if (state.route.kind !== "collection-list") throw new Error("Expected list data");
			state.route.data.page = {
				error: { code: "internal", status: 503, message: "Forced list failure", issues: [] },
			};
			await route.fulfill({ response, json: state });
			return;
		}
		await route.fallback();
	});
	const failedListResponse = page.waitForResponse(
		(response) =>
			new URL(response.url()).pathname === "/admin/collections/posts" &&
			response.request().method() === "GET" &&
			new URL(response.url()).searchParams.get("q") === failedSearch
	);
	await postSearch.fill(failedSearch);
	expect((await failedListResponse).status()).toBe(200);
	await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
	await expect(page.getByText("Welcome to Ridu", { exact: true })).toBeVisible();
	await page.unroute(postStateEndpoint);
	await page.getByRole("button", { name: "Retry" }).click();
	await expect(page.getByRole("heading", { name: "Nothing matches" })).toBeVisible();
	await page.getByRole("button", { name: "Clear filters" }).click();
	await expect(postSearch).toBeFocused();
	await expect(page.getByText("Welcome to Ridu", { exact: true })).toBeVisible();

	await page.getByRole("button", { name: "Columns" }).click();
	await page.getByRole("button", { name: "Summary", exact: true }).click();
	await page.getByRole("button", { name: "ID", exact: true }).click();
	await page.getByRole("button", { name: "Created At", exact: true }).click();
	await page.getByRole("button", { name: "Updated At", exact: true }).click();
	await page.keyboard.press("Escape");
	await expect(page.getByRole("columnheader", { name: "Summary" })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: /^ID\b/ })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: /^Created At\b/ })).toBeVisible();
	await expect(welcomeRow.getByText(/^posts_/)).toBeVisible();

	let releaseSortedList!: () => void;
	let markSortedListStarted!: () => void;
	const sortedListRelease = new Promise<void>((resolve) => (releaseSortedList = resolve));
	const sortedListStarted = new Promise<void>((resolve) => (markSortedListStarted = resolve));
	await page.route(postStateEndpoint, async (route) => {
		const url = new URL(route.request().url());
		if (route.request().method() !== "GET" || url.searchParams.get("sort") !== "title") {
			await route.fallback();
			return;
		}
		markSortedListStarted();
		await sortedListRelease;
		await route.continue();
	});
	const sortByTitle = page.getByRole("button", { name: "Sort Title ascending" });
	const listResults = page.locator('[data-slot="collection-list-results"]');
	await expect(listResults).not.toHaveAttribute("inert", "");
	await sortByTitle.focus();
	await expect(sortByTitle).toBeFocused();
	await sortByTitle.press("Enter");
	await sortedListStarted;
	await expect(listResults).toHaveAttribute("inert", "");
	await expect(page.locator("main")).toHaveAttribute("aria-busy", "true");
	await expect(page.getByText("Welcome to Ridu", { exact: true })).toBeVisible();
	releaseSortedList();
	await expect(page).toHaveURL(/sort=title/);
	await expect(listResults).toHaveAttribute("aria-busy", "false");
	await expect(
		page
			.getByRole("columnheader")
			.filter({ has: page.getByRole("button", { name: "Sort Title ascending", exact: true }) })
	).toHaveAttribute("aria-sort", "ascending");
	await expect(sortByTitle).toBeFocused();
	await page.unroute(postStateEndpoint);

	await page.getByRole("button", { name: /Filters/ }).click();
	await page.getByRole("button", { name: "Add filter", exact: true }).click();
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

	await chooseRiduSelect(page, page.getByLabel("Documents per page"), "10 per page");
	await page.getByRole("button", { name: "More", exact: true }).click();
	await page.getByLabel("Saved view name").fill("Long-form posts");
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect(page.getByRole("button", { name: "Long-form posts", exact: true })).toBeVisible();

	await page.goto("/admin/collections/posts");
	await expect(page.getByRole("columnheader", { name: "Summary" })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: /^ID\b/ })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: /^Created At\b/ })).toBeVisible();
	await expectRiduSelectValue(page.getByLabel("Documents per page"), "Per Page: 10");
	await page.getByRole("button", { name: "More", exact: true }).click();
	await page.getByRole("button", { name: "Long-form posts", exact: true }).click();
	await expect(page).toHaveURL(/filters=/);
	await expect(page).toHaveURL(/sort=title/);
	await expect(page).toHaveURL(/limit=10/);

	await page.goto("/admin/collections/posts");
	await page.getByRole("button", { name: "Columns" }).click();
	await expect(page.getByRole("button", { name: "SEO > Description", exact: true })).toBeVisible();
	await page.getByRole("button", { name: "SEO > Description", exact: true }).click();
	await page.keyboard.press("Escape");
	await expect(page.getByRole("columnheader", { name: /^SEO > Description/ })).toBeVisible();

	await page.getByRole("button", { name: /Filters/ }).click();
	await page.getByRole("button", { name: "Add filter", exact: true }).click();
	await chooseRiduSelect(page, page.getByLabel("Filter field", { exact: true }), "Links > Label");
	await chooseRiduSelect(page, page.getByLabel("Filter operator", { exact: true }), "equals");
	await page.getByLabel("Filter value", { exact: true }).fill("Payload parity roadmap");

	await page.keyboard.press("Escape");
	await expect(page.getByText("Relationship field notes")).toBeVisible();
	await expect(page.getByText("Welcome to Ridu")).toHaveCount(0);

	const postsWorkspacePattern = "**/api/preferences/collection%3Aposts%3Aworkspace";
	let workspaceWriteCount = 0;
	const workspaceWriteOrder: string[] = [];
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
			workspaceWriteOrder.push("first released");
		} else {
			workspaceWriteOrder.push("second started");
			markSecondWorkspaceWriteStarted(
				(route.request().postDataJSON() as { value: Record<string, unknown> }).value
			);
		}
		await route.continue();
	});
	await page.getByRole("button", { name: "Columns" }).click();
	await page.getByRole("button", { name: "Updated At", exact: true }).click();
	await firstWorkspaceWriteStarted;
	await page.getByRole("button", { name: "Created At", exact: true }).click();
	await expect(page.getByRole("button", { name: "Created At", exact: true })).toHaveAttribute(
		"aria-pressed",
		"false"
	);
	expect(workspaceWriteCount).toBe(1);
	releaseFirstWorkspaceWrite();
	const latestWorkspace = await secondWorkspaceWriteStarted;
	expect(latestWorkspace.columns).toContainEqual({ path: "createdAt", active: false });
	expect(latestWorkspace.columns).toContainEqual({ path: "updatedAt", active: false });
	expect(workspaceWriteOrder).toEqual(["first released", "second started"]);
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
	await page.getByRole("button", { name: "Capacity", exact: true }).click();
	await oldWorkspaceStarted;
	await collectionNavigation.getByRole("link", { name: "Editorial notes", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Editorial notes", exact: true })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: /^Capacity\b/ })).toHaveCount(0);
	await expectRiduSelectValue(page.getByLabel("Documents per page"), "Per Page: 10");
	releaseOldWorkspace();
	await oldWorkspaceFinished;
	await expect(page.getByRole("columnheader", { name: /^Capacity\b/ })).toHaveCount(0);
	await expectRiduSelectValue(page.getByLabel("Documents per page"), "Per Page: 10");
	await collectionNavigation.getByRole("link", { name: "Events", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: /^Capacity\b/ })).toBeVisible();
	await page.unroute(eventsWorkspacePattern);

	const workspaceResetOrder: string[] = [];
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
		workspaceResetOrder.push(`write ${writeNumber} started`);
		if (writeNumber === 1) {
			markResetWorkspaceWriteStarted();
			await resetWorkspaceWriteRelease;
		}
		const response = await route.fetch();
		workspaceResetOrder.push(`write ${writeNumber} complete`);
		await route.fulfill({ response });
		if (writeNumber === 2) markResetWorkspaceWritesFinished();
	});
	await page.getByRole("button", { name: "Columns" }).click();
	await page.getByRole("button", { name: "Created At", exact: true }).click();
	await resetWorkspaceWriteStarted;
	await page.getByRole("button", { name: "Updated At", exact: true }).click();
	await expect(page.getByRole("button", { name: "Updated At", exact: true })).toHaveAttribute(
		"aria-pressed",
		"false"
	);
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
		workspaceResetOrder.push("reset");
		markPreferenceResetStarted();
		await route.continue();
	});
	await page.getByRole("button", { name: "Reset all preferences" }).click();
	await expect(page.getByRole("button", { name: "Resetting…" })).toBeDisabled();
	expect(preferenceResetCount).toBe(0);
	releaseResetWorkspaceWrite();
	await resetWorkspaceWritesFinished;
	await preferenceResetStarted;
	await expect(page.getByText("Admin preferences reset.")).toBeVisible();
	expect(resetWorkspaceWriteCount).toBe(2);
	expect(workspaceResetOrder).toEqual([
		"write 1 started",
		"write 1 complete",
		"write 2 started",
		"write 2 complete",
		"reset",
	]);
	await page.unroute("**/api/preferences");
	await page.unroute(eventsWorkspacePattern);

	await collectionNavigation.getByRole("link", { name: "Events", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: /^ID\b/ })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: /^Created At\b/ })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: /^Updated At\b/ })).toBeVisible();
	await collectionNavigation.getByRole("link", { name: "Posts", exact: true }).click();
	await page.getByRole("button", { name: "More", exact: true }).click();
	await expect(page.getByRole("button", { name: "Long-form posts", exact: true })).toHaveCount(0);
	await page.keyboard.press("Escape");
	await page.setViewportSize({ width: 390, height: 844 });
	const tableContainer = page.locator(
		'[data-slot="collection-list-results"] .ridu-list-table__scroll'
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
	await page.route(/\/admin\/collections\/posts(?:\?|$)/, async (route) => {
		if (route.request().method() === "GET") await listHeld;
		try {
			await route.continue();
		} catch {
			// The superseded list request may be aborted while the route is held.
		}
	});
	await page.getByPlaceholder(/^Search by /).fill("query that supersedes selection");
	await expect(page.locator('[data-slot="collection-list-results"]')).toHaveAttribute("inert", "");
	releaseList();
	await canceledPromotion;
	releaseSelection();
	await page.unroute(/\/admin\/collections\/posts(?:\?|$)/);
	await page.unroute("**/api/access/collections/posts/selection*");
	await expect(bulkActions).toBeHidden();
});

test("collection filters compose OR groups and AND conditions; columns retain hidden order", async ({
	page,
}) => {
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in", exact: true }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor({ state: "visible" });
	await page.goto("/admin/collections/posts");
	const table = page.getByRole("table");
	await page.getByRole("button", { name: "Filters", exact: true }).click();
	await page.getByRole("button", { name: "Add filter", exact: true }).click();
	await page.getByLabel("Filter value", { exact: true }).fill("Welcome to Ridu");
	await expect(table.getByText("Relationship field notes", { exact: true })).toHaveCount(0);
	await expect(table.getByText("Welcome to Ridu", { exact: true })).toBeVisible();
	await expect(page.getByLabel("Filter value", { exact: true })).toBeFocused();
	await page.getByRole("button", { name: "Or", exact: true }).click();
	await page.getByLabel("Filter value", { exact: true }).nth(1).fill("Relationship field notes");
	await expect(table.getByText("Relationship field notes", { exact: true })).toBeVisible();
	await expect(table.getByText("Welcome to Ridu", { exact: true })).toBeVisible();
	await page.getByRole("button", { name: "Add AND condition", exact: true }).nth(1).click();
	await chooseRiduSelect(page, page.getByLabel("Filter field", { exact: true }).nth(2), "Status");
	await chooseRiduSelect(
		page,
		page.getByLabel("Filter value", { exact: true }).nth(2),
		"Published"
	);
	await expect(table.getByText("Relationship field notes", { exact: true })).toHaveCount(0);
	await expect(table.getByText("Welcome to Ridu", { exact: true })).toBeVisible();
	await page.reload();
	await expect(table.getByText("Welcome to Ridu", { exact: true })).toBeVisible();
	await expect(table.getByText("Relationship field notes", { exact: true })).toHaveCount(0);
	const descending = page.getByRole("button", { name: "Sort Title descending", exact: true });
	await descending.focus();
	await descending.press("Enter");
	await expect(table.getByRole("columnheader").filter({ has: descending })).toHaveAttribute(
		"aria-sort",
		"descending"
	);
	await expect(descending).toBeFocused();
	await page.getByRole("button", { name: /^Filters/ }).click();
	const removeLast = page.getByRole("button", { name: "Remove filter 3", exact: true });
	await removeLast.focus();
	await removeLast.press("Enter");
	await expect(table.getByRole("row")).toHaveCount(3);
	await expect(page.getByRole("button", { name: "Remove filter 2", exact: true })).toBeFocused();
	await page.getByRole("button", { name: "Columns", exact: true }).click();
	const title = page.getByRole("button", { name: "Title", exact: true });
	await title.focus();
	await title.press("Enter");
	await expect(title).toHaveAttribute("aria-pressed", "false");
	await expect(title).toBeFocused();
	const handle = page.getByRole("button", { name: "Reorder Title column", exact: true });
	await handle.focus();
	await handle.press("Space");
	await handle.press("ArrowRight");
	await handle.press("Space");
	await expect
		.poll(() => new URL(page.url()).searchParams.get("columns")?.split(",").indexOf("-title"))
		.toBeGreaterThan(0);
	await expect(handle).toBeFocused();
	const reorderedColumns = new URL(page.url()).searchParams.get("columns")!.split(",");
	await title.click();
	await expect(title).toHaveAttribute("aria-pressed", "true");
	await expect(table.getByRole("columnheader").nth(1)).toContainText("Status");
	const activeTitleIndex =
		reorderedColumns
			.filter((column) => !column.startsWith("-") || column === "-title")
			.indexOf("-title") + 1;
	await expect(table.getByRole("columnheader").nth(activeTitleIndex)).toContainText("Title");
	await page.reload();
	await expect(table.getByRole("columnheader").nth(1)).toContainText("Status");
	await expect(table.getByRole("columnheader").nth(activeTitleIndex)).toContainText("Title");

	// Plugin cells retain their interactive content and a canonical document entry point.
	await page.goto("/admin/collections/posts?columns=readingMinutes");
	await expect(table.getByRole("columnheader")).toHaveCount(2);
	await expect(table.getByRole("link", { name: "Welcome to Ridu", exact: true })).toBeVisible();
	await table.getByRole("link", { name: "Welcome to Ridu", exact: true }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts\/[^?]+/);
});
