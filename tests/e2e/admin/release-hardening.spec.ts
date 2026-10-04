import { expect, test } from "./fixture";
import {
	documentSaveButton,
	loginAsEditor,
	uploadFixturePng,
	useAdminRuntimeFallback,
} from "./helpers";

test("new versioned documents make draft and publish intent explicit", async ({ page }) => {
	await loginAsEditor(page);

	await page.goto("/admin/collections/posts/create");
	await page.locator('input[name="title"]').fill("Release hardening draft");
	await page
		.getByLabel("Summary — English", { exact: true })
		.fill("Saved deliberately as a draft.");
	await expect(page.getByRole("button", { name: "Save draft", exact: true })).toBeVisible();
	await expect(page.getByRole("button", { name: "Publish", exact: true })).toBeVisible();
	await page.getByRole("button", { name: "Save draft", exact: true }).click();
	await expect(page).toHaveURL(
		/\/admin\/collections\/posts\/(?!create(?:\?|$))[^/?]+(?:\?locale=en)?$/
	);
	let documentID = new URL(page.url()).pathname.split("/").at(-1);
	expect(documentID).toBeTruthy();
	let response = await page.request.get(`/api/collections/posts/${documentID}`);
	expect(response.ok()).toBe(true);
	expect((await response.json()).doc._status).toBe("draft");

	await page.goto("/admin/collections/posts/create");
	await page.locator('input[name="title"]').fill("Release hardening published");
	await page
		.getByLabel("Summary — English", { exact: true })
		.fill("Published atomically on creation.");
	await page.getByRole("button", { name: "Publish", exact: true }).click();
	await expect(page).toHaveURL(
		/\/admin\/collections\/posts\/(?!create(?:\?|$))[^/?]+(?:\?locale=en)?$/
	);
	documentID = new URL(page.url()).pathname.split("/").at(-1);
	expect(documentID).toBeTruthy();
	response = await page.request.get(`/api/collections/posts/${documentID}`);
	expect(response.ok()).toBe(true);
	expect((await response.json()).doc._status).toBe("published");
});

test("in-app navigation cannot silently discard an unsaved document edit", async ({ page }) => {
	await page.clock.install();
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	await page.getByRole("link", { name: "Welcome to Ridu", exact: true }).click();
	const navigationProgress = page.locator('[role="progressbar"]');
	const navigationProgressRegion = navigationProgress.locator("..");
	const expectNavigationProgressSettled = async () => {
		// Pending and idle are both hidden. Drain the reveal, nested animation
		// frames and completion timers before asserting the final idle state.
		await page.clock.runFor(800);
		await expect(navigationProgressRegion).toHaveAttribute("aria-hidden", "true");
		await expect(navigationProgress).toHaveAttribute("aria-valuenow", "0");
	};
	const title = page.locator('input[name="title"]');
	await title.fill("Unsaved release blocker reproduction");

	await page.getByRole("link", { name: "Posts", exact: true }).last().click();
	const warning = page.getByRole("dialog", { name: "Leave without saving?" });
	await expect(warning).toBeVisible();
	await warning.getByRole("button", { name: "Keep editing" }).click();
	await expect(title).toHaveValue("Unsaved release blocker reproduction");
	await expect(page).toHaveURL(/\/admin\/collections\/posts\/[^/]+$/);
	await expectNavigationProgressSettled();

	await page.goBack();
	await expect(warning).toBeVisible();
	await warning.getByRole("button", { name: "Keep editing" }).click();
	await expect(title).toHaveValue("Unsaved release blocker reproduction");
	await expect(page).toHaveURL(/\/admin\/collections\/posts\/[^/]+$/);
	await expectNavigationProgressSettled();

	await page.getByRole("link", { name: "Posts", exact: true }).last().click();
	await warning.getByRole("button", { name: "Leave without saving" }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts(?:\?locale=en)?$/);
	await expectNavigationProgressSettled();
	await page.getByRole("link", { name: "Welcome to Ridu", exact: true }).click();
	await expect(page.locator('input[name="title"]')).toHaveValue("Welcome to Ridu");
});

test("dirty authoring registers native beforeunload protection", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/categories/categories_4");
	const beforeUnloadPrevented = () =>
		page.evaluate(() => {
			const event = new Event("beforeunload", { cancelable: true });
			window.dispatchEvent(event);
			return event.defaultPrevented;
		});

	expect(await beforeUnloadPrevented()).toBe(false);
	const name = page.getByLabel("Name", { exact: true });
	await name.fill("Unsaved category");
	await expect.poll(beforeUnloadPrevented).toBe(true);

	await name.fill("News");
	await expect.poll(beforeUnloadPrevented).toBe(false);
});

test("server validation stays visible without discarding dirty values", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/categories/categories_4");
	const name = page.getByLabel("Name", { exact: true });
	const slug = page.getByLabel("Slug", { exact: true });
	await name.fill("Guides");
	await expect(slug).toHaveValue("guides");

	const failedSave = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === "/api/collections/categories/categories_4"
	);
	await documentSaveButton(page).click();
	const response = await failedSave;
	expect(response.status()).toBe(422);
	expect((await response.json()).error).toMatchObject({
		code: "validation",
		issues: [{ code: "unique", path: "slug", message: "Slug must be unique" }],
	});
	await expect(page.getByText("Slug must be unique", { exact: true })).toBeVisible();
	await expect(
		page
			.getByLabel("Notifications alt+T")
			.getByText("The following field is invalid: Slug", { exact: true })
	).toBeVisible();
	await expect(name).toHaveValue("Guides");
	await expect(slug).toHaveValue("guides");

	const stored = await page.request.get("/api/collections/categories/categories_4");
	expect(stored.ok()).toBe(true);
	expect((await stored.json()).doc).toMatchObject({ name: "News", slug: "news" });
});

test("non-field save failures stay visible without discarding dirty values", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts/posts_9");
	const title = page.getByLabel("Title — English", { exact: true });
	const loaded = await page.request.get("/api/collections/posts/posts_9?locale=en");
	expect(loaded.ok()).toBe(true);
	const current = (await loaded.json()).doc;

	await title.fill("Browser conflict edit");
	const remoteUpdate = await page.request.post("/api/collections/posts/posts_9/publish?locale=en", {
		headers: { "If-Match": `"${current._revision}"` },
		data: { readingMinutes: 8 },
	});
	expect(remoteUpdate.ok(), await remoteUpdate.text()).toBe(true);

	const failedSave = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === "/api/collections/posts/posts_9/publish"
	);
	await page.getByRole("button", { name: "Publish changes", exact: true }).click();
	const response = await failedSave;
	expect(response.status()).toBe(409);
	await expect(
		page.getByLabel("Notifications alt+T").getByText("Document not saved", { exact: true })
	).toBeVisible();
	await expect(
		page
			.getByLabel("Notifications alt+T")
			.getByText("document conflicts with an existing value", { exact: true })
	).toBeVisible();
	await expect(title).toHaveValue("Browser conflict edit");

	const stored = await page.request.get("/api/collections/posts/posts_9?locale=en");
	expect(stored.ok()).toBe(true);
	expect((await stored.json()).doc).toMatchObject({
		title: "Welcome to Ridu",
		readingMinutes: 8,
	});
});

test("selecting an upload file participates in unsaved-navigation protection", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/media/create");
	const fileInput = page.locator('input[type="file"]');
	await fileInput.setInputFiles({
		name: "unsaved-upload.png",
		mimeType: "image/png",
		buffer: uploadFixturePng,
	});
	await expect(
		page.getByRole("heading", { name: "unsaved-upload.png", exact: true })
	).toBeVisible();
	await documentSaveButton(page).click();
	await expect(
		page.getByRole("heading", { name: "unsaved-upload.png", exact: true })
	).toBeVisible();

	await page
		.getByRole("navigation", { name: "Breadcrumb" })
		.getByRole("link", { name: "Media", exact: true })
		.click();
	const warning = page.getByRole("dialog", { name: "Leave without saving?" });
	await expect(warning).toBeVisible();
	await warning.getByRole("button", { name: "Keep editing" }).click();
	await expect(
		page.getByRole("heading", { name: "unsaved-upload.png", exact: true })
	).toBeVisible();

	await page
		.getByRole("navigation", { name: "Breadcrumb" })
		.getByRole("link", { name: "Media", exact: true })
		.click();
	await warning.getByRole("button", { name: "Leave without saving" }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/media(?:\?locale=en)?$/);
});

test("no-draft collection creation requires publish capability", async ({ page }) => {
	await useAdminRuntimeFallback(page);
	await page.route("**/api/schema", async (route) => {
		const response = await route.fetch();
		const body = (await response.json()) as {
			schema: { collections: { slug: string; versionSettings?: { drafts: boolean } }[] };
		};
		const posts = body.schema.collections.find((collection) => collection.slug === "posts");
		if (posts?.versionSettings !== undefined) posts.versionSettings.drafts = false;
		await route.fulfill({ response, json: body });
	});
	await page.route("**/api/access/collections/posts*", async (route) => {
		const response = await route.fetch();
		const body = (await response.json()) as { operations: { publish: boolean } };
		body.operations.publish = false;
		await route.fulfill({ response, json: body });
	});
	let createRequests = 0;
	page.on("request", (request) => {
		if (
			request.method() === "POST" &&
			new URL(request.url()).pathname === "/api/collections/posts"
		) {
			createRequests += 1;
		}
	});

	await loginAsEditor(page);
	await page.goto("/admin/collections/posts/create");
	await page.locator('input[name="title"]').fill("Cannot publish without permission");
	const publish = page.getByRole("button", { name: "Publish", exact: true });
	await expect(publish).toBeVisible();
	await expect(publish).toBeDisabled();
	await page.locator('input[name="title"]').press("Enter");
	// A denied submit has no completion event. Allow a bounded request-observation
	// window; the disabled button alone also describes an illicit pending save.
	await page.waitForTimeout(250);
	await expect(publish).toBeDisabled();
	expect(createRequests).toBe(0);
});

test("published edits autosave a durable working revision without being background-published", async ({
	page,
	request,
}) => {
	await page.clock.install();
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	await page.getByRole("link", { name: "Welcome to Ridu", exact: true }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts\/[^/?]+\?locale=en$/);
	const documentID = new URL(page.url()).pathname.split("/").at(-1);
	expect(documentID).toBeTruthy();
	const workingEndpoint = `/api/collections/posts/${documentID}?locale=en&draft=true`;
	const publicEndpoint = `/api/collections/posts/${documentID}?locale=en&draft=false`;
	const initialWorkingResponse = await page.request.get(workingEndpoint);
	expect(initialWorkingResponse.ok(), await initialWorkingResponse.text()).toBe(true);
	const initialWorking = (await initialWorkingResponse.json()).doc;
	const initialPublicResponse = await request.get(publicEndpoint);
	expect(initialPublicResponse.ok(), await initialPublicResponse.text()).toBe(true);
	const initialPublic = (await initialPublicResponse.json()).doc;
	let publishRequests = 0;
	page.on("request", (sent) => {
		if (
			sent.method() === "POST" &&
			new URL(sent.url()).pathname === `/api/collections/posts/${documentID}/publish`
		) {
			publishRequests += 1;
		}
	});
	await page.locator('input[name="title"]').fill("Must remain an explicit publish");
	await page.clock.fastForward(15_000);

	await expect
		.poll(
			async () => {
				const response = await page.request.get(workingEndpoint);
				return response.ok() ? (await response.json()).doc : undefined;
			},
			{ timeout: 5_000 }
		)
		.toMatchObject({
			title: "Must remain an explicit publish",
			_status: "published",
			_revision: initialWorking._revision + 1,
			_publishedRevision: initialPublic._revision,
			_hasDraftChanges: true,
		});
	const publicResponse = await request.get(publicEndpoint);
	expect(publicResponse.ok(), await publicResponse.text()).toBe(true);
	expect((await publicResponse.json()).doc).toMatchObject({
		title: initialPublic.title,
		_status: "published",
		_revision: initialPublic._revision,
	});
	expect(publishRequests).toBe(0);

	await page.reload();
	await expect(page.locator('input[name="title"]')).toHaveValue("Must remain an explicit publish");
	await expect(
		page.getByText("Saved draft changes pending publication", { exact: true })
	).toBeVisible();
});

test("published edits use the publish endpoint and remain published", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	await page.getByRole("link", { name: "Welcome to Ridu", exact: true }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts\/[^/?]+\?locale=en$/);
	const documentID = new URL(page.url()).pathname.split("/").at(-1);
	expect(documentID).toBeTruthy();
	let ordinaryUpdates = 0;
	page.on("request", (request) => {
		if (
			request.method() === "PATCH" &&
			new URL(request.url()).pathname === `/api/collections/posts/${documentID}`
		) {
			ordinaryUpdates += 1;
		}
	});
	await page.locator('input[name="title"]').fill("Published through lifecycle");
	const publishResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/posts/${documentID}/publish`
	);
	await page.getByRole("button", { name: "Publish changes", exact: true }).click();
	expect((await publishResponse).ok()).toBe(true);
	expect(ordinaryUpdates).toBe(0);
	const response = await page.request.get(`/api/collections/posts/${documentID}`);
	expect(response.ok()).toBe(true);
	expect((await response.json()).doc).toMatchObject({
		title: "Published through lifecycle",
		_status: "published",
	});
});

test("published edits can save drafts without publish capability", async ({ page, request }) => {
	await useAdminRuntimeFallback(page);
	await page.route(/\/api\/access\/collections\/posts(?:\?|$)/, async (route) => {
		const response = await route.fetch();
		const access = (await response.json()) as {
			operations: { update: boolean; publish: boolean };
		};
		access.operations.update = true;
		access.operations.publish = false;
		await route.fulfill({ response, json: access });
	});
	let publishRequests = 0;
	page.on("request", (request) => {
		if (request.method() === "POST" && new URL(request.url()).pathname.endsWith("/publish")) {
			publishRequests += 1;
		}
	});
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	const documentURL = await page
		.getByRole("link", { name: "Welcome to Ridu", exact: true })
		.getAttribute("href");
	if (documentURL === null) throw new Error("Expected document URL");
	await page.goto(documentURL);
	await expect(page.getByRole("heading", { name: "Welcome to Ridu", exact: true })).toBeVisible();
	const documentID = new URL(page.url()).pathname.split("/").at(-1);
	expect(documentID).toBeTruthy();
	const workingEndpoint = `/api/collections/posts/${documentID}?locale=en&draft=true`;
	const publicEndpoint = `/api/collections/posts/${documentID}?locale=en&draft=false`;
	const initialWorkingResponse = await page.request.get(workingEndpoint);
	expect(initialWorkingResponse.ok(), await initialWorkingResponse.text()).toBe(true);
	const initialWorking = (await initialWorkingResponse.json()).doc;
	const initialPublicResponse = await request.get(publicEndpoint);
	expect(initialPublicResponse.ok(), await initialPublicResponse.text()).toBe(true);
	const initialPublic = (await initialPublicResponse.json()).doc;
	await page.locator('input[name="title"]').fill("Cannot bypass publish access");
	const publish = page.getByRole("button", { name: "Publish changes", exact: true });
	const saveDraft = page.getByRole("button", { name: "Save draft", exact: true });
	await expect(publish).toHaveCount(0);
	await expect(saveDraft).toBeEnabled();
	const savedDraftResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/posts/${documentID}` &&
			new URL(response.url()).searchParams.get("draft") === "true"
	);
	await saveDraft.click();
	expect((await savedDraftResponse).ok()).toBe(true);
	const workingResponse = await page.request.get(workingEndpoint);
	expect(workingResponse.ok(), await workingResponse.text()).toBe(true);
	expect((await workingResponse.json()).doc).toMatchObject({
		title: "Cannot bypass publish access",
		_status: "published",
		_revision: initialWorking._revision + 1,
		_publishedRevision: initialPublic._revision,
		_hasDraftChanges: true,
	});
	const publicResponse = await request.get(publicEndpoint);
	expect(publicResponse.ok(), await publicResponse.text()).toBe(true);
	expect((await publicResponse.json()).doc).toMatchObject({
		title: initialPublic.title,
		_revision: initialPublic._revision,
		_status: "published",
	});
	await page.reload();
	await expect(page.locator('input[name="title"]')).toHaveValue("Cannot bypass publish access");
	await expect(publish).toHaveCount(0);
	await page.locator('input[name="title"]').fill("Still cannot bypass publish access");
	await page.locator('input[name="title"]').press("Enter");
	// Keep the request observer active beyond the completed keyboard event.
	await page.waitForTimeout(250);
	await expect(publish).toHaveCount(0);
	expect(publishRequests).toBe(0);
});

test("dirty drafts autosave to a new server revision", async ({ page }) => {
	await page.clock.install();
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/posts?draft=true", {
		data: { title: "Autosave draft", summary: "Before the timer." },
	});
	expect(created.ok()).toBe(true);
	const initial = (await created.json()).doc;
	await page.goto(`/admin/collections/posts/${initial.id}`);
	await page.locator('input[name="title"]').fill("Autosaved draft revision");
	await page.clock.fastForward(15_000);

	await expect
		.poll(
			async () => {
				const response = await page.request.get(`/api/collections/posts/${initial.id}`);
				if (!response.ok()) return undefined;
				return (await response.json()).doc;
			},
			{ timeout: 5_000 }
		)
		.toMatchObject({
			title: "Autosaved draft revision",
			_status: "draft",
			_revision: initial._revision + 1,
		});
});

test("draft publication saves dirty edits atomically", async ({ page }) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/posts?draft=true", {
		data: { title: "Publication control draft", summary: "Stored before editing." },
	});
	expect(created.ok()).toBe(true);
	const documentID = (await created.json()).doc.id;
	await page.goto(`/admin/collections/posts/${documentID}`);
	await expect(page.getByRole("link", { name: "Versions", exact: true })).toBeVisible();
	const publish = page.getByRole("button", { name: "Publish changes", exact: true });
	await expect(publish).toBeEnabled();
	await page.locator('input[name="title"]').fill("Dirty draft edit");
	const publishResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/posts/${documentID}/publish`
	);
	await publish.click();
	expect((await publishResponse).ok()).toBe(true);
	const response = await page.request.get(`/api/collections/posts/${documentID}`);
	expect(response.ok()).toBe(true);
	expect((await response.json()).doc).toMatchObject({
		title: "Dirty draft edit",
		_status: "published",
	});
	await expect(page.getByText("Published", { exact: true }).first()).toBeVisible();
	await expect(publish).toBeDisabled();
	await expect(page.getByText("Last saved less than a minute ago")).toBeVisible();
	await page.setViewportSize({ width: 1280, height: 720 });
	const bar = await page.locator(".ridu-document-bar").boundingBox();
	const heading = await page.getByRole("heading", { name: "Dirty draft edit" }).boundingBox();
	expect((bar?.y ?? 0) - (heading?.y ?? 0)).toBe(96);
	expect(bar?.height).toBe(56);
	await page.getByRole("button", { name: "Schedule", exact: true }).click();
	await page.getByRole("button", { name: "Schedule publication", exact: true }).click();
	await expect(page.getByRole("dialog").getByRole("radio", { name: "Unpublish" })).toBeChecked();
	const dialog = page.getByRole("dialog");
	const timeZone = dialog.getByRole("combobox", { name: "Timezone", exact: true });
	await expect(timeZone).toHaveValue(/^\(UTC\+0[01]:00\) London \(.+\)$/);
	await timeZone.fill("Paris");
	await page.getByRole("option", { name: /^\(UTC\+0[12]:00\) Paris \(.+\)$/ }).click();
	const year = new Date().getUTCFullYear() + 2;
	for (const [segment, digits] of [
		["month", "01"],
		["day,", "15"],
		["year", String(year)],
		["hour", "11"],
		["minute", "30"],
		["AM/PM", "a"],
	] as const) {
		const control = dialog.getByRole("spinbutton", { name: new RegExp(segment) });
		await control.click();
		await control.pressSequentially(digits);
	}
	const scheduledResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/posts/${documentID}/schedule`
	);
	await dialog.getByRole("button", { name: "Save", exact: true }).click();
	const scheduled = await scheduledResponse;
	expect(scheduled.status()).toBe(201);
	const event = (await scheduled.json()).scheduledPublication;
	expect(event).toMatchObject({
		action: "unpublish",
		timeZone: "Europe/Paris",
		runAt: `${year}-01-15T10:30:00Z`,
	});
	await expect(dialog.locator(".ridu-schedule__zone")).toHaveText(
		"(UTC+01:00) Paris (Central European Standard Time)"
	);
	await page.reload();
	await page.getByRole("button", { name: "Schedule", exact: true }).click();
	await page.getByRole("button", { name: "Schedule publication", exact: true }).click();
	await expect(page.locator(".ridu-schedule__zone")).toHaveText(
		"(UTC+01:00) Paris (Central European Standard Time)"
	);
	await expect(page.locator(".ridu-schedule__event")).toContainText("11:30 AM");
	await page.getByRole("button", { name: "Cancel scheduled unpublish", exact: true }).click();
	await expect(page.getByText("No upcoming events scheduled.", { exact: true })).toBeVisible();
});
