import { expect, test } from "./fixture";

import {
	documentSaveButton,
	expectRiduSliderValue,
	loginAsEditor,
	observePageErrors,
	setRiduSliderValue,
	uploadFixturePng,
} from "./helpers";

test("bulk uploads validate metadata, reject private URLs, and discard route-owned queues", async ({
	page,
}) => {
	const { consoleErrors, pageErrors } = observePageErrors(page);
	const collectionsNavigation = await loginAsEditor(page);
	consoleErrors.length = 0;
	await collectionsNavigation.getByRole("link", { name: "Media", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Media" })).toBeVisible();
	await page.getByRole("link", { name: "Bulk upload" }).click();
	await expect(page.getByRole("heading", { name: "Bulk upload" })).toBeVisible();
	const bulkUploadInput = page.locator('input[type="file"]');
	await expect(bulkUploadInput).toBeEnabled();
	await bulkUploadInput.setInputFiles([
		{ name: "payload-field-notes.png", mimeType: "image/png", buffer: uploadFixturePng },
		{ name: "payload-parity-cover.png", mimeType: "image/png", buffer: uploadFixturePng },
	]);
	const uploadQueue = page.getByRole("list", { name: "Upload queue" });
	const fieldNotesRow = uploadQueue.getByRole("listitem").filter({
		hasText: "payload-field-notes.png",
	});
	const parityCoverRow = uploadQueue.getByRole("listitem").filter({
		hasText: "payload-parity-cover.png",
	});
	await expect(fieldNotesRow.getByLabel("Alt text")).toHaveValue("payload field notes");
	await expect(parityCoverRow.getByLabel("Alt text")).toHaveValue("payload parity cover");
	await fieldNotesRow.getByLabel("Alt text").fill("");
	await page.getByRole("button", { name: "Upload 2" }).click();
	await expect(page.getByText(/Alt text.*required/i)).toBeVisible();
	await expect(fieldNotesRow.getByLabel("Alt text")).toBeFocused();
	await expect(page.getByRole("link", { name: "Open asset" })).toHaveCount(1);
	expect(consoleErrors).toEqual([
		"Failed to load resource: the server responded with a status of 422 (Unprocessable Entity)",
	]);
	consoleErrors.length = 0;
	await fieldNotesRow.getByLabel("Alt text").fill("payload field notes");
	await page.getByRole("button", { name: "Retry upload of payload-field-notes.png" }).click();
	await expect(page.getByRole("link", { name: "Open asset" })).toHaveCount(2);
	await expect(page.getByRole("link", { name: "Open asset" }).first()).toBeFocused();
	await page.getByLabel("Asset URL").fill("http://127.0.0.1/private.png");
	await page.getByLabel("URL import metadata alt").fill("Private network image");
	await page.getByRole("button", { name: "Import from URL" }).click();
	await expect(page.getByText("remote upload URL resolves to a non-public address")).toBeVisible();
	expect(consoleErrors).toEqual([
		"Failed to load resource: the server responded with a status of 422 (Unprocessable Entity)",
	]);
	consoleErrors.length = 0;
	await bulkUploadInput.setInputFiles({
		name: "route-owned.txt",
		mimeType: "text/plain",
		buffer: Buffer.from("route-owned"),
	});
	await expect(page.getByText("route-owned.txt", { exact: true })).toBeVisible();
	await page.evaluate(() => {
		history.pushState({}, "", "/admin/collections/posts/upload");
		window.dispatchEvent(new PopStateEvent("popstate"));
	});
	await expect(page.getByText("This collection is not configured for uploads.")).toBeVisible();
	await expect(page.getByText("No files queued yet.")).toBeVisible();
	await expect(page.locator('input[type="file"]')).toBeDisabled();
	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});

test("image editing recovers committed responses and blocks unknown outcomes", async ({ page }) => {
	test.setTimeout(45_000);
	const { consoleErrors, pageErrors } = observePageErrors(page);
	const collectionsNavigation = await loginAsEditor(page);
	consoleErrors.length = 0;
	await collectionsNavigation.getByRole("link", { name: "Media", exact: true }).click();
	await page.getByRole("link", { name: "Warm orange Ridu cover", exact: true }).click();
	const assetPreview = page.getByRole("region", { name: "Asset preview" });
	await expect(assetPreview.getByRole("img", { name: "Warm orange Ridu cover" })).toBeVisible();
	await expect(assetPreview).toContainText("24 × 16");
	await expect(page.getByRole("textbox", { name: "Alt text" })).toHaveValue(
		"Warm orange Ridu cover"
	);
	await expect(assetPreview).toContainText("ridu-cover.png");
	await expect(assetPreview).toContainText("image/png");
	await page.getByRole("button", { name: "More actions" }).click();
	await expect(page.getByRole("link", { name: "Download", exact: true })).toBeVisible();
	await page.keyboard.press("Escape");
	await expect(page.getByLabel("Filename")).toHaveCount(0);
	await assetPreview.getByRole("button", { name: "Edit image" }).click();
	const imageEditor = page.getByRole("dialog", { name: /^Edit / });
	const focalPlane = imageEditor.getByRole("button", { name: "Choose image focal point" });
	const horizontalFocal = imageEditor.getByRole("slider", { name: /Horizontal/ });
	const verticalFocal = imageEditor.getByRole("slider", { name: /Vertical/ });
	await focalPlane.focus();
	await focalPlane.press("Enter");
	await expectRiduSliderValue(horizontalFocal, 50);
	await expectRiduSliderValue(verticalFocal, 50);
	await focalPlane.press("ArrowRight");
	await expectRiduSliderValue(horizontalFocal, 51);
	await focalPlane.press("Shift+ArrowUp");
	await expectRiduSliderValue(verticalFocal, 40);
	await expect(imageEditor.locator("[data-focal-position-status]")).toHaveText(
		"Focal point: 51% horizontal, 40% vertical."
	);
	await setRiduSliderValue(horizontalFocal, 20);
	await setRiduSliderValue(verticalFocal, 80);
	await imageEditor.getByRole("checkbox", { name: "Enable crop rectangle" }).check();
	await setRiduSliderValue(imageEditor.getByRole("slider", { name: /Left/ }), 15);
	await setRiduSliderValue(imageEditor.getByRole("slider", { name: /Top/ }), 10);
	await setRiduSliderValue(imageEditor.getByRole("slider", { name: /Width/ }), 70);
	await setRiduSliderValue(imageEditor.getByRole("slider", { name: /Height/ }), 75);
	const assetAlt = page.getByRole("textbox", { name: "Alt text" });
	await assetAlt.fill("Unsaved metadata survives regeneration");
	let releaseImageRegeneration!: () => void;
	let markImageRegenerationCommitted!: () => void;
	const imageRegenerationGate = new Promise<void>((resolve) => {
		releaseImageRegeneration = resolve;
	});
	const imageRegenerationCommitted = new Promise<void>((resolve) => {
		markImageRegenerationCommitted = resolve;
	});
	await page.route("**/api/collections/media/*/image", async (route) => {
		await route.fetch();
		markImageRegenerationCommitted();
		await imageRegenerationGate;
		await route.abort("failed");
	});
	await imageEditor.getByRole("button", { name: "Apply image edit" }).click();
	await imageRegenerationCommitted;
	await expect(assetPreview).toHaveAttribute("aria-busy", "true");
	await expect(horizontalFocal).toBeDisabled();
	await expect(assetAlt).toBeDisabled();
	await expect(imageEditor.getByRole("checkbox", { name: "Enable crop rectangle" })).toBeDisabled();
	releaseImageRegeneration();
	await expect(page.getByText("Image sizes regenerated.", { exact: true })).toBeVisible();
	await expect(
		page.getByText("The response was interrupted, but the committed image state was recovered.")
	).toBeVisible();
	await expect(assetAlt).toHaveValue("Unsaved metadata survives regeneration");
	expect(consoleErrors).toEqual(["Failed to load resource: net::ERR_FAILED"]);
	consoleErrors.length = 0;
	await assetAlt.fill("Warm orange Ridu cover");
	await page.unroute("**/api/collections/media/*/image");
	await page.reload();
	await assetPreview.getByRole("button", { name: "Edit image" }).click();
	await expect(imageEditor.getByRole("checkbox", { name: "Enable crop rectangle" })).toBeChecked();
	await expectRiduSliderValue(imageEditor.getByRole("slider", { name: /Width/ }), 70);
	const assetID = new URL(page.url()).pathname.split("/").at(-1)!;
	const imageEndpoint = `**/api/collections/media/${assetID}/image`;
	const documentEndpoint = (url: URL) => url.pathname === `/api/collections/media/${assetID}`;
	let staleDocumentWrites = 0;
	await page.route(imageEndpoint, async (route) => route.abort("failed"));
	await page.route(documentEndpoint, async (route) => {
		if (route.request().method() === "GET") {
			await route.abort("failed");
			return;
		}
		staleDocumentWrites++;
		await route.fulfill({ status: 500, json: { error: { message: "unexpected stale write" } } });
	});
	await assetAlt.fill("Unknown image outcome must block saving");
	await setRiduSliderValue(horizontalFocal, 35);
	await imageEditor.getByRole("button", { name: "Apply image edit" }).click();
	await expect(
		page
			.getByRole("main")
			.getByText(
				"The server may have committed the image change. Refresh before saving or trying again."
			)
	).toBeVisible();
	await expect(documentSaveButton(page)).toBeDisabled();
	await assetAlt.press("Enter");
	// A denied submit has no completion signal. Observe a bounded window for
	// forbidden writes instead of treating a still-disabled button as proof.
	await page.waitForTimeout(250);
	await expect(documentSaveButton(page)).toBeDisabled();
	expect(staleDocumentWrites).toBe(0);
	expect(consoleErrors.length).toBeGreaterThanOrEqual(2);
	expect(
		consoleErrors.every((message) => message === "Failed to load resource: net::ERR_FAILED")
	).toBe(true);
	consoleErrors.length = 0;
	await page.unroute(imageEndpoint);
	await page.unroute(documentEndpoint);
	await assetAlt.fill("Warm orange Ridu cover");
	await page.reload();
	await expect(assetAlt).toHaveValue("Warm orange Ridu cover");
	await assetPreview.getByRole("button", { name: "Preview sizes" }).click();
	const sizesDialog = page.getByRole("dialog", { name: /^Sizes for / });
	await sizesDialog.getByRole("button", { name: "card", exact: true }).click();
	await expect(sizesDialog).toContainText("640 × 360");
	await sizesDialog.getByRole("button", { name: "thumbnail", exact: true }).click();
	await expect(sizesDialog).toContainText("160 × 160");
	await sizesDialog.getByRole("button", { name: "Close" }).click();
	await assetPreview.getByRole("button", { name: "Edit image" }).click();
	await imageEditor.getByRole("button", { name: "Choose image focal point" }).press("Enter");
	await expectRiduSliderValue(imageEditor.getByRole("slider", { name: /Horizontal/ }), 50);
	await imageEditor.getByRole("button", { name: "Close" }).click();
	const originalAssetURL = page.url();
	await page.getByRole("button", { name: "More actions" }).click();
	await page.getByRole("button", { name: "Duplicate", exact: true }).last().click();
	await expect(page).not.toHaveURL(originalAssetURL);
	await expect(page.getByRole("textbox", { name: "Alt text" })).toHaveValue(
		"Warm orange Ridu cover"
	);
	await expect(assetPreview.getByRole("img", { name: "Warm orange Ridu cover" })).toBeVisible();
	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});
