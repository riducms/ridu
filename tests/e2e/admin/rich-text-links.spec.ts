import type { Locator } from "@playwright/test";

import { expect, test, type Page } from "./fixture";

import { loginAsEditor, observePageErrors, selectRichText } from "./helpers";

async function selectEditorRange(editor: Locator, start: number, end: number) {
	await editor.evaluate(
		(root, offsets) => {
			const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
			const points: Array<{ node: Text; start: number; end: number }> = [];
			let position = 0;
			while (walker.nextNode()) {
				const node = walker.currentNode;
				if (!(node instanceof Text)) continue;
				points.push({ node, start: position, end: position + node.length });
				position += node.length;
			}
			const startPoint = points.find(
				({ start, end }) => offsets.start >= start && offsets.start <= end
			);
			const endPoint = points.find(({ start, end }) => offsets.end >= start && offsets.end <= end);
			if (startPoint === undefined || endPoint === undefined)
				throw new Error("rich-text selection offsets are outside the editor text");
			const range = document.createRange();
			range.setStart(startPoint.node, offsets.start - startPoint.start);
			range.setEnd(endPoint.node, offsets.end - endPoint.start);
			(root as HTMLElement).focus();
			const selection = window.getSelection();
			selection?.removeAllRanges();
			selection?.addRange(range);
			document.dispatchEvent(new Event("selectionchange"));
		},
		{ start, end }
	);
}

async function placeCaretInLink(page: Page) {
	await page
		.getByRole("textbox", { name: "Content" })
		.locator("a")
		.evaluate((link) => {
			const text = link.firstChild;
			const editor = link.closest<HTMLElement>("[contenteditable='true']");
			if (text === null || editor === null) throw new Error("editable link text is unavailable");
			editor.focus();
			const range = document.createRange();
			range.setStart(text, Math.min(1, text.textContent?.length ?? 0));
			range.collapse(true);
			const selection = window.getSelection();
			selection?.removeAllRanges();
			selection?.addRange(range);
			document.dispatchEvent(new Event("selectionchange"));
		});
}

test("rich-text links use a validated drawer and collapsed-link controls", async ({ page }) => {
	const { consoleErrors, pageErrors } = observePageErrors(page);
	await loginAsEditor(page);
	consoleErrors.length = 0;
	await page.goto("/admin/collections/posts/create");

	const editor = page.getByRole("textbox", { name: "Content" });
	await editor.fill("Ridu guide");
	await selectRichText(page, editor);
	await page.keyboard.press("ControlOrMeta+K");

	let drawer = page.getByRole("dialog", { name: "Edit link" });
	await expect(drawer).toBeVisible();
	const textInput = drawer.getByRole("textbox", { name: "Text to display" });
	const urlInput = drawer.getByRole("textbox", { name: "Link URL" });
	await expect(textInput).toBeFocused();
	await expect(textInput).toHaveValue("Ridu guide");
	await page.keyboard.press("Escape");
	await expect(drawer).toBeHidden();
	await expect(editor.locator("a")).toHaveCount(0);
	await expect(editor).toBeFocused();

	await selectRichText(page, editor);
	await page.keyboard.press("ControlOrMeta+K");
	drawer = page.getByRole("dialog", { name: "Edit link" });
	await textInput.fill("Ridu docs");
	await urlInput.fill("javascript:alert(1)");
	await drawer.getByRole("button", { name: "Save changes" }).click();
	await expect(drawer.getByText("Enter a valid web, email, or telephone URL.")).toBeVisible();
	await expect(editor.locator("a")).toHaveCount(0);

	await urlInput.fill("ridu.dev/docs");
	await drawer.getByRole("checkbox", { name: "Open in a new tab" }).click();
	await drawer.getByRole("button", { name: "Save changes" }).click();
	const link = editor.locator("a");
	await expect(link).toHaveText("Ridu docs");
	await expect(link).toHaveAttribute("href", "https://ridu.dev/docs");
	await expect(link).toHaveAttribute("target", "_blank");
	await expect(link).toHaveAttribute("rel", "noopener noreferrer");

	await placeCaretInLink(page);
	const preview = page.getByRole("dialog", { name: "Link options" });
	await expect(preview).toBeVisible();
	const previewURL = preview.getByRole("link", { name: "https://ridu.dev/docs" });
	await expect(previewURL).toBeVisible();
	const editPreviewLink = preview.getByRole("button", { name: "Edit link" });
	await page.keyboard.press("Tab");
	await expect(previewURL).toBeFocused();
	await page.keyboard.press("Tab");
	await expect(editPreviewLink).toBeFocused();
	await page.keyboard.press("Enter");
	await drawer.getByRole("textbox", { name: "Text to display" }).fill("Changed but cancelled");
	await drawer.getByRole("textbox", { name: "Link URL" }).fill("cancelled.example");
	await drawer.getByRole("button", { name: "Cancel" }).click();
	await expect(link).toHaveText("Ridu docs");
	await expect(link).toHaveAttribute("href", "https://ridu.dev/docs");

	await placeCaretInLink(page);
	await editPreviewLink.click();
	await drawer.getByRole("textbox", { name: "Text to display" }).fill("Ridu reference");
	await drawer.getByRole("textbox", { name: "Link URL" }).fill("/reference");
	await drawer.getByRole("checkbox", { name: "Open in a new tab" }).click();
	await drawer.getByRole("button", { name: "Save changes" }).click();
	await expect(link).toHaveText("Ridu reference");
	await expect(link).toHaveAttribute("href", "/reference");
	await expect(link).not.toHaveAttribute("target", "_blank");

	await placeCaretInLink(page);
	await preview.getByRole("button", { name: "Remove" }).click();
	await expect(editor.locator("a")).toHaveCount(0);
	await expect(editor).toContainText("Ridu reference");
	await page.keyboard.press("ControlOrMeta+z");
	await expect(editor.locator("a")).toHaveAttribute("href", "/reference");

	expect(pageErrors).toEqual([]);
	expect(consoleErrors).toEqual([]);
});

test("a mixed link selection edits the complete selected range", async ({ page }) => {
	const { consoleErrors, pageErrors } = observePageErrors(page);
	await loginAsEditor(page);
	consoleErrors.length = 0;
	await page.goto("/admin/collections/posts/create");

	const editor = page.getByRole("textbox", { name: "Content" });
	await editor.fill("Linked plain");
	await selectEditorRange(editor, 0, 6);
	await page.keyboard.press("ControlOrMeta+K");
	let drawer = page.getByRole("dialog", { name: "Edit link" });
	await drawer.getByRole("textbox", { name: "Link URL" }).fill("linked.example");
	await drawer.getByRole("button", { name: "Save changes" }).click();
	await expect(editor.locator("a")).toHaveText("Linked");

	await selectEditorRange(editor, 6, 12);
	const toolbar = page.getByRole("toolbar", { name: "Text formatting" });
	await toolbar.getByRole("button", { name: "Add link", exact: true }).click();
	await expect(drawer.getByRole("textbox", { name: "Text to display" })).toHaveValue(" plain");
	await drawer.getByRole("button", { name: "Cancel" }).click();

	await selectEditorRange(editor, 0, 12);
	await page.keyboard.press("ControlOrMeta+K");
	drawer = page.getByRole("dialog", { name: "Edit link" });
	await expect(drawer.getByRole("textbox", { name: "Text to display" })).toHaveValue(
		"Linked plain"
	);
	await expect(drawer.getByRole("textbox", { name: "Link URL" })).toHaveValue("");
	await drawer.getByRole("textbox", { name: "Link URL" }).fill("complete.example");
	await drawer.getByRole("button", { name: "Save changes" }).click();
	await expect(editor.locator("a")).toHaveText("Linked plain");
	await expect(editor.locator("a")).toHaveAttribute("href", "https://complete.example");

	expect(pageErrors).toEqual([]);
	expect(consoleErrors).toEqual([]);
});
