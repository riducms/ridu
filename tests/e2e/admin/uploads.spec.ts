import { expect, test } from "./fixture";

import { documentSaveButton, loginAsEditor, observePageErrors, uploadFixturePng } from "./helpers";

test("bulk uploads validate metadata, reject invalid URLs, and discard route-owned queues", async ({
	page,
}) => {
	const { consoleErrors, pageErrors } = observePageErrors(page);
	const collectionsNavigation = await loginAsEditor(page);
	consoleErrors.length = 0;
	await collectionsNavigation.getByRole("link", { name: "Media", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Media" })).toBeVisible();
	await page.getByRole("link", { name: "Bulk upload", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Add files", exact: true })).toBeVisible();
	await expect(page.getByRole("button", { name: "Select a file", exact: true })).toBeVisible();
	const bulkUploadInput = page.locator(".ridu-bulk-upload-picker > input").first();
	await expect(bulkUploadInput).toBeEnabled();
	await bulkUploadInput.setInputFiles([
		{ name: "payload-field-notes.png", mimeType: "image/png", buffer: uploadFixturePng },
		{ name: "payload-parity-cover.png", mimeType: "image/png", buffer: uploadFixturePng },
	]);
	await expect(page.getByRole("heading", { name: "Asset", exact: true })).toBeVisible();
	await expect(page.getByText("2 Files to Upload", { exact: true })).toBeVisible();
	await expect(page.locator(".ridu-bulk-upload-status")).toHaveText(
		"0 complete · 2 queued · 0 failed · 0 outcome unknown"
	);
	const uploadQueue = page.getByRole("list", { name: "Upload queue" });
	const fileEditor = page.locator(".ridu-bulk-upload-editor");
	await expect(fileEditor.getByLabel("Alt text", { exact: true })).toHaveValue(
		"payload field notes"
	);

	await page.getByRole("button", { name: "Edit all", exact: true }).click();
	const editAll = page.getByRole("dialog", { name: "Editing 2 assets", exact: true });
	await editAll.locator(".ridu-bulk-field-picker input").fill("Caption");
	await page.getByRole("option", { name: "Caption", exact: true }).click();
	await editAll.getByLabel("Caption", { exact: true }).fill("Bulk workflow browser check");
	await editAll.getByRole("button", { name: "Apply Changes", exact: true }).click();
	await expect(editAll).not.toBeVisible();
	await expect(fileEditor.getByLabel("Caption", { exact: true })).toHaveValue(
		"Bulk workflow browser check"
	);
	await page.getByRole("button", { name: "Next", exact: true }).click();
	await expect(fileEditor.getByLabel("Alt text", { exact: true })).toHaveValue(
		"payload parity cover"
	);
	await expect(fileEditor.getByLabel("Caption", { exact: true })).toHaveValue(
		"Bulk workflow browser check"
	);
	await page.getByRole("button", { name: "Previous", exact: true }).click();

	await fileEditor.getByLabel("Alt text", { exact: true }).fill("");
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect(page.getByText(/Alt text.*required/i)).toBeVisible();
	await expect(fileEditor.getByLabel("Alt text", { exact: true })).toBeFocused();
	await expect(uploadQueue.getByText("complete", { exact: true })).toHaveCount(1);
	expect(consoleErrors).toEqual([]);
	await fileEditor.getByLabel("Alt text", { exact: true }).fill("payload field notes");
	await page.getByRole("button", { name: "Retry", exact: true }).click();
	await expect(page.getByRole("link", { name: "Open asset", exact: true })).toBeVisible();
	await expect(uploadQueue.getByText("complete", { exact: true })).toHaveCount(2);
	const mediaResponse = await page.request.get("/api/collections/media?limit=100");
	expect(mediaResponse.ok()).toBe(true);
	const media = (await mediaResponse.json()) as {
		docs: { filename?: string; caption?: string }[];
	};
	expect(
		media.docs
			.filter((doc) =>
				["payload-field-notes.png", "payload-parity-cover.png"].includes(doc.filename ?? "")
			)
			.map(({ filename, caption }) => ({ filename, caption }))
			.sort((a, b) => (a.filename ?? "").localeCompare(b.filename ?? ""))
	).toEqual([
		{ filename: "payload-field-notes.png", caption: "Bulk workflow browser check" },
		{ filename: "payload-parity-cover.png", caption: "Bulk workflow browser check" },
	]);

	const remote = page.locator("details.ridu-bulk-upload-remote");
	await remote.getByText("Paste URL", { exact: true }).click();
	const remoteURL = remote.getByLabel("Asset URL", { exact: true });
	let remoteUploadRequests = 0;
	page.on("request", (request) => {
		if (
			request.method() === "POST" &&
			new URL(request.url()).pathname === "/api/collections/media/remote-upload"
		)
			remoteUploadRequests += 1;
	});
	await remoteURL.fill("not a url");
	await remote.getByRole("button", { name: "Import from URL", exact: true }).click();
	expect(await remoteURL.evaluate((input: HTMLInputElement) => input.validity.typeMismatch)).toBe(
		true
	);
	expect(remoteUploadRequests).toBe(0);

	await remoteURL.fill("http://127.0.0.1/private.png");
	await remote.getByLabel("Alt text", { exact: true }).fill("Private network image");
	await remote.getByRole("button", { name: "Import from URL", exact: true }).click();
	await expect(
		remote.getByText("remote upload URL resolves to a non-public address")
	).toBeVisible();
	expect(remoteUploadRequests).toBe(1);
	expect(consoleErrors).toEqual([
		"Failed to load resource: the server responded with a status of 422 (Unprocessable Entity)",
	]);
	consoleErrors.length = 0;
	await page
		.locator(".ridu-bulk-upload-picker > input")
		.first()
		.setInputFiles({
			name: "route-owned.txt",
			mimeType: "text/plain",
			buffer: Buffer.from("route-owned"),
		});
	await expect(page.getByText("route-owned.txt", { exact: true })).toBeVisible();
	await page.getByRole("link", { name: "Close", exact: true }).click();
	await expect(page.getByRole("dialog", { name: "Discard this upload queue?" })).toBeVisible();
	await page.getByRole("button", { name: "Discard files", exact: true }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/media(?:\?|$)/);
	await page.goto("/admin/collections/posts/upload");
	await expect(page.getByText("This collection is not configured for uploads.")).toBeVisible();
	await expect(page.getByRole("heading", { name: "Add files", exact: true })).toBeVisible();
	await expect(page.locator(".ridu-bulk-upload-picker > input")).toBeDisabled();
	const unavailableRemote = page.locator("details.ridu-bulk-upload-remote");
	await unavailableRemote.getByText("Paste URL", { exact: true }).click();
	await expect(unavailableRemote.getByLabel("Asset URL", { exact: true })).toBeDisabled();
	await expect(
		unavailableRemote.getByRole("button", { name: "Import from URL", exact: true })
	).toBeDisabled();
	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});

test("media drafts commit crops with metadata and retain a reversible original", async ({
	page,
}) => {
	const { consoleErrors, pageErrors } = observePageErrors(page);
	await loginAsEditor(page);
	await page.goto("/admin/collections/media/create");
	await page.locator('input[type="file"]').setInputFiles({
		name: "reversible.png",
		mimeType: "image/png",
		buffer: uploadFixturePng,
	});
	await page.getByLabel("Alt text", { exact: true }).fill("Reversible image");
	await page.getByRole("combobox", { name: "Kind", exact: true }).click();
	await page.getByRole("option", { name: "image", exact: true }).click();
	await page.getByRole("combobox", { name: "Credit", exact: true }).fill("Demo");
	await page.getByRole("option", { name: "Demo Author", exact: true }).click();
	await expect(page.getByRole("combobox", { name: "Credit", exact: true })).toHaveValue(
		"Demo Author"
	);
	await page.getByRole("button", { name: "Edit Image", exact: true }).click();
	const editor = page.getByRole("dialog", { name: /^Editing / });
	await expect.poll(async () => (await editor.boundingBox())?.x ?? Infinity).toBeLessThan(80);
	const selection = editor.locator(".ridu-crop-selection");
	await expect
		.poll(() => selection.evaluate((node) => node.getBoundingClientRect().width))
		.toBeGreaterThan(0);
	const eastHandle = editor.locator('cropper-handle[action="e-resize"]');
	const handleBounds = await eastHandle.boundingBox();
	if (!handleBounds) throw new Error("Crop resize handle was not mounted");
	const viewport = page.viewportSize();
	if (!viewport) throw new Error("Browser viewport was unavailable");
	const handleX = handleBounds.x + handleBounds.width / 2;
	const visibleTop = Math.max(0, handleBounds.y);
	const visibleBottom = Math.min(viewport.height, handleBounds.y + handleBounds.height);
	const handleY = (visibleTop + visibleBottom) / 2;
	const hit = await page.evaluate(
		({ x, y }) => {
			const element = document.elementFromPoint(x, y);
			return element instanceof Element
				? { tag: element.tagName, action: element.getAttribute("action") }
				: null;
		},
		{ x: handleX, y: handleY }
	);
	expect(hit).toEqual({ tag: "CROPPER-HANDLE", action: "e-resize" });
	await page.mouse.move(handleX, handleY);
	await page.mouse.down();
	await page.mouse.move(handleX - 50, handleY, { steps: 5 });
	await page.mouse.up();
	await expect(editor.getByLabel("Width (px)")).not.toHaveValue("20");
	await editor.getByRole("button", { name: "Reset", exact: true }).first().click();
	const corner = editor.locator('cropper-handle[action="se-resize"]');
	await corner.scrollIntoViewIfNeeded();
	const cornerBounds = await corner.boundingBox();
	if (!cornerBounds) throw new Error("Diagonal crop resize handle was not mounted");
	const cornerX = cornerBounds.x + cornerBounds.width / 2;
	const cornerY = cornerBounds.y + cornerBounds.height / 2;
	const cornerHit = await page.evaluate(
		({ x, y }) => document.elementFromPoint(x, y)?.getAttribute("action"),
		{ x: cornerX, y: cornerY }
	);
	expect(cornerHit).toBe("se-resize");
	await page.mouse.move(cornerX, cornerY);
	await page.mouse.down();
	await page.mouse.move(cornerX - 50, cornerY - 50, { steps: 5 });
	await page.mouse.up();
	await expect
		.poll(async () => Number(await editor.getByLabel("Width (px)").inputValue()))
		.toBeLessThan(20);
	await expect
		.poll(async () => Number(await editor.getByLabel("Height (px)").inputValue()))
		.toBeLessThan(20);
	await editor.getByRole("button", { name: "Reset", exact: true }).first().click();
	await editor.getByLabel("Width (px)").fill("16");
	await editor.getByLabel("Height (px)").fill("12");
	await editor.getByRole("button", { name: "Focal Point", exact: true }).press("ArrowRight");
	await expect(editor.getByLabel("X %", { exact: true })).toHaveValue("51");
	await editor.getByRole("button", { name: "Apply Changes", exact: true }).click();
	await expect(editor).not.toBeVisible();
	await expect(page.locator(".ridu-upload")).toContainText("16 × 12");
	await expect(page).toHaveURL(/media\/create/);
	const createdResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			/\/api\/collections\/media(?:\?|$)/.test(response.url())
	);
	await documentSaveButton(page).click();
	const created = await (await createdResponse).json();
	expect(created.doc).toMatchObject({
		alt: "Reversible image",
		width: 16,
		height: 12,
		_revision: 1,
	});
	await expect(page).toHaveURL(new RegExp(`media/${created.doc.id}`));
	const sourceKey = created.doc.source.objectKey;
	await page.reload();
	await page.getByRole("button", { name: "Edit Image", exact: true }).click();
	await expect(editor.getByLabel("Width (px)")).toHaveValue("16");
	await editor.getByRole("button", { name: "Reset", exact: true }).first().click();
	await expect(editor.getByLabel("Width (px)")).toHaveValue("20");
	await expect(editor.getByLabel("Height (px)")).toHaveValue("20");
	await editor.getByRole("button", { name: "Cancel", exact: true }).click();
	await expect(documentSaveButton(page)).toBeDisabled();
	await expect(page.locator(".ridu-upload")).toContainText("16 × 12");
	await page.getByRole("button", { name: "Edit Image", exact: true }).click();
	await expect(editor.getByLabel("Width (px)")).toHaveValue("16");
	await editor.getByRole("button", { name: "Reset", exact: true }).first().click();
	await editor.getByRole("button", { name: "Apply Changes", exact: true }).click();
	const updatedResponse = page.waitForResponse(
		(response) => response.request().method() === "PATCH" && response.url().includes("/upload")
	);
	await documentSaveButton(page).click();
	const updated = await (await updatedResponse).json();
	expect(updated.doc).toMatchObject({
		width: 20,
		height: 20,
		_revision: 2,
		source: { objectKey: sourceKey },
	});
	await expect(documentSaveButton(page)).toBeDisabled();
	await page.getByRole("button", { name: "Preview Sizes", exact: true }).click();
	const sizes = page.getByRole("dialog", { name: /^Sizes for / });
	await sizes.getByRole("button", { name: /^card / }).click();
	await expect(sizes.getByRole("region", { name: "card", exact: true })).toContainText("640 × 360");
	await sizes.getByRole("button", { name: /^thumbnail / }).click();
	await expect(sizes.getByRole("region", { name: "thumbnail", exact: true })).toContainText(
		"160 × 160"
	);
	await sizes.getByRole("button", { name: "Close", exact: true }).click();
	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});

test("an interrupted media save blocks retries until the committed document is reloaded", async ({
	page,
}) => {
	const { pageErrors } = observePageErrors(page);
	const navigation = await loginAsEditor(page);
	await navigation.getByRole("link", { name: "Media", exact: true }).click();
	await page.getByRole("link", { name: "Warm orange Ridu cover", exact: true }).click();
	await page.getByLabel("Alt text", { exact: true }).fill("Committed despite lost response");
	await page.getByRole("button", { name: "Edit Image", exact: true }).click();
	const editor = page.getByRole("dialog", { name: /^Editing / });
	await editor.getByLabel("Width (px)").fill("18");
	await editor.getByLabel("Height (px)").fill("12");
	await editor.getByRole("button", { name: "Apply Changes", exact: true }).click();
	let writes = 0;
	await page.route("**/api/collections/media/*/upload*", async (route) => {
		if (route.request().method() !== "PATCH") return route.continue();
		writes++;
		const response = await route.fetch();
		expect(response.ok()).toBe(true);
		await route.abort("failed");
	});
	await documentSaveButton(page).click();
	const reload = page.getByRole("button", { name: "Reload saved document", exact: true });
	await expect(reload).toBeVisible();
	await expect(documentSaveButton(page)).toBeDisabled();
	await page.getByLabel("Alt text", { exact: true }).press("Enter");
	await reload.click();
	await expect(reload).not.toBeVisible();
	await expect(page.getByLabel("Alt text", { exact: true })).toHaveValue(
		"Committed despite lost response"
	);
	await expect(page.locator(".ridu-upload")).toContainText("18 × 12");
	expect(writes).toBe(1);
	await expect(documentSaveButton(page)).toBeDisabled();
	expect(pageErrors).toEqual([]);
});
