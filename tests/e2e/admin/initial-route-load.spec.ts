import { expect, test, type Page } from "./fixture";
import type { Locator, Request } from "@playwright/test";
import { chooseRiduSelect, uploadFixturePng } from "./helpers";

const loadingSelector = '[data-ridu-loading-surface],[data-slot="skeleton"]';

test.beforeEach(async ({ page }) => {
	await page.addInitScript((selector) => {
		Reflect.set(window, "__riduLoadingInsertions", [] as string[]);
		new MutationObserver((records) => {
			const insertions = Reflect.get(window, "__riduLoadingInsertions") as string[];
			for (const record of records) {
				for (const node of record.addedNodes) {
					if (!(node instanceof Element)) continue;
					for (const element of [
						...(node.matches(selector) ? [node] : []),
						...node.querySelectorAll(selector),
					]) {
						insertions.push(
							element.getAttribute("data-ridu-loading-surface") ??
								element.getAttribute("data-slot") ??
								element.tagName
						);
					}
				}
			}
		}).observe(document, { childList: true, subtree: true });
	}, loadingSelector);
	await authenticate(page);
});

test("cold load, reload, pathname/query navigation, and history commit prepared lists atomically", async ({
	page,
}) => {
	const browserReads: string[] = [];
	page.on("request", (request) => {
		const url = new URL(request.url());
		if (
			url.pathname === "/api/schema" ||
			url.pathname === "/api/auth/session" ||
			url.pathname.startsWith("/api/admin/collection-list/") ||
			(url.pathname.startsWith("/api/collections/") && request.method() === "GET")
		)
			browserReads.push(`${request.method()} ${url.pathname}${url.search}`);
	});

	const documentResponse = await page.goto(
		"/admin/collections/events?limit=10&page=1&q=Launch&sort=title"
	);
	await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	await expect(page.locator('[data-slot="collection-list-results"]')).toHaveAttribute(
		"aria-busy",
		"false"
	);
	await expectNoLoadingInsertion(page);
	expect(browserReads).toEqual([]);
	await expect.poll(() => readyMarks(page)).toBe(1);
	const htmlBytes = (await documentResponse!.body()).byteLength;
	expect(htmlBytes).toBeLessThan(2 * 1024 * 1024);
	await test.info().attach("initial-route-evidence.json", {
		body: Buffer.from(
			JSON.stringify({
				htmlBytes,
				browserReads: browserReads.length,
				firstContentfulPaint: await firstContentfulPaint(page),
				readyMarks: await readyMarks(page),
			})
		),
		contentType: "application/json",
	});

	await page.reload();
	await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	await expectNoLoadingInsertion(page);
	expect(browserReads).toEqual([]);

	const navigationResponse = page.waitForResponse(
		(response) =>
			new URL(response.url()).pathname === "/admin/collections/editorial-notes" &&
			response.headers()["content-type"] === "application/vnd.ridu.admin-route-state+json"
	);
	await page
		.getByRole("navigation", { name: "Admin navigation" })
		.getByRole("link", { name: "Editorial notes", exact: true })
		.click();
	await expect(page.getByRole("heading", { name: "Editorial notes", exact: true })).toBeVisible();
	await expectNoLoadingInsertion(page);
	expect(browserReads).toEqual([]);
	const compactResponse = await navigationResponse;
	const compact = await compactResponse.json();
	expect(compact.outcome).toBe("prepared");
	expect(compact).not.toHaveProperty("runtime");
	expect(compact.navigation).toHaveProperty("collectionOperations");
	expect(compact.navigation).not.toHaveProperty("manifest");
	expect(compact.navigation).not.toHaveProperty("session");
	// An independent request without the context header establishes the full-size
	// baseline for the exact same URL, outside the browser's navigation/read counts.
	const fullResponse = await page.request.get(compactResponse.url(), {
		headers: { Accept: "application/vnd.ridu.admin-route-state+json" },
	});
	const full = await fullResponse.json();
	expect(full.runtime.manifest).toBeDefined();
	expect(full.route).toEqual(compact.route);
	const fullBytes = (await fullResponse.body()).byteLength;
	const compactBytes = (await compactResponse.body()).byteLength;
	expect(compactBytes).toBeLessThan(fullBytes / 5);
	await test.info().attach("navigation-payload-evidence.json", {
		body: Buffer.from(
			JSON.stringify({ fullBytes, compactBytes, browserReads: browserReads.length })
		),
		contentType: "application/json",
	});

	await page.goBack();
	await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	await page.goForward();
	await expect(page.getByRole("heading", { name: "Editorial notes", exact: true })).toBeVisible();
	await expectNoLoadingInsertion(page);
	expect(browserReads).toEqual([]);
});

test("custom dashboard loaders and category cards preserve prepared navigation and explicit refresh", async ({
	page,
}) => {
	const reads: string[] = [];
	page.on("request", (request) => {
		const path = new URL(request.url()).pathname;
		if (path.startsWith("/api/admin/loaders/") || path.startsWith("/api/admin/collection-list/"))
			reads.push(path);
	});
	await page.goto("/admin/");
	await expect(page.getByTestId("dashboard-category-count")).toContainText("Categories: 2");
	await expectNoLoadingInsertion(page);
	expect(reads).toEqual([]);
	await page.getByRole("button", { name: "Refresh dashboard", exact: true }).click();
	await expect.poll(() => reads).toEqual(["/api/admin/loaders/editorial-dashboard"]);
	await expect(page.getByRole("button", { name: "Refresh dashboard", exact: true })).toBeEnabled();
	await page.getByRole("link", { name: "Welcome posts", exact: true }).click();
	await expect(page.getByTestId("editorial-dashboard")).toContainText("Search: Welcome");
	await expectNoLoadingInsertion(page);
	expect(reads).toEqual(["/api/admin/loaders/editorial-dashboard"]);
	await page.getByRole("link", { name: "Categories", exact: true }).first().click();
	await expect(page.getByTestId("category-cards")).toBeVisible();
	await expect(page.getByTestId("category-cards").locator("article")).toHaveCount(2);
	await expect(
		page.getByTestId("category-cards").getByRole("link", { name: "News", exact: true })
	).toHaveAttribute("href", /\/admin\/collections\/categories\/[^?]+\?locale=en$/);
	const selectNews = page
		.getByTestId("category-cards")
		.getByRole("button", { name: "Select News", exact: true });
	await selectNews.click();
	await expect(selectNews).toHaveAttribute("aria-pressed", "true");
	await expectNoLoadingInsertion(page);
	await page.reload();
	await expect(page.getByTestId("category-cards")).toBeVisible();
	await expectNoLoadingInsertion(page);
	expect(reads).toEqual(["/api/admin/loaders/editorial-dashboard"]);
	await page.goBack();
	await expect(page.getByTestId("editorial-dashboard")).toBeVisible();
	await expectNoLoadingInsertion(page);
	expect(reads).toEqual(["/api/admin/loaders/editorial-dashboard"]);
});

test("custom routes and complete replacement views prepare data across reload, URL changes and history", async ({
	page,
}) => {
	const created = await page.request.post("/api/collections/loader-records?locale=en", {
		data: { title: "Alpha" },
	});
	expect(created.ok(), await created.text()).toBe(true);
	const body = await created.json();
	const id = body.doc.id as string;
	const reads: string[] = [];
	page.on("request", (request) => {
		const url = new URL(request.url());
		if (
			url.pathname.startsWith("/api/admin/") ||
			url.pathname.startsWith("/api/collections/loader-records") ||
			url.pathname === "/api/schema" ||
			url.pathname === "/api/auth/session"
		)
			reads.push(url.pathname);
	});
	for (const path of [
		"/editorial-report",
		"/collections/loader-records",
		"/collections/loader-records/create",
		"/collections/loader-records/create/api",
		`/collections/loader-records/${id}`,
		`/collections/loader-records/${id}/api`,
		"/globals/loader-summary",
		"/globals/loader-summary/api",
	]) {
		const response = await page.goto(`/admin${path}?locale=en`);
		const html = await response!.text();
		const state = JSON.parse(
			html.match(/<template id="ridu-admin-initial-state">([\s\S]*?)<\/template>/)![1]
		);
		expect(state).toMatchObject({
			outcome: "prepared",
			route: { kind: "custom" },
			moduleGroups: ["entry"],
		});
		expect(state.route).not.toHaveProperty("document");
		expect(state.route).not.toHaveProperty("data");
		await expect(page.getByTestId("view-path")).toHaveText(path);
		if (path.startsWith("/globals/")) {
			await expect(
				page.getByRole("heading", { name: "Editorial summary", exact: true })
			).toBeVisible();
			await expect(
				page.getByRole("link", { name: "Open Editorial summary", exact: true })
			).toHaveCount(0);
		}
		await expectNoLoadingInsertion(page);
		expect(reads).toEqual([]);
		await page.reload();
		await expect(page.getByTestId("view-path")).toHaveText(path);
		await expectNoLoadingInsertion(page);
		expect(reads).toEqual([]);
	}
	const navigation = page.getByRole("navigation", { name: "Editorial workspace" });
	await navigation.getByRole("link", { name: "Report", exact: true }).click();
	await expect(page.getByTestId("view-path")).toHaveText("/editorial-report");
	await page.getByRole("link", { name: "Open Alpha", exact: true }).click();
	await expect(page.getByTestId("view-path")).toHaveText(`/collections/loader-records/${id}`);
	await navigation.getByRole("link", { name: "Records", exact: true }).click();
	await expect(page.getByTestId("view-path")).toHaveText("/collections/loader-records");
	await page.getByRole("link", { name: "Open Alpha", exact: true }).click();
	await expect(page.getByTestId("view-path")).toHaveText(`/collections/loader-records/${id}`);
	await expect(page.getByRole("link", { name: "Open Alpha", exact: true })).toHaveCount(0);
	await navigation.getByRole("link", { name: "French Alpha", exact: true }).click();
	await expect(page.getByTestId("editorial-view")).toContainText("Locale: fr · Search: Alpha");
	await expectNoLoadingInsertion(page);
	expect(reads).toEqual([]);
	await page.goBack();
	await expect(page.getByTestId("editorial-view")).toContainText("Locale: en · Search: All");
	await page.goForward();
	await expect(page.getByTestId("editorial-view")).toContainText("Locale: fr · Search: Alpha");
	await expectNoLoadingInsertion(page);
	await page.getByRole("button", { name: "Refresh workspace" }).click();
	await expect(page.getByRole("button", { name: "Refresh workspace" })).toBeEnabled();
	expect(reads).toEqual(["/api/admin/loaders/editorial-view"]);
	await expectNoLoadingInsertion(page);
});

test("superseding a slow custom route keeps the previous view and never commits its stale data", async ({
	page,
}) => {
	await page.goto("/admin/editorial-report?locale=en");
	await expect(page.getByTestId("view-path")).toHaveText("/editorial-report");
	let release!: () => void;
	const held = new Promise<void>((resolve) => {
		release = resolve;
	});
	await page.route("**/admin/editorial-report?locale=fr&q=Alpha", async (route) => {
		await held;
		await route.continue().catch(() => {});
	});
	await page.getByRole("link", { name: "French Alpha", exact: true }).click();
	await expect(page.locator("main")).toHaveAttribute("aria-busy", "true");
	await expect(page.getByTestId("editorial-view")).toContainText("Locale: en · Search: All");
	await page
		.getByRole("navigation", { name: "Admin navigation" })
		.getByRole("link", { name: "Events", exact: true })
		.click();
	await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	release();
	await expect(page.getByTestId("editorial-view")).toHaveCount(0);
	await expectNoLoadingInsertion(page);
});

test("login cold load starts complete without schema or session bootstrap reads", async ({
	page,
}) => {
	await page.context().clearCookies();
	const browserReads: string[] = [];
	page.on("request", (request) => {
		const pathname = new URL(request.url()).pathname;
		if (pathname === "/api/schema" || pathname === "/api/auth/session") browserReads.push(pathname);
	});

	await page.goto("/admin/login");
	await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
	await expectNoLoadingInsertion(page);
	expect(browserReads).toEqual([]);
	// The fixture replaces the login surface, so it intentionally uses the runtime-only fallback.
	expect(await readyMarks(page)).toBe(0);
});

test("Posts stays complete across cold load, reload and status navigation on slow 4G", async ({
	page,
}) => {
	test.setTimeout(60_000);
	const network = await page.context().newCDPSession(page);
	await network.send("Network.enable");
	await network.send("Network.setCacheDisabled", { cacheDisabled: true });
	await network.send("Network.emulateNetworkConditions", {
		offline: false,
		latency: 150,
		downloadThroughput: (1.6 * 1024 * 1024) / 8,
		uploadThroughput: (750 * 1024) / 8,
	});
	const browserReads: string[] = [];
	page.on("request", (request) => {
		const url = new URL(request.url());
		if (request.method() === "GET" && url.pathname.startsWith("/api/"))
			browserReads.push(`${url.pathname}${url.search}`);
	});
	const response = await page.goto("/admin/collections/posts?locale=en");
	const html = await response!.text();
	const snapshot = JSON.parse(
		html.match(/<template id="ridu-admin-initial-state">([\s\S]*?)<\/template>/)![1]
	);
	expect(snapshot).toMatchObject({
		outcome: "prepared",
		route: { kind: "collection-list" },
	});
	await expect(page.getByRole("link", { name: "Welcome to Ridu", exact: true })).toBeVisible();
	await expectNoLoadingInsertion(page);
	expect(browserReads).toEqual([]);
	await test.info().attach("posts-slow-4g-evidence.json", {
		body: Buffer.from(
			JSON.stringify({
				htmlBytes: Buffer.byteLength(html),
				browserReads: browserReads.length,
				firstContentfulPaint: await firstContentfulPaint(page),
				readyMarks: await readyMarks(page),
			})
		),
		contentType: "application/json",
	});

	await page.reload();
	await expect(page.getByRole("link", { name: "Welcome to Ridu", exact: true })).toBeVisible();
	await expectNoLoadingInsertion(page);
	expect(browserReads).toEqual([]);

	await page.getByRole("button", { name: "Filters", exact: true }).click();
	await page.getByRole("button", { name: "Add filter", exact: true }).click();
	await chooseRiduSelect(page, page.getByLabel("Filter field", { exact: true }), "Status");
	await chooseRiduSelect(page, page.getByLabel("Filter value", { exact: true }), "Draft");
	await expect(page).toHaveURL(/filters=/);
	await expect(
		page.getByRole("link", { name: "Relationship field notes", exact: true })
	).toBeVisible();
	await expect(page.getByRole("link", { name: "Welcome to Ridu", exact: true })).toHaveCount(0);
	await expectNoLoadingInsertion(page);
	expect(browserReads).toEqual([]);
	const folders = page.waitForResponse("**/api/collections/folders?limit=100&locale=en");
	await page.getByRole("button", { name: "More", exact: true }).click();
	await folders;
	await page.getByRole("button", { name: "Folder", exact: true }).click();
	await expect(page.getByRole("option", { name: "Editorial", exact: true })).toBeVisible();
	expect(browserReads).toEqual(["/api/collections/folders?limit=100&locale=en"]);
	await network.detach();
});

test("framework-owned core routes commit their first complete DOM without duplicate reads", async ({
	page,
}) => {
	const events = (await (await page.request.get("/api/collections/events?limit=1")).json()) as {
		docs: { id: string }[];
	};
	const media = (await (await page.request.get("/api/collections/media?limit=1")).json()) as {
		docs: { id: string }[];
	};
	const eventID = events.docs[0]?.id;
	const mediaID = media.docs[0]?.id;
	expect(eventID).toBeDefined();
	expect(mediaID).toBeDefined();
	const filters = encodeURIComponent(
		JSON.stringify([{ field: "name", operator: "like", value: "Launch" }])
	);
	const routes: { path: string; ready: () => Locator; expectedReads?: string[] }[] = [
		{
			path: "/admin",
			ready: () => page.getByRole("heading", { name: "Content", exact: true }),
		},
		{
			path: `/admin/collections/events?filters=${filters}&limit=10&locale=en&page=1&q=Launch&sort=name`,
			ready: () => page.getByRole("heading", { name: "Events", exact: true }),
		},
		{
			path: "/admin/collections/media/trash?locale=en",
			ready: () => page.getByRole("heading", { name: "Media", exact: true }),
		},
		{
			path: "/admin/collections/events/create?locale=en",
			ready: () => page.getByRole("heading", { name: "New event", exact: true }),
		},
		{
			path: `/admin/collections/events/${eventID}?locale=en`,
			ready: () => page.getByLabel("Name", { exact: true }),
		},
		{
			path: `/admin/collections/events/${eventID}/api?locale=en`,
			ready: () => page.getByRole("button", { name: "Copy URL", exact: true }),
		},
		{
			path: `/admin/collections/media/${mediaID}?locale=en`,
			ready: () => page.getByRole("region", { name: "Asset preview" }),
		},
		{
			path: "/admin/collections/pages/pages_10/versions?locale=en",
			ready: () => page.getByRole("table", { name: "Version history" }),
		},
		{
			path: "/admin/collections/pages/pages_10/versions/2?locale=en",
			ready: () => page.getByRole("heading", { name: "Compare Versions" }),
			expectedReads: ["GET /api/collections/pages/pages_10/versions/1?locale=all"],
		},
		{
			path: "/admin/globals/validation-settings?locale=en",
			ready: () => page.getByRole("heading", { name: "Validation settings" }),
		},
		{
			path: "/admin/globals/validation-settings/api?locale=en",
			ready: () => page.getByRole("button", { name: "Copy URL", exact: true }),
		},
		{
			path: "/admin/collections/media/upload?locale=en",
			ready: () => page.getByRole("heading", { name: "Add files", exact: true }),
		},
	];

	for (const route of routes) {
		const browserReads: string[] = [];
		const recordRead = (request: Request) => {
			const url = new URL(request.url());
			if (
				url.pathname === "/api/schema" ||
				url.pathname === "/api/auth/session" ||
				url.pathname.startsWith("/api/admin/collection-list/") ||
				url.pathname.startsWith("/api/access/") ||
				(url.pathname.startsWith("/api/collections/") &&
					!url.pathname.startsWith("/api/collections/users/") &&
					request.method() === "GET") ||
				(url.pathname.startsWith("/api/globals/") && request.method() === "GET")
			)
				browserReads.push(`${request.method()} ${url.pathname}${url.search}`);
		};
		page.on("request", recordRead);
		await page.goto(route.path);
		await expect(route.ready()).toBeVisible();
		await expectNoLoadingInsertion(page);
		expect(browserReads, route.path).toEqual(route.expectedReads ?? []);
		await expect.poll(() => readyMarks(page), { message: route.path }).toBe(1);
		page.off("request", recordRead);
	}
});

test("document API routes show the document body without acquiring an edit lock", async ({
	page,
}) => {
	const events = (await (await page.request.get("/api/collections/events?limit=1")).json()) as {
		docs: { id: string }[];
	};
	const eventID = events.docs[0]?.id;
	expect(eventID).toBeDefined();
	const lockWrites: string[] = [];
	page.on("request", (request) => {
		const url = new URL(request.url());
		if (url.pathname.endsWith("/lock") && request.method() === "POST")
			lockWrites.push(url.pathname);
	});

	await page.goto(`/admin/collections/events/${eventID}/api?locale=en`);
	await expect(page.getByRole("button", { name: "Copy URL", exact: true })).toBeVisible();
	await expect(page.getByText('"name":', { exact: true }).first()).toBeVisible();
	await expect(page.getByText('"doc":', { exact: true })).toHaveCount(0);
	expect(lockWrites).toEqual([]);
});

test("cold lists honor the saved workspace without a browser reload", async ({ page }) => {
	const preference = await page.request.put("/api/preferences/collection%3Aevents%3Aworkspace", {
		data: {
			value: {
				columns: [{ path: "name", active: true }],
				limit: 10,
			},
		},
	});
	expect(preference.ok(), await preference.text()).toBe(true);
	const browserReads: string[] = [];
	page.on("request", (request) => {
		const url = new URL(request.url());
		if (
			url.pathname === "/api/schema" ||
			url.pathname === "/api/auth/session" ||
			url.pathname.startsWith("/api/admin/collection-list/") ||
			url.pathname.startsWith("/api/preferences/") ||
			(url.pathname === "/api/collections/events" && request.method() === "GET")
		)
			browserReads.push(`${request.method()} ${url.pathname}${url.search}`);
	});

	const response = await page.goto("/admin/collections/events?locale=en");
	await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Sort Name ascending", exact: true })
	).toBeVisible();
	const html = await response!.text();
	const state = JSON.parse(
		html.match(/<template id="ridu-admin-initial-state">([\s\S]*?)<\/template>/)![1]
	);
	expect(state.route.data.preferences.workspace.value.limit).toBe(10);
	expect(state.route.data.page.value.pagination.limit).toBe(10);
	await expectNoLoadingInsertion(page);
	expect(browserReads).toEqual([]);
});

test("an irrelevant query navigation cannot leave a seed for a later API refresh", async ({
	page,
}) => {
	const events = (await (await page.request.get("/api/collections/events?limit=1")).json()) as {
		docs: { id: string }[];
	};
	const eventID = events.docs[0]?.id;
	expect(eventID).toBeDefined();
	await page.goto(`/admin/collections/events/${eventID}/api?locale=en`);
	await expect(page.getByRole("button", { name: "Copy URL", exact: true })).toBeVisible();
	await page.evaluate(() => {
		history.pushState(null, "", `${location.pathname}?locale=en&panel=details`);
		window.dispatchEvent(new PopStateEvent("popstate"));
	});
	await expect(page).toHaveURL(/panel=details/);
	await expect.poll(() => readyMarks(page)).toBe(2);

	const reads: string[] = [];
	page.on("request", (request) => {
		const url = new URL(request.url());
		if (url.pathname === `/api/collections/events/${eventID}` && request.method() === "GET") {
			reads.push(url.search);
		}
	});
	await page.getByRole("button", { name: "Run request", exact: true }).click();
	await expect.poll(() => reads.length).toBe(1);
});

test("a restored create draft is access-checked before its complete form is revealed", async ({
	page,
}) => {
	const manifest = (await (await page.request.get("/api/schema")).json()).schema as {
		collections: { id: string; slug: string }[];
	};
	const events = manifest.collections.find((collection) => collection.slug === "events");
	expect(events).toBeDefined();
	await page.goto("/admin");
	await page.evaluate((collection) => {
		sessionStorage.setItem(
			`ridu:form-recovery:${collection.id}:new`,
			JSON.stringify({
				version: 1,
				collection,
				values: { name: "Recovered event" },
				original: {},
				createdAt: new Date().toISOString(),
			})
		);
	}, events!);
	const accessReads: string[] = [];
	page.on("request", (request) => {
		const url = new URL(request.url());
		if (url.pathname === "/api/access/collections/events") accessReads.push(request.method());
	});

	await page.goto("/admin/collections/events/create?locale=en");
	await expect(page.getByRole("heading", { name: "Recovered event", exact: true })).toBeVisible();
	await expect(page.getByLabel("Name", { exact: true })).toHaveValue("Recovered event");
	await expectNoLoadingInsertion(page);
	expect(accessReads).toEqual(["POST"]);
});

test("a dirty form blocks navigation while leaving the complete outgoing route usable", async ({
	page,
}) => {
	await page.goto("/admin/collections/events/create?locale=en");
	await page.getByLabel("Name", { exact: true }).fill("Unsaved event");
	const destination = page
		.getByRole("navigation", { name: "Admin navigation" })
		.getByRole("link", { name: "Editorial notes", exact: true });
	await destination.click();
	const dialog = page.getByRole("dialog");
	await expect(
		dialog.getByRole("heading", { name: "Leave without saving?", exact: true })
	).toBeVisible();
	await expect(page.getByRole("heading", { name: "Unsaved event", exact: true })).toBeVisible();
	await dialog.getByRole("button", { name: "Keep editing" }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/events\/create\?locale=en$/);
	await expect(page.getByLabel("Name", { exact: true })).toHaveValue("Unsaved event");

	await destination.click();
	await dialog.getByRole("button", { name: "Leave without saving" }).click();
	await expect(page.getByRole("heading", { name: "Editorial notes", exact: true })).toBeVisible();
	await expectNoLoadingInsertion(page);
});

test("slow route code retains the old page, disables its main surface, and reveals the form once", async ({
	page,
}) => {
	await page.goto("/admin/collections/events");
	await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	await clearLoadingInsertions(page);

	let releaseChunk!: () => void;
	let chunkStarted!: () => void;
	const release = new Promise<void>((resolve) => (releaseChunk = resolve));
	const started = new Promise<void>((resolve) => (chunkStarted = resolve));
	await page.route(/\/document-route-[^/]+\.js$/, async (route) => {
		chunkStarted();
		await release;
		await route.continue();
	});
	await page.getByRole("link", { name: /^New / }).click({ noWaitAfter: true });
	await started;
	await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	await expect(page.locator("main")).toHaveAttribute("inert", "");
	await expect(page.locator(loadingSelector)).toHaveCount(0);
	releaseChunk();
	await expect(page.getByRole("heading", { name: "New event", exact: true })).toBeVisible();
	await expect(page.locator("main")).not.toHaveAttribute("inert", "");
	await expectNoLoadingInsertion(page);
	await page.unroute(/\/document-route-[^/]+\.js$/);
});

test("a newer sidebar navigation supersedes a held document navigation", async ({ page }) => {
	await page.goto("/admin/collections/events");
	let releaseChunk!: () => void;
	let chunkStarted!: () => void;
	const release = new Promise<void>((resolve) => (releaseChunk = resolve));
	const started = new Promise<void>((resolve) => (chunkStarted = resolve));
	await page.route(/\/document-route-[^/]+\.js$/, async (route) => {
		chunkStarted();
		await release;
		await route.continue();
	});
	await page.getByRole("link", { name: /^New / }).click();
	await started;
	await page
		.getByRole("navigation", { name: "Admin navigation" })
		.getByRole("link", { name: "Editorial notes", exact: true })
		.click();
	releaseChunk();
	await expect(page.getByRole("heading", { name: "Editorial notes", exact: true })).toBeVisible();
	await expect(page).toHaveURL(/\/admin\/collections\/editorial-notes(?:\?.*)?$/);
	await page.unroute(/\/document-route-[^/]+\.js$/);
});

for (const fallback of ["full-runtime", "data-free", "runtime-failed"]) {
	test(`browser-loader startup recovers an authoritative document (${fallback} fallback)`, async ({
		page,
	}) => {
		await page.route("**/admin/collections/events", async (route) => {
			const response = await route.fetch();
			const html = (await response.text())
				.replace(/<template id="ridu-admin-initial-state">[\s\S]*?<\/template>/, "")
				// A context-less fallback may also have started from an older build.
				.replace(
					/(<meta name="ridu-admin-build-id" content=")[^"]+/,
					(_match, prefix) => `${prefix}${"0".repeat(24)}`
				);
			await route.fulfill({ response, body: html });
		});
		await page.goto("/admin/collections/events");
		await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
		await expect(page.locator('[data-slot="collection-list-results"]')).toHaveAttribute(
			"aria-busy",
			"false"
		);
		await expect(page.locator("html")).not.toHaveAttribute("data-ridu-context");
		await page.unroute("**/admin/collections/events");
		if (fallback !== "full-runtime") {
			await page.route("**/admin/plugin-contract", async (route) => {
				if (route.request().resourceType() === "document") return route.continue();
				const response = await route.fetch();
				const state = await response.json();
				delete state.runtime;
				if (fallback === "runtime-failed") {
					state.contextKey = "";
					state.fingerprint = "";
				}
				state.diagnostic = {
					code: fallback === "runtime-failed" ? "runtime_failed" : "snapshot_too_large",
					message: "The route uses its browser loader.",
				};
				await route.fulfill({ response, json: state });
			});
		}

		// A replaced route must not attach a fresh context key to the browser-loaded
		// runtime. Even this fallback destination needs a full document first.
		const documentResponse = page.waitForResponse(
			(response) =>
				response.request().resourceType() === "document" &&
				new URL(response.url()).pathname === "/admin/plugin-contract"
		);
		await page.getByRole("link", { name: "Plugin contract", exact: true }).click();
		const document = await documentResponse;
		expect(await document.text()).toContain('id="ridu-admin-initial-state"');
		await expect(page.getByRole("heading", { name: "Plugin route contract" })).toBeVisible();
		await expect(page.locator("html")).toHaveAttribute("data-ridu-context", /^[0-9a-f]{32}$/);
		if (fallback !== "full-runtime") await page.unroute("**/admin/plugin-contract");

		const navigationResponse = page.waitForResponse(
			(response) =>
				new URL(response.url()).pathname === "/admin/collections/events" &&
				response.headers()["content-type"] === "application/vnd.ridu.admin-route-state+json"
		);
		await page
			.getByRole("navigation", { name: "Admin navigation" })
			.getByRole("link", { name: "Events", exact: true })
			.click();
		expect(await (await navigationResponse).json()).not.toHaveProperty("runtime");
		await expect(page.getByRole("heading", { name: "Events", exact: true })).toBeVisible();
	});
}

test("plugin routes and replaced core views reuse runtime without speculative route reads", async ({
	page,
}) => {
	await page.goto("/admin");
	await expect(page.locator("html")).toHaveAttribute("data-ridu-context", /^[0-9a-f]{32}$/);
	for (const path of [
		"/admin/plugin-contract",
		"/admin/collections/payload-only-capabilities?locale=en",
		"/admin/account",
		"/admin/account/security",
		"/admin/does-not-exist",
	]) {
		const state = await page.evaluate(async (target) => {
			const response = await fetch(target, {
				headers: {
					Accept: "application/vnd.ridu.admin-route-state+json",
					"Ridu-Admin-Context": document.documentElement.dataset.riduContext ?? "",
				},
			});
			return response.json();
		}, path);
		expect(state).toMatchObject({ outcome: "fallback" });
		expect(state).not.toHaveProperty("runtime");
		expect(state).not.toHaveProperty("route");
		expect(state.navigation).toHaveProperty("collectionOperations");
	}
	await page.goto("/admin/plugin-contract");
	await expect(page.getByRole("heading", { name: "Plugin route contract" })).toBeVisible();
	await page.goto("/admin/collections/payload-only-capabilities");
	await expect(page.getByText("Plugin collectionList view", { exact: true })).toBeVisible();
});

test("same-owner query navigation retains an upload whose server response is pending", async ({
	page,
}) => {
	await page.goto("/admin/collections/media/upload?locale=en&x=1");
	await expect(page.getByRole("heading", { name: "Add files", exact: true })).toBeVisible();
	const committed = Promise.withResolvers<void>();
	const release = Promise.withResolvers<void>();
	await page.route("**/api/collections/media?*", async (route) => {
		if (route.request().method() !== "POST") return route.continue();
		const response = await route.fetch();
		committed.resolve();
		await release.promise;
		await route.fulfill({ response });
	});
	try {
		await page.locator('input[type="file"]').setInputFiles({
			name: "retained-upload.png",
			mimeType: "image/png",
			buffer: uploadFixturePng,
		});
		await expect(page.getByRole("region", { name: "Asset preview" })).toBeVisible();
		// File selection may load the image editor; query navigation must retain the ready editor.
		await clearLoadingInsertions(page);
		await page.getByRole("button", { name: "Save", exact: true }).click();
		await committed.promise;
		const state = page.waitForResponse((response) =>
			response.url().includes("/admin/collections/media/upload?x=2&locale=en")
		);
		await page.evaluate(() => {
			history.pushState({}, "", "/admin/collections/media/upload?x=2&locale=en");
			window.dispatchEvent(new PopStateEvent("popstate"));
		});
		await state;
		await expect(page.getByText("retained-upload.png", { exact: true })).toBeVisible();
		release.resolve();
		await expect(page.getByRole("link", { name: "Open asset", exact: true })).toBeVisible();
		await expectNoLoadingInsertion(page);
	} finally {
		release.resolve();
	}
});

async function authenticate(page: Page) {
	const response = await page.request.post("/api/auth/users/login", {
		data: { email: "admin@riducms.test", password: "ridu-admin" },
	});
	expect(response.ok(), await response.text()).toBe(true);
}

async function expectNoLoadingInsertion(page: Page) {
	expect(
		await page.evaluate(() => Reflect.get(window, "__riduLoadingInsertions") as string[])
	).toEqual([]);
}

async function clearLoadingInsertions(page: Page) {
	await page.evaluate(() => Reflect.set(window, "__riduLoadingInsertions", []));
}

async function readyMarks(page: Page) {
	return page.evaluate(() => performance.getEntriesByName("ridu:admin-route-ready").length);
}

async function firstContentfulPaint(page: Page) {
	return page.evaluate(
		() => performance.getEntriesByName("first-contentful-paint")[0]?.startTime ?? null
	);
}
