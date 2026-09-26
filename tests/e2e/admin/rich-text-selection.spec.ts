import type { Locator, Page } from "@playwright/test";
import { expect, test } from "./fixture";

import { loginAsEditor, observePageErrors } from "./helpers";
import { block, bodyCards, bodyEditor, richDocument } from "./rich-text-block-fixture";

async function textEdge(block: Locator, edge: "start" | "end") {
	return block.evaluate((element, edge) => {
		const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
		const nodes: Text[] = [];
		while (walker.nextNode()) {
			const node = walker.currentNode;
			if (node instanceof Text && node.length > 0) nodes.push(node);
		}
		const node = edge === "start" ? nodes[0] : nodes.at(-1);
		if (node === undefined) throw new Error("Expected text in the rich-text block");
		const range = document.createRange();
		const offset = edge === "start" ? 0 : node.length - 1;
		range.setStart(node, offset);
		range.setEnd(node, offset + 1);
		const rect = range.getBoundingClientRect();
		return {
			x: edge === "start" ? rect.left + 1 : rect.right - 1,
			y: rect.top + rect.height / 2,
		};
	}, edge);
}

async function selectedText(page: Page) {
	return page.evaluate(() => window.getSelection()?.toString() ?? "");
}

async function selectionIntersects(node: Locator) {
	return node.evaluate((element) => {
		const selection = window.getSelection();
		return selection !== null && selection.rangeCount > 0
			? selection.getRangeAt(0).intersectsNode(element)
			: false;
	});
}

async function dragAcrossBlocks(
	page: Page,
	from: Locator,
	to: Locator,
	options: { reverse: boolean; selectedFragments: readonly string[]; spannedNode?: Locator }
) {
	const start = await textEdge(from, options.reverse ? "end" : "start");
	const end = await textEdge(to, options.reverse ? "start" : "end");
	await page.mouse.move(start.x, start.y);
	await page.mouse.down();
	await page.mouse.move(end.x, end.y, { steps: 12 });

	// The range must survive while the button is held, before any mouseup repair is possible.
	for (const fragment of options.selectedFragments)
		await expect.poll(() => selectedText(page)).toContain(fragment);
	if (options.spannedNode !== undefined)
		await expect.poll(() => selectionIntersects(options.spannedNode!)).toBe(true);
	await expect(page.getByRole("toolbar", { name: "Text formatting" })).toBeHidden();
	await page.mouse.up();
	for (const fragment of options.selectedFragments)
		await expect.poll(() => selectedText(page)).toContain(fragment);
	if (options.spannedNode !== undefined)
		await expect.poll(() => selectionIntersects(options.spannedNode!)).toBe(true);
	await expect(page.getByRole("toolbar", { name: "Text formatting" })).toBeVisible();
}

test("pointer selection stays intact across rich-text blocks in both directions", async ({
	page,
}) => {
	const { consoleErrors, pageErrors } = observePageErrors(page);
	await loginAsEditor(page);
	consoleErrors.length = 0;
	await page.goto("/admin/collections/posts/create");
	const editor = page.getByRole("textbox", { name: "Content" });
	await editor.click();
	await page.keyboard.type("First block keeps its selection.");
	await page.keyboard.press("Enter");
	await page.keyboard.type("Second block should stay selected too.");
	await page.keyboard.press("Enter");
	await page.keyboard.type("Third block finishes the range.");
	const blocks = editor.locator(":scope > p");
	await expect(blocks).toHaveCount(3);

	await dragAcrossBlocks(page, blocks.nth(2), blocks.nth(0), {
		reverse: true,
		selectedFragments: [
			"block keeps its selection",
			"Second block should stay selected too.",
			"block finishes the range",
		],
	});
	await page.keyboard.press("ArrowLeft");
	await dragAcrossBlocks(page, blocks.nth(0), blocks.nth(2), {
		reverse: false,
		selectedFragments: [
			"block keeps its selection",
			"Second block should stay selected too.",
			"block finishes the range",
		],
	});
	const toolbar = page.getByRole("toolbar", { name: "Text formatting" });
	await page.keyboard.press("Tab");
	await expect(toolbar.getByRole("button", { name: "Text style" })).toBeFocused();
	await toolbar.getByRole("button", { name: "Bold" }).click();
	await expect(blocks).toHaveText([
		"First block keeps its selection.",
		"Second block should stay selected too.",
		"Third block finishes the range.",
	]);
	await expect(blocks.nth(0).locator("strong")).toContainText("block keeps its selection");
	await expect(blocks.nth(1).locator("strong")).toHaveText(
		"Second block should stay selected too."
	);
	await expect(blocks.nth(2).locator("strong")).toContainText("block finishes the range");
	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});

test("selection across a heading, paragraph and embedded block keeps content through format and undo", async ({
	page,
}) => {
	const { consoleErrors, pageErrors } = observePageErrors(page);
	await loginAsEditor(page);
	consoleErrors.length = 0;
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Mixed selection",
			body: richDocument([
				{
					type: "heading",
					tag: "h2",
					children: [{ type: "text", text: "Heading starts the range" }],
				},
				{
					type: "paragraph",
					children: [{ type: "text", text: "Paragraph before the block" }],
				},
				block("callout", { title: "Embedded callout" }),
				{
					type: "paragraph",
					children: [{ type: "text", text: "Paragraph after the block" }],
				},
			]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const document = (await created.json()).doc;
	await page.goto(`/admin/collections/block-articles/${document.id}`);
	const editor = bodyEditor(page);
	const card = bodyCards(page).first();
	await expect(card).toContainText("Embedded callout");
	const blockRoot = editor.locator(":scope > .ridu-richtext-embedded");
	const order = () =>
		editor.evaluate((element) =>
			Array.from(element.children, (child) =>
				child.classList.contains("ridu-richtext-embedded") ? "BLOCK" : child.tagName
			)
		);
	await expect.poll(order).toEqual(["H2", "P", "BLOCK", "P"]);
	const key = await card.getAttribute("data-block-key");
	const expectOriginalContent = async () => {
		await expect(editor.locator(":scope > h2")).toHaveText("Heading starts the range");
		await expect(editor.locator(":scope > p")).toHaveText([
			"Paragraph before the block",
			"Paragraph after the block",
		]);
		await expect(card.locator(".ridu-richtext-block-card__summary")).toHaveText("Embedded callout");
	};

	await dragAcrossBlocks(page, editor.locator(":scope > p").last(), editor.locator(":scope > h2"), {
		reverse: true,
		selectedFragments: ["starts the range", "Paragraph before the block", "Paragraph after the"],
		spannedNode: blockRoot,
	});
	await page
		.getByRole("toolbar", { name: "Text formatting" })
		.getByRole("button", { name: "Bold" })
		.click();
	await expect(editor.locator(":scope > p").first().locator("strong")).toHaveText(
		"Paragraph before the block"
	);
	await expect(editor.locator(":scope > h2 strong")).toContainText("starts the range");
	await expect(editor.locator(":scope > p").last().locator("strong")).toContainText(
		"Paragraph after the"
	);
	await expect.poll(order).toEqual(["H2", "P", "BLOCK", "P"]);
	await expect(card).toHaveAttribute("data-block-key", key!);
	await expectOriginalContent();

	await editor.focus();
	await editor.press("ControlOrMeta+z");
	await expect(editor.locator("strong")).toHaveCount(0);
	await expect.poll(order).toEqual(["H2", "P", "BLOCK", "P"]);
	await expect(card).toHaveAttribute("data-block-key", key!);
	await expectOriginalContent();
	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});
