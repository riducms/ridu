import { expect, test } from "./fixture";

import { documentSaveButton, loginAsEditor, observePageErrors, uploadFixturePng } from "./helpers";

test("SEO plugin renders, generates from the unsaved draft, previews, and switches locale", async ({
	page,
}) => {
	test.setTimeout(45_000);
	const { consoleErrors, pageErrors } = observePageErrors(page);
	await loginAsEditor(page);
	consoleErrors.length = 0;
	await page.goto("/admin/collections/pages/create");

	await page.getByLabel("Title", { exact: true }).fill("A current unsaved SEO draft");
	const slug = page.getByLabel("Slug", { exact: true });
	await expect(slug).toHaveValue("a-current-unsaved-seo-draft");
	await expect(slug).toHaveAttribute("readonly", "");
	const seoTab = page.getByRole("tab", { name: "SEO", exact: true });
	await seoTab.click();
	await expect(seoTab).toHaveAttribute("aria-selected", "true");

	const overview = page.locator("[data-seo-overview]");
	await expect(overview).toContainText("0/3 checks are passing");
	await expect(overview.getByRole("heading")).toHaveCount(0);
	const title = page.getByLabel("Title — English", { exact: true });
	const description = page.getByLabel("Description — English", { exact: true });
	await expect(title).toBeVisible();
	await expect(description).toBeVisible();
	const emptyDescriptionBox = await description.boundingBox();
	expect(emptyDescriptionBox).not.toBeNull();
	expect(emptyDescriptionBox!.height).toBeGreaterThanOrEqual(60);
	const titleField = page.locator('[data-field-path="meta.title"]');
	const titleLabel = titleField.locator(".ridu-field-label");
	const generateTitle = titleField.getByRole("button", { name: "Auto-generate" });
	const [titleLabelBox, generateTitleBox] = await Promise.all([
		titleLabel.boundingBox(),
		generateTitle.boundingBox(),
	]);
	expect(titleLabelBox).not.toBeNull();
	expect(generateTitleBox).not.toBeNull();
	expect(
		Math.abs(
			titleLabelBox!.y +
				titleLabelBox!.height / 2 -
				(generateTitleBox!.y + generateTitleBox!.height / 2)
		)
	).toBeLessThanOrEqual(2);
	await generateTitle.click();
	await expect(title).toHaveValue("A current unsaved SEO draft | Ridu");
	await page
		.locator('[data-field-path="meta.description"]')
		.getByRole("button", { name: "Auto-generate" })
		.click();
	await expect(description).toHaveValue(/A current unsaved SEO draft/);
	const tooShort = page.locator('[data-length-status="tooShort"]');
	await expect(tooShort).toHaveCount(1);
	await expect(tooShort.locator(".ridu-seo-pill")).toHaveCSS("background-color", "rgb(255, 69, 0)");
	await expect(tooShort.locator(".ridu-seo-pill")).toHaveCSS("color", "rgb(20, 20, 20)");
	await expect(tooShort.locator(".ridu-seo-progress")).toHaveCSS("height", "2px");
	await expect(tooShort.locator(".ridu-seo-progress")).toHaveCSS(
		"background-color",
		"rgb(243, 243, 243)"
	);

	const preview = page.locator("[data-seo-preview]");
	await expect(preview).toContainText("A current unsaved SEO draft | Ridu");
	await expect(preview).toContainText("https://riducms.test/en/pages/a-current-unsaved-seo-draft");
	const previewCard = preview.locator(".ridu-seo-preview-card");
	const previewCardBox = await previewCard.boundingBox();
	expect(previewCardBox).not.toBeNull();
	expect(previewCardBox!.width).toBeLessThanOrEqual(600);
	await expect(previewCard).toHaveCSS("padding", "20px");
	await expect(preview.locator(".ridu-seo-preview-title")).toHaveCSS("font-size", "16px");
	const themeOverride = await page.addStyleTag({
		content: ":root { --seo-status-short: rgb(91, 61, 131); }",
	});
	await expect(tooShort.locator(".ridu-seo-pill")).toHaveCSS(
		"background-color",
		"rgb(91, 61, 131)"
	);
	await themeOverride.evaluate((element) => element.remove());
	await description.fill("One line.");
	await expect
		.poll(async () => (await description.boundingBox())?.height ?? 0)
		.toBeLessThan(emptyDescriptionBox!.height);
	const oneLineDescriptionBox = await description.boundingBox();
	expect(oneLineDescriptionBox).not.toBeNull();
	expect(oneLineDescriptionBox!.height).toBeGreaterThanOrEqual(38);
	await description.fill("First line\nSecond line\nThird line");
	await expect
		.poll(async () => (await description.boundingBox())?.height ?? 0)
		.toBeGreaterThan(oneLineDescriptionBox!.height);
	const imageField = page.locator('[data-field-path="meta.image"]');
	const intake = imageField.locator(".ridu-seo-image-intake");
	const droppedFile = await page.evaluateHandle(
		({ bytes }) => {
			const binary = atob(bytes);
			const data = Uint8Array.from(binary, (character) => character.charCodeAt(0));
			const transfer = new DataTransfer();
			transfer.items.add(new File([data], "seo-dropped.png", { type: "image/png" }));
			return transfer;
		},
		{ bytes: uploadFixturePng.toString("base64") }
	);
	await intake.dispatchEvent("dragover", { dataTransfer: droppedFile });
	await expect(intake).toHaveAttribute("data-dragging", "true");
	await intake.dispatchEvent("drop", { dataTransfer: droppedFile });
	const droppedImageBrowser = page.getByRole("dialog", { name: "New asset" });
	await expect(droppedImageBrowser.getByLabel("Filename", { exact: true })).toHaveValue(
		"seo-dropped.png"
	);
	await droppedImageBrowser.getByRole("button", { name: "Close relationship browser" }).click();
	const discardDrop = page.getByRole("alertdialog", { name: "Discard changes?" });
	await expect(discardDrop).toBeVisible();
	await discardDrop.getByRole("button", { name: "Discard", exact: true }).click();
	await expect(droppedImageBrowser).toBeHidden();
	const createNew = imageField.getByRole("button", { name: "Create new" });
	await expect(createNew).toBeEnabled();
	await createNew.click();
	const createImageBrowser = page.getByRole("dialog", { name: "New asset" });
	await expect(createImageBrowser).toBeVisible();
	await createImageBrowser.getByRole("button", { name: "Close relationship browser" }).click();
	await expect(createImageBrowser).toBeHidden();
	const chooseExisting = imageField.getByRole("button", { name: "Choose existing" });
	await expect(chooseExisting).toBeEnabled();
	await chooseExisting.click();
	const imageBrowser = page.getByRole("dialog", { name: "Select meta image — english" });
	await imageBrowser
		.getByRole("table", { name: "Results" })
		.getByRole("button", { name: "ridu-cover.png", exact: true })
		.click();
	await expect(imageBrowser).toBeHidden();
	const selectedImage = imageField.locator(".ridu-seo-image-selection");
	const selectedImageBox = await selectedImage.boundingBox();
	expect(selectedImageBox).not.toBeNull();
	expect(selectedImageBox!.height).toBe(60);
	await expect(selectedImage).toHaveCSS("padding", "10px");
	const selectedImageThumbnailBox = await selectedImage
		.locator(".ridu-seo-image-thumbnail")
		.boundingBox();
	expect(selectedImageThumbnailBox).not.toBeNull();
	expect(selectedImageThumbnailBox!.width).toBe(40);
	expect(selectedImageThumbnailBox!.height).toBe(40);
	const selectedImageLink = selectedImage.getByRole("link", {
		name: "ridu-cover.png",
		exact: true,
	});
	await expect(selectedImageLink).toBeVisible();
	await expect(selectedImageLink).toHaveAttribute("target", "_blank");
	await expect(selectedImage.locator("img")).toHaveAttribute("alt", "");
	await expect(selectedImage.locator(".ridu-seo-image-metadata")).toHaveText(
		/^\d+(?:[,.]\d+)? bytes — 24x16 — image\/png$/
	);
	await expect(selectedImage.getByRole("button", { name: "Replace image" })).toHaveCount(0);
	const inspectImage = selectedImage.getByRole("button", { name: "Inspect image" });
	await expect(inspectImage).toBeVisible();
	await expect(selectedImage.getByRole("button", { name: "Remove image" })).toBeVisible();
	await inspectImage.click();
	const selectedImageBrowser = page.getByRole("dialog", { name: "ridu-cover.png" });
	const selectedImageAlt = selectedImageBrowser.getByLabel("Alt text", { exact: true });
	await expect(selectedImageAlt).toHaveValue("Warm orange Ridu cover");
	await selectedImageAlt.fill("Updated SEO image alternative text");
	const replacementFile = selectedImageBrowser.locator('input[type="file"]');
	await expect(replacementFile).toBeEnabled();
	await replacementFile.setInputFiles({
		name: "seo-refreshed.png",
		mimeType: "image/png",
		buffer: uploadFixturePng,
	});
	const refreshedImageBrowser = page.getByRole("dialog", { name: "seo-refreshed.png" });
	await expect(refreshedImageBrowser.getByLabel("Filename", { exact: true })).toHaveValue(
		"seo-refreshed.png"
	);
	await refreshedImageBrowser.getByRole("button", { name: "Save", exact: true }).click();
	await expect(selectedImage.getByRole("link", { name: "seo-refreshed.png" })).toBeVisible();
	await refreshedImageBrowser
		.getByRole("button", { name: "Close relationship browser", exact: true })
		.click();
	await expect(refreshedImageBrowser).toBeHidden();
	await selectedImage.getByRole("button", { name: "Remove image" }).click();
	await expect(intake).toBeVisible();
	await chooseExisting.click();
	const replacementBrowser = page.getByRole("dialog", { name: "Select meta image — english" });
	await replacementBrowser
		.getByRole("table", { name: "Results" })
		.getByRole("button", { name: "seo-refreshed.png", exact: true })
		.click();
	await expect(replacementBrowser).toBeHidden();
	await expect(
		imageField.getByRole("link", { name: "seo-refreshed.png", exact: true })
	).toBeVisible();

	await page.goto("/admin/collections/pages");
	await page.getByRole("link", { name: "About Ridu", exact: true }).click();
	const existingSlug = page.getByLabel("Slug", { exact: true });
	await page.getByRole("button", { name: "Unlock slug", exact: true }).click();
	await existingSlug.fill("manual-about-path");
	await documentSaveButton(page).click();
	await expect(existingSlug).toHaveValue("manual-about-path");
	await expect(existingSlug).not.toHaveAttribute("readonly", "");
	await page.getByRole("tab", { name: "SEO", exact: true }).click();
	await page.getByLabel("Content locale", { exact: true }).click();
	await page.getByRole("menuitem", { name: "French (fr)", exact: true }).click();
	await page.getByRole("tab", { name: "SEO", exact: true }).click();
	await expect(page.locator("[data-seo-preview]")).toContainText(
		"https://riducms.test/fr/pages/manual-about-path"
	);
	await expect(page.getByRole("link", { name: "best practices" }).first()).toBeVisible();

	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});
