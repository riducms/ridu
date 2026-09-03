import { expect, test } from "./fixture";

import { documentSaveButton, loginAsEditor, uploadFixturePng } from "./helpers";

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
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	await page.getByRole("link", { name: "Welcome to Ridu", exact: true }).click();
	const navigationProgress = page.locator('[role="progressbar"]');
	const navigationProgressRegion = navigationProgress.locator("..");
	const expectNavigationProgressSettled = async () => {
		await page.waitForTimeout(800);
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
	await page.waitForTimeout(100);
	expect(createRequests).toBe(0);
});

test("published edits checkpoint locally without being background-published", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	await page.getByRole("link", { name: "Welcome to Ridu", exact: true }).click();
	const documentID = new URL(page.url()).pathname.split("/").at(-1);
	await page.locator('input[name="title"]').fill("Must remain an explicit publish");

	await expect
		.poll(
			() =>
				page.evaluate(() => {
					const key = Object.keys(sessionStorage).find((candidate) =>
						candidate.startsWith("ridu:form-recovery:")
					);
					if (key === undefined) return undefined;
					const checkpoint = JSON.parse(sessionStorage.getItem(key) ?? "null") as {
						values?: { title?: string };
					} | null;
					return checkpoint?.values?.title;
				}),
			{ timeout: 20_000 }
		)
		.toBe("Must remain an explicit publish");
	const response = await page.request.get(`/api/collections/posts/${documentID}`);
	expect(response.ok()).toBe(true);
	const stored = (await response.json()).doc;
	expect(stored.title).toBe("Welcome to Ridu");
	expect(stored._status).toBe("published");

	await page.reload();
	await expect(page.locator('input[name="title"]')).toHaveValue("Must remain an explicit publish");
});

test("published edits use the publish endpoint and remain published", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	await page.getByRole("link", { name: "Welcome to Ridu", exact: true }).click();
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

test("published edits require publish capability", async ({ page }) => {
	await page.route(/\/api\/access\/collections\/posts(?:\?|$)/, async (route) => {
		const response = await route.fetch();
		const body = (await response.json()) as { operations: { publish: boolean; update: boolean } };
		body.operations.update = true;
		body.operations.publish = false;
		await route.fulfill({ response, json: body });
	});
	let publishRequests = 0;
	page.on("request", (request) => {
		if (request.method() === "POST" && new URL(request.url()).pathname.endsWith("/publish")) {
			publishRequests += 1;
		}
	});
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	await page.getByRole("link", { name: "Welcome to Ridu", exact: true }).click();
	await page.locator('input[name="title"]').fill("Cannot bypass publish access");
	const publish = page.getByRole("button", { name: "Publish changes", exact: true });
	await expect(publish).toBeDisabled();
	await page.locator('input[name="title"]').press("Enter");
	await page.waitForTimeout(100);
	expect(publishRequests).toBe(0);
});

test("dirty drafts autosave to a new server revision", async ({ page }) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/posts?draft=true", {
		data: { title: "Autosave draft", summary: "Before the timer." },
	});
	expect(created.ok()).toBe(true);
	const initial = (await created.json()).doc;
	await page.goto(`/admin/collections/posts/${initial.id}`);
	await page.locator('input[name="title"]').fill("Autosaved draft revision");

	await expect
		.poll(
			async () => {
				const response = await page.request.get(`/api/collections/posts/${initial.id}`);
				if (!response.ok()) return undefined;
				return (await response.json()).doc;
			},
			{ timeout: 20_000 }
		)
		.toMatchObject({
			title: "Autosaved draft revision",
			_status: "draft",
			_revision: initial._revision + 1,
		});
});

test("publication controls cannot erase dirty draft edits", async ({ page }) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/posts?draft=true", {
		data: { title: "Publication control draft", summary: "Stored before editing." },
	});
	expect(created.ok()).toBe(true);
	const documentID = (await created.json()).doc.id;
	await page.goto(`/admin/collections/posts/${documentID}`);
	await page.locator('input[name="title"]').fill("Dirty draft edit");
	await expect(page.getByRole("button", { name: "Publish", exact: true })).toBeDisabled();
	await page.getByRole("button", { name: "Save draft", exact: true }).click();
	await expect(page.getByRole("button", { name: "Publish", exact: true })).toBeEnabled();
});
