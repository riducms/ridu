import type { Locator, Page } from "@playwright/test";
import { expect, test } from "./fixture";

import { loginAsEditor, observePageErrors, selectRichText, submitDocumentForm } from "./helpers";

async function expectBlockActionsAligned(toolbar: Locator, block: Locator) {
	await expect
		.poll(async () => {
			const firstLineCenter = await block.evaluate((element) => {
				const bounds = element.getBoundingClientRect();
				return bounds.y + Number.parseFloat(getComputedStyle(element).lineHeight) / 2;
			});
			const bounds = await toolbar.boundingBox();
			return bounds === null ? Infinity : Math.abs(bounds.y + bounds.height / 2 - firstLineCenter);
		})
		.toBeLessThanOrEqual(1);
}

async function dragBlock(page: Page, source: Locator, target: Locator, targetFraction: number) {
	await target.scrollIntoViewIfNeeded();
	await source.hover();
	const grip = page.getByRole("button", { name: "Drag to move block" });
	await expect(grip).toBeVisible();
	await grip.hover();
	const start = await grip.boundingBox();
	const end = await target.boundingBox();
	expect(start).not.toBeNull();
	expect(end).not.toBeNull();
	const startX = start!.x + start!.width / 2;
	const startY = start!.y + start!.height / 2;
	await page.mouse.move(startX, startY);
	await page.mouse.down();
	await page.mouse.move(startX + 8, startY + 2, { steps: 4 });
	await page.mouse.move(end!.x + end!.width / 2, end!.y + end!.height * targetFraction, {
		steps: 16,
	});
	await expect(page.locator(".ridu-richtext-drop-line")).toHaveCSS("opacity", "0.8");
	await expect
		.poll(async () => {
			const marker = await page.locator(".ridu-richtext-drop-line").boundingBox();
			return marker === null ? Infinity : Math.abs(marker.x - end!.x);
		})
		.toBeLessThan(1);
	await page.mouse.up();
}

test("rich-text dragging replaces an empty paragraph and highlights the moved block", async ({
	page,
}) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts/create");
	const richText = page.getByRole("textbox", { name: "Content" });
	await richText.fill("First");
	await richText.press("End");
	await richText.press("Enter");
	await page.keyboard.type("A");
	await richText.press("Enter");
	await richText.press("Enter");
	await page.keyboard.type("B");
	await richText.press("Enter");
	await page.keyboard.type("Last");
	await expect(richText.locator("p")).toHaveText(["First", "A", "", "B", "Last"]);

	await dragBlock(page, richText.getByText("First"), richText.locator("p").nth(2), 0.5);
	await expect(richText.locator("p")).toHaveText(["A", "First", "B", "Last"]);
	await expect(richText).toBeFocused();
	const highlight = page.locator(".ridu-richtext-drop-highlight");
	await expect(highlight).toHaveCSS("opacity", "0.1");
	const blockRect = await richText
		.locator("p")
		.filter({ hasText: /^First$/ })
		.boundingBox();
	const highlightRect = await highlight.boundingBox();
	expect(blockRect).not.toBeNull();
	expect(highlightRect).not.toBeNull();
	expect(highlightRect!.x).toBeCloseTo(blockRect!.x - 4, 0);
	expect(highlightRect!.y).toBeCloseTo(blockRect!.y - 4, 0);
	expect(highlightRect!.width).toBeCloseTo(blockRect!.width + 8, 0);
	await expect(highlight).toHaveCSS("opacity", "0");

	await richText.press("ControlOrMeta+z");
	await expect(richText.locator("p")).toHaveText(["First", "A", "", "B", "Last"]);
	await page.emulateMedia({ reducedMotion: "reduce" });
	await page.evaluate(() => document.documentElement.setAttribute("dir", "rtl"));
	await dragBlock(page, richText.getByText("Last"), richText.locator("p").nth(2), 0.5);
	await expect(richText.locator("p")).toHaveText(["First", "A", "Last", "B"]);
	await expect(highlight).toHaveCSS("opacity", "0.1");
	await expect(highlight).toHaveCSS("opacity", "0");
	await richText.press("ControlOrMeta+z");
	await expect(richText.locator("p")).toHaveText(["First", "A", "", "B", "Last"]);
});

test("rich-text grip drops before and after block midpoints", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts/create");
	const richText = page.getByRole("textbox", { name: "Content" });
	await richText.fill("First");
	await richText.press("End");
	await richText.press("Enter");
	await page.keyboard.type("Second");
	await richText.press("Enter");
	await page.keyboard.type("Third");
	await expect(richText.locator("p")).toHaveText(["First", "Second", "Third"]);
	const scrollDelta = await richText.evaluate((element) => {
		let container = element.parentElement;
		while (container !== null) {
			const overflowY = getComputedStyle(container).overflowY;
			if (
				(overflowY === "auto" || overflowY === "scroll") &&
				container.scrollHeight > container.clientHeight
			) {
				const before = container.scrollTop;
				container.scrollTop += before >= 24 ? -24 : 24;
				return container.scrollTop - before;
			}
			container = container.parentElement;
		}
		return 0;
	});
	expect(Math.abs(scrollDelta)).toBe(24);

	await dragBlock(page, richText.getByText("Third"), richText.getByText("First"), 0.2);
	await expect(richText.locator("p")).toHaveText(["Third", "First", "Second"]);
	await expect(richText).toBeFocused();
	await dragBlock(page, richText.getByText("Third"), richText.getByText("Second"), 0.8);
	await expect(richText.locator("p")).toHaveText(["First", "Second", "Third"]);
	await expect(richText).toBeFocused();

	await richText.focus();
	await richText.press("ControlOrMeta+z");
	await expect(richText.locator("p")).toHaveText(["Third", "First", "Second"]);

	const footerLocator = page.locator(".ridu-richtext-footer");
	await footerLocator.scrollIntoViewIfNeeded();
	await richText.getByText("Third").hover();
	const grip = page.getByRole("button", { name: "Drag to move block" });
	await grip.hover();
	const start = await grip.boundingBox();
	const validTarget = await richText.getByText("Second").boundingBox();
	const footer = await footerLocator.boundingBox();
	expect(start).not.toBeNull();
	expect(validTarget).not.toBeNull();
	expect(footer).not.toBeNull();
	const viewport = page.viewportSize();
	expect(viewport).not.toBeNull();
	for (const [x, y] of [
		[start!.x + start!.width / 2, start!.y + start!.height / 2],
		[validTarget!.x + validTarget!.width / 2, validTarget!.y + validTarget!.height * 0.8],
		[footer!.x + footer!.width / 2, footer!.y + footer!.height / 2],
	]) {
		expect(x).toBeGreaterThanOrEqual(0);
		expect(x).toBeLessThan(viewport!.width);
		expect(y).toBeGreaterThanOrEqual(0);
		expect(y).toBeLessThan(viewport!.height);
	}
	await page.mouse.move(start!.x + start!.width / 2, start!.y + start!.height / 2);
	await page.mouse.down();
	await page.mouse.move(start!.x + start!.width / 2 + 8, start!.y + start!.height / 2 + 2, {
		steps: 4,
	});
	await page.mouse.move(
		validTarget!.x + validTarget!.width / 2,
		validTarget!.y + validTarget!.height * 0.8,
		{ steps: 16 }
	);
	await expect(page.locator(".ridu-richtext-drop-line")).toHaveCSS("opacity", "0.8");
	await page.mouse.move(footer!.x + footer!.width / 2, footer!.y + footer!.height / 2, {
		steps: 16,
	});
	await expect(page.locator(".ridu-richtext-drop-line")).toHaveCSS("opacity", "0");
	await page.mouse.up();
	await expect(richText.locator("p")).toHaveText(["Third", "First", "Second"]);
	await expect(richText).toBeFocused();

	await richText.getByText("Third").click();
	await richText.press("Alt+Shift+ArrowDown");
	await expect(richText.locator("p")).toHaveText(["First", "Third", "Second"]);
	await expect(richText).toBeFocused();
});

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
		"Alt+Shift+ArrowUp Alt+Shift+ArrowDown Control+K Meta+K"
	);
	await richText.fill("Trail testing tells the truth.");
	await expect(richText).toHaveCSS("outline-style", "none");
	await selectRichText(page, richText);
	await expect(page.getByRole("toolbar", { name: "Text formatting" })).toBeVisible();
	await page.getByRole("button", { name: "Text style", exact: true }).click();
	await page.getByRole("menuitem", { name: "Heading 2", exact: true }).click();
	await expect(richText.locator("h2")).toHaveText("Trail testing tells the truth.");
	await page.getByRole("button", { name: "Alignment", exact: true }).click();
	await page.getByRole("menuitem", { name: "Align center", exact: true }).click();
	await expect(richText.locator("h2")).toHaveCSS("text-align", "center");
	await page.getByRole("button", { name: "Increase indent", exact: true }).click();
	await expect(richText.locator("h2")).toHaveAttribute("style", /padding-inline-start/);
	await page.getByRole("button", { name: "Decrease indent", exact: true }).click();
	await expect(richText.locator("h2")).not.toHaveAttribute("style", /padding-inline-start/);
	await page.getByRole("button", { name: "Text style", exact: true }).click();
	await page.getByRole("menuitem", { name: "Paragraph", exact: true }).click();
	await page.getByRole("button", { name: "Alignment", exact: true }).click();
	await page.getByRole("menuitem", { name: "Align left", exact: true }).click();
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

	const outsideField = page.getByLabel("Summary — English", { exact: true });
	const addLink = page.getByRole("button", { name: "Add link" });
	await addLink.click();
	const linkDrawer = page.getByRole("dialog", { name: "Edit link" });
	await expect(linkDrawer.getByRole("textbox", { name: "Text to display" })).toBeFocused();
	await linkDrawer.getByRole("textbox", { name: "Link URL" }).fill("ridu.dev");
	await linkDrawer.getByRole("button", { name: "Save changes" }).click();
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
	expect(
		await richText.evaluate((element) => {
			const gutter = getComputedStyle(element, "::before");
			return [gutter.borderInlineStartWidth, gutter.borderInlineStartStyle];
		})
	).toEqual(["1px", "solid"]);
	await expect(page.locator(".ridu-richtext-editor")).toHaveCSS("border-top-width", "0px");
	const grip = blockActions.getByRole("button", { name: "Drag to move block" });
	await expect(grip).toHaveAttribute("draggable", "true");
	const editorBox = await richText.boundingBox();
	const gripBox = await grip.boundingBox();
	const addBlockButton = blockActions.getByRole("button", { name: "Add block" });
	await expect(blockActions.getByRole("button")).toHaveCount(2);
	await expect(
		blockActions.getByRole("button", {
			name: /Cycle block alignment|Outdent block|Indent block/,
		})
	).toHaveCount(0);
	const addBlockBox = await addBlockButton.boundingBox();
	expect(editorBox).not.toBeNull();
	expect(gripBox).not.toBeNull();
	expect(addBlockBox).not.toBeNull();
	await expectBlockActionsAligned(blockActions, richText.locator("h2"));
	const gripCenter = gripBox!.x + gripBox!.width / 2;
	expect(Math.abs(gripCenter - editorBox!.x)).toBeLessThanOrEqual(1);

	await richText.locator("p").first().hover();
	await expectBlockActionsAligned(blockActions, richText.locator("p").first());

	await richText.locator("h2").hover();
	await expectBlockActionsAligned(blockActions, richText.locator("h2"));
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
	const blockActionsBox = await blockActions.boundingBox();
	expect(blockActionsBox).not.toBeNull();
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
	const assetCommand = page.getByRole("option", { name: /^Asset$/ });
	await expect(assetCommand).toBeVisible();
	await blockFilter.press("Enter");
	relationDialog = page.getByRole("dialog", { name: /Select asset/i });
	await expect(relationDialog).toBeVisible();
	const referenceSearch = relationDialog.getByRole("searchbox");
	await expect(referenceSearch).toBeFocused();
	await expect(page.locator('[data-slot="tooltip-content"]')).toHaveCount(0);
	const firstAsset = relationDialog
		.getByRole("table", { name: "Results" })
		.getByRole("button", { name: "ridu-cover.png", exact: true });
	await firstAsset.click();
	await expect(relationDialog).toBeHidden();
	const uploadedAsset = richText.getByRole("group", { name: /Asset: ridu-cover.png/ });
	await expect(uploadedAsset).toBeVisible();
	const uploadPreview = uploadedAsset.locator(".ridu-richtext-upload-preview");
	await expect(uploadPreview.locator("img")).toBeVisible();
	await expect
		.poll(async () => {
			const box = await uploadPreview.boundingBox();
			return box === null ? 0 : box.width / box.height;
		})
		.toBeCloseTo(24 / 16, 1);
	const uploadBox = await uploadPreview.boundingBox();
	expect(uploadBox).not.toBeNull();
	expect(uploadBox!.width).toBeLessThanOrEqual(450);
	const uploadCaption = uploadedAsset.getByRole("textbox", { name: "Caption for ridu-cover.png" });
	await uploadCaption.fill("A trail-side code review");
	await expect(uploadCaption).toHaveValue("A trail-side code review");

	await uploadedAsset.getByRole("button", { name: "Edit ridu-cover.png" }).first().click();
	const assetEditor = page.getByRole("dialog", { name: "ridu-cover.png" });
	await expect(assetEditor).toBeVisible();
	await expect(assetEditor.getByLabel("Alt text", { exact: true })).toHaveValue(
		"Warm orange Ridu cover"
	);
	await assetEditor.getByRole("button", { name: "Close relationship browser" }).click();

	await uploadedAsset.hover();
	const assetActions = uploadedAsset.getByLabel("Asset actions");
	await expect(assetActions).toHaveCSS("opacity", "1");
	await uploadedAsset.getByRole("button", { name: "Replace ridu-cover.png" }).click();
	relationDialog = page.getByRole("dialog", { name: /Select asset/i });
	await expect(relationDialog).toBeVisible();
	await expect(relationDialog.getByRole("searchbox")).toBeFocused();
	await expect(
		relationDialog
			.getByRole("table", { name: "Results" })
			.getByRole("row")
			.filter({ hasText: "ridu-cover.png" })
	).toHaveAttribute("data-selected", "true");
	await relationDialog.getByRole("searchbox").press("Shift+Tab");
	const closeReferenceBrowser = relationDialog.getByRole("button", {
		name: "Close relationship browser",
	});
	await expect(closeReferenceBrowser).toBeFocused();
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
	await blockFilter.fill("relation");
	await blockPicker.getByRole("option", { name: "Relationship", exact: true }).click();
	await page
		.getByRole("dialog")
		.getByRole("combobox", { name: "Select a Collection to Browse" })
		.fill("Post");
	await page.getByRole("option", { name: "Post", exact: true }).click();
	relationDialog = page.getByRole("dialog", { name: "Select post" });
	await expect
		.poll(() => relationDialog.evaluate((dialog) => dialog.contains(document.activeElement)))
		.toBe(true);
	await relationDialog.getByRole("searchbox").fill("Relationship field notes");
	await relationDialog
		.getByRole("table", { name: "Results" })
		.getByRole("button", { name: "Relationship field notes", exact: true })
		.click();
	await expect(relationDialog).toBeHidden();
	const embeddedPost = richText.getByRole("group", { name: /Post: Relationship field notes/ });
	await expect(embeddedPost).toBeVisible();
	await expect(embeddedPost.getByRole("button", { name: "Replace" })).toBeVisible();

	await richText.locator("p").first().hover();
	await dragBlock(page, richText.locator("p").first(), richText.locator("h2"), 0.8);
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

	const publish = page
		.locator(".ridu-document-actions")
		.getByRole("button", { name: /^Publish( changes)?$/ });
	await publish.click();
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
	await page.getByRole("combobox", { name: "Status", exact: true }).click();
	await page.getByRole("option", { name: "Published" }).click();
	await publish.click();
	await expect(page).not.toHaveURL(/\/create$/);
	await expect(page.locator('input[name="title"]')).toHaveValue("Browser-created post");
	const publishedID = new URL(page.url()).pathname.split("/").at(-1);
	expect(publishedID).toBeTruthy();
	await expect
		.poll(async () => {
			const response = await page.request.get(`/api/collections/posts/${publishedID}?draft=false`);
			return response.ok() ? (await response.json()).doc : undefined;
		})
		.toMatchObject({ title: "Browser-created post", _status: "published" });

	await page.getByLabel("Summary").fill("");
	await publish.click();
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
	await submitDocumentForm(page);
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
