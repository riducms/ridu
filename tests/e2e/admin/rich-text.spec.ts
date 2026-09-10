import { expect, test } from "./fixture";

import { documentSaveButton, loginAsEditor, observePageErrors, selectRichText } from "./helpers";

test("rich-text formatting, block insertion, embeds, movement, and persistence", async ({
	page,
}) => {
	test.setTimeout(60_000);
	const { consoleErrors, pageErrors } = observePageErrors(page);
	await loginAsEditor(page);
	consoleErrors.length = 0;
	await page.goto("/admin/collections/posts/create");
	let relationDialog = page.getByRole("dialog", { name: /Select asset/i });
	const richText = page.getByRole("textbox", { name: "Content" });
	await expect(richText).toBeVisible();
	await page.reload();
	await expect(richText).toBeVisible();
	await expect(richText).toHaveAttribute(
		"aria-keyshortcuts",
		"Alt+Shift+ArrowUp Alt+Shift+ArrowDown"
	);
	await richText.fill("Trail testing tells the truth.");
	await expect(richText).toHaveCSS("outline-style", "none");
	await selectRichText(page, richText);
	await expect(page.getByRole("toolbar", { name: "Text formatting" })).toBeVisible();
	const bold = page.getByRole("button", { name: "Bold" });
	await bold.click();
	await expect(bold).toHaveAttribute("aria-pressed", "true");
	await expect(richText.locator("strong")).toHaveText("Trail testing tells the truth.");
	const italic = page.getByRole("button", { name: "Italic" });
	await italic.click();
	await expect(italic).toHaveAttribute("aria-pressed", "true");
	await expect(richText.locator(".ridu-richtext-italic")).toHaveText(
		"Trail testing tells the truth."
	);
	await expect(richText.locator(".ridu-richtext-italic")).toHaveCSS("font-style", "italic");
	const underline = page.getByRole("button", { name: "Underline" });
	await underline.click();
	await expect(underline).toHaveAttribute("aria-pressed", "true");
	await expect(richText.locator(".ridu-richtext-underline")).toHaveText(
		"Trail testing tells the truth."
	);
	await expect(page.getByRole("button", { name: "Subscript" })).toBeVisible();
	await expect(page.getByRole("button", { name: "Superscript" })).toBeVisible();

	const addLink = page.getByRole("button", { name: "Add link" });
	await addLink.click();
	const linkInput = page.getByRole("textbox", { name: "Link URL" });
	await expect(linkInput).toBeFocused();
	await expect(addLink).toHaveAttribute("aria-expanded", "true");
	await linkInput.press("Escape");
	await expect(linkInput).toBeHidden();
	await expect(richText).toBeFocused();
	await selectRichText(page, richText);
	await addLink.click();
	await expect(linkInput).toBeFocused();
	const outsideField = page.getByLabel("Summary — English", { exact: true });
	await outsideField.click();
	await expect(linkInput).toBeHidden();
	await expect(outsideField).toBeFocused();
	await richText.click();
	await selectRichText(page, richText);
	await addLink.click();
	await expect(linkInput).toBeFocused();
	await linkInput.fill("ridu.dev");
	await page.getByRole("button", { name: "Apply" }).click();
	await expect(richText.locator("a")).toHaveAttribute("href", "https://ridu.dev");

	await expect(richText).toBeFocused();
	await richText.press("ArrowRight");
	await richText.press("Enter");
	await page.emulateMedia({ reducedMotion: "reduce" });
	await page.keyboard.type("/");
	const insertBlock = page.getByRole("listbox", { name: "Insert block" });
	await expect(insertBlock).toBeVisible();
	await expect(page.getByRole("listbox")).toHaveCount(1);
	const controlledMenuID = await richText.getAttribute("aria-controls");
	expect(controlledMenuID).toBeTruthy();
	await expect(insertBlock).toHaveAttribute("id", controlledMenuID!);
	const initialActiveDescendant = await richText.getAttribute("aria-activedescendant");
	expect(initialActiveDescendant).toBeTruthy();
	await expect(insertBlock.locator(`#${initialActiveDescendant}`)).toHaveCount(1);
	await richText.press("ArrowDown");
	const nextActiveDescendant = await richText.getAttribute("aria-activedescendant");
	expect(nextActiveDescendant).toBeTruthy();
	expect(nextActiveDescendant).not.toBe(initialActiveDescendant);
	const headingOption = insertBlock.getByRole("option", { name: /Heading 2/ });
	await headingOption.hover();
	const headingOptionID = await headingOption.getAttribute("id");
	expect(headingOptionID).toBeTruthy();
	await expect(richText).toHaveAttribute("aria-activedescendant", headingOptionID!);
	const slashMenuSurface = insertBlock.locator("[data-richtext-menu-surface]");
	await expect(slashMenuSurface).toHaveCSS("animation-name", "none");
	await slashMenuSurface.evaluate(async (element) => {
		await Promise.all(element.getAnimations().map((animation) => animation.finished));
	});
	const slashMenuBox = await slashMenuSurface.boundingBox();
	const viewport = page.viewportSize();
	expect(slashMenuBox).not.toBeNull();
	expect(viewport).not.toBeNull();
	expect(slashMenuBox!.x).toBeGreaterThanOrEqual(7);
	expect(slashMenuBox!.y).toBeGreaterThanOrEqual(7);
	expect(slashMenuBox!.x + slashMenuBox!.width).toBeLessThanOrEqual(viewport!.width - 7);
	expect(slashMenuBox!.y + slashMenuBox!.height).toBeLessThanOrEqual(viewport!.height - 7);
	const scrollDelta = await richText.evaluate((element) => {
		let scrollContainer = element.parentElement;
		while (scrollContainer !== null) {
			const overflowY = getComputedStyle(scrollContainer).overflowY;
			if (
				(overflowY === "auto" || overflowY === "scroll") &&
				scrollContainer.scrollHeight > scrollContainer.clientHeight
			) {
				const before = scrollContainer.scrollTop;
				scrollContainer.scrollTop += before >= 24 ? -24 : 24;
				return scrollContainer.scrollTop - before;
			}
			scrollContainer = scrollContainer.parentElement;
		}
		return 0;
	});
	expect(Math.abs(scrollDelta)).toBe(24);
	await expect
		.poll(async () => {
			const currentMenuBox = await slashMenuSurface.boundingBox();
			if (currentMenuBox === null) return false;
			return (
				currentMenuBox.x >= 7 &&
				currentMenuBox.y >= 7 &&
				currentMenuBox.x + currentMenuBox.width <= viewport!.width - 7 &&
				currentMenuBox.y + currentMenuBox.height <= viewport!.height - 7
			);
		})
		.toBe(true);
	await headingOption.click();
	await expect(richText).not.toHaveAttribute("aria-controls");
	await expect(richText).not.toHaveAttribute("aria-activedescendant");
	await page.keyboard.type("What broke, and where");
	await expect(richText.locator("h2")).toHaveText("What broke, and where");
	await richText.press("ControlOrMeta+End");
	await richText.press("Enter");
	await page.keyboard.type("/");
	await expect(page.getByRole("listbox", { name: "Insert block" })).toBeVisible();
	await richText.press("Escape");
	await expect(page.getByRole("listbox", { name: "Insert block" })).toBeHidden();
	await expect(richText).not.toHaveAttribute("aria-controls");
	await expect(richText).not.toHaveAttribute("aria-activedescendant");
	await richText.press("Backspace");

	await richText.locator("h2").hover();
	const blockActions = page.getByRole("toolbar", { name: "Block actions" });
	await expect(blockActions).toBeVisible();
	await expect(richText).toHaveCSS("border-left-width", "1px");
	await expect(richText).toHaveCSS("border-left-style", "solid");
	await expect(page.locator(".ridu-richtext-editor")).toHaveCSS("border-top-width", "0px");
	await expect(blockActions.locator("..")).toHaveAttribute("draggable", "true");
	const editorBox = await richText.boundingBox();
	const hoveredNodeBox = await richText.locator("h2").boundingBox();
	const blockActionsBox = await blockActions.boundingBox();
	const hoveredNodeLineHeight = await richText
		.locator("h2")
		.evaluate((element) => Number.parseFloat(getComputedStyle(element).lineHeight));
	const gripBox = await blockActions.locator(".ridu-richtext-block-grip").boundingBox();
	const addBlockButton = blockActions.getByRole("button", { name: "Add block" });
	await expect(blockActions.getByRole("button")).toHaveCount(1);
	await expect(
		blockActions.getByRole("button", {
			name: /Cycle block alignment|Outdent block|Indent block/,
		})
	).toHaveCount(0);
	const addBlockBox = await addBlockButton.boundingBox();
	expect(editorBox).not.toBeNull();
	expect(hoveredNodeBox).not.toBeNull();
	expect(blockActionsBox).not.toBeNull();
	expect(gripBox).not.toBeNull();
	expect(addBlockBox).not.toBeNull();
	const hoveredNodeFirstLineCenter = hoveredNodeBox!.y + hoveredNodeLineHeight / 2;
	const blockActionsCenter = blockActionsBox!.y + blockActionsBox!.height / 2;
	expect(Math.abs(blockActionsCenter - hoveredNodeFirstLineCenter)).toBeLessThanOrEqual(1);
	const gripCenter = gripBox!.x + gripBox!.width / 2;
	expect(Math.abs(gripCenter - editorBox!.x)).toBeLessThanOrEqual(1);

	await richText.locator("p").first().hover();
	const paragraphBox = await richText.locator("p").first().boundingBox();
	const paragraphToolbarBox = await blockActions.boundingBox();
	const paragraphLineHeight = await richText
		.locator("p")
		.first()
		.evaluate((element) => Number.parseFloat(getComputedStyle(element).lineHeight));
	expect(paragraphBox).not.toBeNull();
	expect(paragraphToolbarBox).not.toBeNull();
	expect(
		Math.abs(
			paragraphToolbarBox!.y +
				paragraphToolbarBox!.height / 2 -
				(paragraphBox!.y + paragraphLineHeight / 2)
		)
	).toBeLessThanOrEqual(1);

	await richText.locator("h2").hover();
	const returnedToolbarBox = await blockActions.boundingBox();
	expect(returnedToolbarBox).not.toBeNull();
	expect(
		Math.abs(returnedToolbarBox!.y + returnedToolbarBox!.height / 2 - hoveredNodeFirstLineCenter)
	).toBeLessThanOrEqual(1);
	expect(gripBox!.x).toBeLessThan(editorBox!.x);
	expect(addBlockBox!.x).toBeGreaterThan(editorBox!.x);
	await expect(addBlockButton).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
	await addBlockButton.hover();
	await expect
		.poll(() => addBlockButton.evaluate((element) => getComputedStyle(element).backgroundColor))
		.not.toBe("rgba(0, 0, 0, 0)");
	const blocksBeforePicker = await richText.locator(":scope > *").count();
	await addBlockButton.click();
	const blockPicker = page.getByRole("dialog", { name: "Insert block" });
	const blockFilter = blockPicker.getByRole("combobox", { name: "Filter blocks" });
	await expect(blockPicker).toBeVisible();
	await expect(blockFilter).toBeFocused();
	await expect(blockPicker.getByRole("option", { name: /Checklist/ })).toBeVisible();
	await expect(richText.locator(":scope > *")).toHaveCount(blocksBeforePicker);
	const pickerBox = await blockPicker.boundingBox();
	expect(pickerBox).not.toBeNull();
	expect(pickerBox!.x).toBeGreaterThanOrEqual(blockActionsBox!.x + blockActionsBox!.width);
	const viewportHeight = page.viewportSize()?.height;
	expect(viewportHeight).toBeDefined();
	await expect
		.poll(async () => {
			const settledPickerBox = await blockPicker.boundingBox();
			if (settledPickerBox === null || viewportHeight === undefined) return Infinity;
			const collisionSafeY = Math.min(blockActionsBox!.y, viewportHeight - settledPickerBox.height);
			return Math.abs(settledPickerBox.y - collisionSafeY);
		})
		.toBeLessThanOrEqual(1);
	await blockFilter.press("Escape");
	await expect(blockPicker).toBeHidden();
	await expect(richText).toBeFocused();
	await expect(richText.locator(":scope > *")).toHaveCount(blocksBeforePicker);
	await richText.locator("h2").hover();
	await addBlockButton.click();
	await expect(blockFilter).toBeFocused();
	// The dismissible layer attaches after the popover mounts; exercise outside-click after it paints.
	await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())));
	await outsideField.click();
	await expect(blockPicker).toBeHidden();
	await expect(outsideField).toBeFocused();

	await richText.locator("h2").hover();
	await addBlockButton.click();
	await blockFilter.fill("asset");
	const assetCommand = page.getByRole("option", { name: /Asset Select or upload asset/ });
	await expect(assetCommand).toBeVisible();
	await blockFilter.press("Enter");
	relationDialog = page.getByRole("dialog", { name: /Select asset/i });
	await expect(relationDialog).toBeVisible();
	const referenceSearch = relationDialog.getByRole("searchbox");
	await expect(referenceSearch).toBeFocused();
	await expect(page.locator('[data-slot="tooltip-content"]')).toHaveCount(0);
	const firstAsset = relationDialog.getByRole("radio", { name: "Select ridu-cover.png" }).first();
	await firstAsset.click();
	await expect(firstAsset).toBeChecked();
	await relationDialog.getByRole("button", { name: "Select", exact: true }).click();
	const uploadedAsset = richText.getByRole("group", { name: /Asset: ridu-cover.png/ });
	await expect(uploadedAsset).toBeVisible();
	await expect(uploadedAsset.locator(".ridu-richtext-upload-preview")).toHaveCSS(
		"aspect-ratio",
		"16 / 7"
	);
	const uploadCaption = uploadedAsset.getByRole("textbox", { name: "Caption for ridu-cover.png" });
	await uploadCaption.fill("A trail-side code review");
	await expect(uploadCaption).toHaveValue("A trail-side code review");

	await uploadedAsset.getByRole("button", { name: "Edit ridu-cover.png" }).first().click();
	const assetEditor = page.getByRole("dialog", { name: "Warm orange Ridu cover" });
	await expect(assetEditor).toBeVisible();
	await expect(assetEditor.getByLabel("Alt text")).toBeVisible();
	await assetEditor.getByRole("button", { name: "Close relationship browser" }).click();

	await uploadedAsset.hover();
	const assetActions = uploadedAsset.getByLabel("Asset actions");
	await expect(assetActions).toHaveCSS("opacity", "1");
	await uploadedAsset.getByRole("button", { name: "Replace ridu-cover.png" }).click();
	relationDialog = page.getByRole("dialog", { name: /Select asset/i });
	await expect(relationDialog).toBeVisible();
	await expect(relationDialog.getByRole("searchbox")).toBeFocused();
	await expect(
		relationDialog.getByRole("radio", { name: "Select ridu-cover.png" }).first()
	).toBeChecked();
	await relationDialog.getByRole("searchbox").press("Shift+Tab");
	const closeReferenceBrowser = relationDialog.getByRole("button", {
		name: "Close relationship browser",
	});
	await expect(closeReferenceBrowser).toBeFocused();
	await expect(page.locator('[data-slot="tooltip-content"]')).toHaveText(
		"Close relationship browser"
	);
	await closeReferenceBrowser.click();
	await expect(relationDialog).toBeHidden();

	await uploadedAsset.hover();
	await uploadedAsset.getByRole("button", { name: "Remove ridu-cover.png" }).click();
	await expect(uploadedAsset).toBeHidden();
	await page.keyboard.press("ControlOrMeta+z");
	await expect(uploadedAsset).toBeVisible();
	await expect(
		uploadedAsset.getByRole("textbox", { name: "Caption for ridu-cover.png" })
	).toHaveValue("A trail-side code review");

	await richText.locator("h2").hover();
	await addBlockButton.click();
	await blockFilter.fill("related post");
	await blockPicker.getByRole("option", { name: /Related Post/ }).click();
	relationDialog = page.getByRole("dialog", { name: "Select post" });
	await expect
		.poll(() => relationDialog.evaluate((dialog) => dialog.contains(document.activeElement)))
		.toBe(true);
	await relationDialog.getByRole("searchbox").fill("Relationship field notes");
	await relationDialog.getByRole("radio", { name: /Relationship field notes/ }).click();
	await relationDialog.getByRole("button", { name: "Select", exact: true }).click();
	const embeddedPost = richText.getByRole("group", { name: /Post: Relationship field notes/ });
	await expect(embeddedPost).toBeVisible();
	await expect(embeddedPost.getByRole("button", { name: "Replace" })).toBeVisible();

	await richText.locator("p").first().hover();
	const dragContainer = page.locator(".ridu-richtext-block-toolbar").locator("..");
	const dataTransfer = await page.evaluateHandle(() => new DataTransfer());
	const headingBox = await richText.locator("h2").boundingBox();
	expect(headingBox).not.toBeNull();
	await dragContainer.dispatchEvent("dragstart", { dataTransfer });
	await richText.locator("h2").dispatchEvent("dragover", {
		clientX: headingBox!.x + headingBox!.width / 2,
		clientY: headingBox!.y + headingBox!.height / 2,
		dataTransfer,
	});
	await richText.locator("h2").dispatchEvent("drop", {
		clientX: headingBox!.x + headingBox!.width / 2,
		clientY: headingBox!.y + headingBox!.height / 2,
		dataTransfer,
	});
	await dragContainer.dispatchEvent("dragend", { dataTransfer });
	await expect
		.poll(() => richText.evaluate((element) => element.firstElementChild?.tagName))
		.toBe("H2");
	await richText.locator("h2").evaluate((heading) => {
		const range = document.createRange();
		range.selectNodeContents(heading);
		range.collapse(true);
		const selection = window.getSelection();
		selection?.removeAllRanges();
		selection?.addRange(range);
		(heading.parentElement as HTMLElement | null)?.focus();
		document.dispatchEvent(new Event("selectionchange"));
	});
	await page.keyboard.press("Alt+Shift+ArrowUp");
	await expect(page.locator("[data-richtext-movement-status]")).toContainText(
		"This block is already first."
	);
	await expect
		.poll(() => richText.evaluate((element) => element.firstElementChild?.tagName))
		.toBe("H2");

	await richText.locator("h2").evaluate((heading) => {
		const range = document.createRange();
		range.selectNodeContents(heading);
		range.collapse(true);
		const selection = window.getSelection();
		selection?.removeAllRanges();
		selection?.addRange(range);
		(heading.parentElement as HTMLElement | null)?.focus();
		document.dispatchEvent(new Event("selectionchange"));
	});
	await page.keyboard.press("Alt+Shift+ArrowDown");
	await expect(page.locator("[data-richtext-movement-status]")).toContainText(
		/Moved block to position 2 of \d+\./
	);
	await expect
		.poll(() => richText.evaluate((element) => element.firstElementChild?.tagName))
		.not.toBe("H2");
	await expect(richText).toBeFocused();
	await richText.press("ControlOrMeta+z");
	await expect
		.poll(() => richText.evaluate((element) => element.firstElementChild?.tagName))
		.toBe("H2");

	await documentSaveButton(page).click();
	await expect(
		page
			.locator("[data-sonner-toast][data-front='true']")
			.filter({ hasText: "The following fields are invalid (2):" })
	).toBeVisible();
	await expect(page.getByText("Title is required")).toBeVisible();
	await expect(page.getByText("Summary is required")).toBeVisible();
	const invalidFieldsToast = page
		.locator("[data-sonner-toast][data-front='true']")
		.filter({ hasText: "The following fields are invalid (2):" });
	await expect(invalidFieldsToast.getByText("Title", { exact: true })).toBeVisible();
	await expect(invalidFieldsToast.getByText("Summary", { exact: true })).toBeVisible();
	await expect(page.locator('input[name="title"]')).toBeFocused();
	expect(consoleErrors).toEqual([]);

	await page.getByLabel("Summary").fill("Browser contract");
	await page.locator('input[name="title"]').fill("Browser-created post");
	await page.getByRole("button", { name: "Status" }).click();
	await page.getByRole("option", { name: "Published" }).click();
	await documentSaveButton(page).click();
	await expect(page).not.toHaveURL(/\/create$/);
	await expect(page.locator('input[name="title"]')).toHaveValue("Browser-created post");
	await expect(
		page
			.locator("[data-sonner-toast][data-front='true']")
			.filter({ hasText: "Post successfully created." })
	).toBeVisible();

	await page.getByLabel("Summary").fill("");
	await documentSaveButton(page).click();
	await expect(page.getByText("Summary is required")).toBeVisible();
	await expect(page.getByLabel("Summary")).toBeFocused();
	await expect(
		page
			.locator("[data-sonner-toast][data-front='true']")
			.filter({ hasText: "The following field is invalid: Summary" })
	).toBeVisible();
	expect(consoleErrors).toEqual([]);

	await page.getByLabel("Summary").fill("Browser contract updated");
	await page.locator('input[name="title"]').fill("Browser-edited post");
	await documentSaveButton(page).click();
	await expect(page.locator('input[name="title"]')).toHaveValue("Browser-edited post");
	await expect(
		page
			.locator("[data-sonner-toast][data-front='true']")
			.filter({ hasText: "Updated successfully." })
	).toBeVisible();
	const directURL = page.url();
	await page.goto(directURL);
	await expect(page.locator('input[name="title"]')).toHaveValue("Browser-edited post");
	await expect(page.getByRole("textbox", { name: "Content" }).locator("h2")).toHaveText(
		"What broke, and where"
	);
	await expect(
		page.getByRole("textbox", { name: "Content" }).getByRole("group", {
			name: /Asset: ridu-cover.png/,
		})
	).toBeVisible();
	await expect(
		page
			.getByRole("textbox", { name: "Content" })
			.getByRole("textbox", { name: "Caption for ridu-cover.png" })
	).toHaveValue("A trail-side code review");
	await expect(
		page.getByRole("textbox", { name: "Content" }).getByRole("group", {
			name: /Post: Relationship field notes/,
		})
	).toBeVisible();
	await expect(page.locator(".vite-error-overlay, [data-nextjs-dialog]")).toHaveCount(0);
	expect(pageErrors).toEqual([]);
});
