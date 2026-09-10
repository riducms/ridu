import { expect, test } from "./fixture";

import { documentSaveButton } from "./helpers";

test("document live preview streams unsaved drafts across configured viewports", async ({
	page,
	adminServer,
}) => {
	await page.route("**/api/schema", async (route) => {
		const response = await route.fetch();
		const body = (await response.json()) as {
			schema: {
				collections: { slug: string; versionSettings?: { autosaveIntervalSeconds: number } }[];
			};
		};
		const posts = body.schema.collections.find((collection) => collection.slug === "posts");
		if (!posts?.versionSettings) throw new Error("Posts fixture must expose autosave settings");
		posts.versionSettings.autosaveIntervalSeconds = 0;
		await route.fulfill({ response, json: body });
	});
	const consoleErrors: string[] = [];
	page.on("console", (message) => {
		if (message.type() === "error") consoleErrors.push(message.text());
	});
	await page.goto("/admin/login");
	await expect(page.getByText("Plugin provider boundary", { exact: true })).toBeAttached();
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in" }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor({ state: "visible" });
	consoleErrors.length = 0;
	const postID = await page.evaluate(async () => {
		const response = await fetch("/api/collections/posts?limit=100");
		if (!response.ok) throw new Error(`list posts: ${response.status}`);
		const result = (await response.json()) as { docs: { id: string; title?: string }[] };
		return result.docs.find((document) => document.title === "Relationship field notes")?.id;
	});
	expect(postID).toBeDefined();
	await page.goto(`/admin/collections/posts/${postID}/api`);
	const previewTokenPattern = `**/api/preview/collections/posts/${postID}/token`;
	const rotatedTokens: string[] = [];
	let previewTokenMintCount = 0;
	await page.route(previewTokenPattern, async (route) => {
		const response = await route.fetch();
		const payload = (await response.json()) as {
			previewToken: { token: string; expiresAt: string };
		};
		rotatedTokens.push(payload.previewToken.token);
		previewTokenMintCount += 1;
		await route.fulfill({
			response,
			json: {
				...payload,
				previewToken:
					previewTokenMintCount === 1
						? {
								...payload.previewToken,
								expiresAt: new Date(Date.now() + 31_000).toISOString(),
							}
						: payload.previewToken,
			},
		});
	});
	const previewTokenResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			response.url().endsWith(`/api/preview/collections/posts/${postID}/token`)
	);
	await page.getByRole("button", { name: "Live preview", exact: true }).click();
	expect((await previewTokenResponse).status()).toBe(201);
	await expect(page).toHaveURL(new RegExp(`/admin/collections/posts/${postID}(?:\\?locale=en)?$`));
	const preview = page.frameLocator('iframe[title="Live preview"]');
	await expect(page.getByText("Connected", { exact: true })).toBeVisible();
	await expect(preview.getByRole("heading", { name: "Relationship field notes" })).toBeVisible();
	await page.locator('input[name="title"]').fill("Unsaved preview headline");
	await expect(preview.getByRole("heading", { name: "Unsaved preview headline" })).toBeVisible();
	await page.getByRole("button", { name: "Mobile", exact: true }).click();
	const previewRegion = page.getByRole("region", { name: "Live preview" });
	await expect(previewRegion.getByRole("spinbutton").nth(0)).toHaveValue("375");
	await expect(previewRegion.getByRole("spinbutton").nth(1)).toHaveValue("667");
	await expect(page.locator('iframe[title="Live preview"]')).toHaveAttribute("width", "375");
	await expect(page.locator('iframe[title="Live preview"]')).toHaveAttribute("height", "667");
	const previewWindowLink = page.getByRole("link", { name: "Open preview in new window" });
	const escapedPreviewOrigin = adminServer.previewURL.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
	await expect(previewWindowLink).toHaveAttribute(
		"href",
		new RegExp(
			`^${escapedPreviewOrigin}/preview/posts/${postID}\\?slug=relationship-field-notes&__ridu_preview=[^&]+&__ridu_preview_token=`
		)
	);
	const popupPromise = page.waitForEvent("popup");
	await previewWindowLink.click();
	const popup = await popupPromise;
	await expect(popup.getByRole("heading", { name: "Unsaved preview headline" })).toBeVisible();
	await expect.poll(() => rotatedTokens.length).toBeGreaterThanOrEqual(2);
	await expect
		.poll(() => new URL(popup.url()).searchParams.get("__ridu_preview_token"))
		.toBe(rotatedTokens.at(-1));
	await page.locator('input[name="title"]').fill("Popup reconnect headline");
	await expect(popup.getByRole("heading", { name: "Popup reconnect headline" })).toBeVisible();
	const currentPreviewToken = new URL(popup.url()).searchParams.get("__ridu_preview_token");
	expect(currentPreviewToken).toBeTruthy();
	const activeStatus = await page.evaluate(
		async ({ id, token }) =>
			(
				await fetch(`/api/preview/collections/posts/${id}`, {
					headers: { Authorization: `Bearer ${token}` },
				})
			).status,
		{ id: postID, token: currentPreviewToken }
	);
	expect(activeStatus).toBe(200);
	await popup.close();
	const revokedTokenResponse = page.waitForResponse((response) => {
		if (
			response.request().method() !== "POST" ||
			!response.url().endsWith("/api/preview/token/revoke")
		) {
			return false;
		}
		const requestBody = response.request().postDataJSON() as { token?: string };
		return requestBody.token === currentPreviewToken;
	});
	await page.getByRole("button", { name: "Live preview", exact: true }).click();
	expect((await revokedTokenResponse).status()).toBe(200);
	await expect(page.getByRole("region", { name: "Live preview" })).toHaveCount(0);
	const revokedStatus = await page.evaluate(
		async ({ id, token }) =>
			(
				await fetch(`/api/preview/collections/posts/${id}`, {
					headers: { Authorization: `Bearer ${token}` },
				})
			).status,
		{ id: postID, token: currentPreviewToken }
	);
	expect(revokedStatus).toBe(401);
	expect(consoleErrors).toEqual([
		"Failed to load resource: the server responded with a status of 401 (Unauthorized)",
	]);
	consoleErrors.length = 0;
	await page.unroute(previewTokenPattern);

	const replacementTokenResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			response.url().endsWith(`/api/preview/collections/posts/${postID}/token`)
	);
	await page.getByRole("button", { name: "Live preview", exact: true }).click();
	expect((await replacementTokenResponse).status()).toBe(201);
	await expect(page.getByText("Connected", { exact: true })).toBeVisible();
	await expect(page.getByRole("dialog", { name: "Leave without saving?" })).toHaveCount(0);
	const replacementPopupPromise = page.waitForEvent("popup");
	await page.getByRole("link", { name: "Open preview in new window" }).click();
	const replacementPopup = await replacementPopupPromise;
	await expect(
		replacementPopup.getByRole("heading", { name: "Popup reconnect headline" })
	).toBeVisible();
	const storedDraft = await page.request.get(`/api/collections/posts/${postID}`);
	expect(storedDraft.ok()).toBe(true);
	expect((await storedDraft.json()).doc.title).toBe("Relationship field notes");
	const routeRevokeResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" && response.url().endsWith("/api/preview/token/revoke")
	);
	const releasedLock = page.waitForResponse(
		(response) =>
			response.request().method() === "DELETE" &&
			response.url().endsWith(`/api/collections/posts/${postID}/lock`)
	);
	await page
		.getByRole("navigation", { name: "Breadcrumb" })
		.getByRole("link", { name: "Posts", exact: true })
		.click();
	const leaveDialog = page.getByRole("dialog", { name: "Leave without saving?" });
	await expect(leaveDialog).toBeVisible();
	await leaveDialog.getByRole("button", { name: "Leave without saving" }).click();
	expect((await routeRevokeResponse).status()).toBe(200);
	await releasedLock;
	await expect.poll(() => replacementPopup.isClosed()).toBe(true);
	await page.goBack();
	await expect(page.locator('input[name="title"]')).toHaveValue("Relationship field notes");
	await expect(page.getByRole("region", { name: "Live preview" })).toHaveCount(0);
	expect(consoleErrors).toEqual([]);
});

test("live preview popups are owned independently by each admin page", async ({
	context,
	page,
}) => {
	test.setTimeout(45_000);
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in" }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor({ state: "visible" });
	const postIDs = await page.evaluate(async () => {
		const createPost = async (title: string) => {
			const response = await fetch("/api/collections/posts", {
				method: "POST",
				headers: { "Content-Type": "application/json" },
				body: JSON.stringify({ title, summary: `${title} preview summary.` }),
			});
			if (!response.ok) throw new Error(`create post: ${response.status}`);
			return ((await response.json()) as { doc: { id: string } }).doc.id;
		};
		return [
			await createPost("First popup ownership post"),
			await createPost("Second popup ownership post"),
		];
	});
	expect(postIDs).toHaveLength(2);

	await page.goto(`/admin/collections/posts/${postIDs[0]}`);
	const secondAdmin = await context.newPage();
	await secondAdmin.goto(`/admin/collections/posts/${postIDs[1]}`);
	await page.getByRole("button", { name: "Live preview", exact: true }).click();
	await secondAdmin.getByRole("button", { name: "Live preview", exact: true }).click();
	await expect(page.getByText("Connected", { exact: true })).toBeVisible();
	await expect(secondAdmin.getByText("Connected", { exact: true })).toBeVisible();

	const firstPopupPromise = page.waitForEvent("popup");
	await page.getByRole("link", { name: "Open preview in new window" }).click();
	const firstPopup = await firstPopupPromise;
	const secondPopupPromise = secondAdmin.waitForEvent("popup");
	await secondAdmin.getByRole("link", { name: "Open preview in new window" }).click();
	const secondPopup = await secondPopupPromise;
	expect(firstPopup).not.toBe(secondPopup);
	expect(firstPopup.url()).toContain(`/preview/posts/${postIDs[0]}`);
	expect(secondPopup.url()).toContain(`/preview/posts/${postIDs[1]}`);

	await page.locator('input[name="title"]').fill("First page unsaved preview");
	await secondAdmin.locator('input[name="title"]').fill("Second page unsaved preview");
	await expect(
		firstPopup.getByRole("heading", { name: "First page unsaved preview" })
	).toBeVisible();
	await expect(
		secondPopup.getByRole("heading", { name: "Second page unsaved preview" })
	).toBeVisible();

	await page.getByRole("button", { name: "Live preview", exact: true }).click();
	await expect.poll(() => firstPopup.isClosed()).toBe(true);
	expect(secondPopup.isClosed()).toBe(false);
	await secondAdmin.locator('input[name="title"]').fill("Second page remains connected");
	await expect(
		secondPopup.getByRole("heading", { name: "Second page remains connected" })
	).toBeVisible();
	await secondAdmin.getByRole("button", { name: "Live preview", exact: true }).click();
	await expect.poll(() => secondPopup.isClosed()).toBe(true);
	await secondAdmin.close();
});

test("an upload in flight cannot be abandoned through in-app navigation", async ({ page }) => {
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in" }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor({ state: "visible" });
	await page.goto("/admin/collections/media/create");
	await page.locator('input[type="file"]').setInputFiles({
		name: "route-departure.png",
		mimeType: "image/png",
		buffer: Buffer.from(
			"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABAQMAAAAl21bKAAAAA1BMVEV8Ou0Bg+xSAAAACklEQVQI12NgAAAAAgAB4iG8MwAAAABJRU5ErkJggg==",
			"base64"
		),
	});
	await page.getByRole("textbox", { name: "Alt text" }).fill("Route departure");
	let releaseUpload!: () => void;
	let markUploadCommitted!: () => void;
	const uploadGate = new Promise<void>((resolve) => {
		releaseUpload = resolve;
	});
	const uploadCommitted = new Promise<void>((resolve) => {
		markUploadCommitted = resolve;
	});
	const mediaCollectionEndpoint = (url: URL) => url.pathname === "/api/collections/media";
	await page.route(mediaCollectionEndpoint, async (route) => {
		if (route.request().method() !== "POST") {
			await route.continue();
			return;
		}
		const response = await route.fetch();
		markUploadCommitted();
		await uploadGate;
		await route.fulfill({ response });
	});
	await documentSaveButton(page).click();
	await uploadCommitted;
	await page
		.getByRole("navigation", { name: "Admin navigation" })
		.getByRole("link", { name: "Posts", exact: true })
		.click();
	const leaveDialog = page.getByRole("dialog", { name: "Leave without saving?" });
	await expect(leaveDialog).toBeVisible();
	await expect(leaveDialog.getByRole("button", { name: "Leave without saving" })).toBeDisabled();
	await expect(page).toHaveURL(/\/admin\/collections\/media\/create(?:\?locale=en)?$/);
	releaseUpload();
	await expect(page).toHaveURL(
		/\/admin\/collections\/media\/(?!create(?:\?|$))[^/?]+(?:\?locale=en)?$/
	);
	await page
		.getByRole("navigation", { name: "Admin navigation" })
		.getByRole("link", { name: "Posts", exact: true })
		.click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts(?:\?locale=en)?$/);
	await expect(page.getByText("Asset successfully created.")).toHaveCount(1);
	await page.unroute(mediaCollectionEndpoint);
});
