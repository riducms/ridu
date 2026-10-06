import { expect, test } from "../e2e/admin/fixture";
import { loginAsEditor } from "../e2e/admin/helpers";
import {
	insertBlock,
	bodyEditor,
	bodyCards,
	richDocument,
	block,
} from "../e2e/admin/rich-text-block-fixture";

test("100 named cards keep header typing below 100 ms p95 with inline nested editors", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	await loginAsEditor(page);
	const children = Array.from({ length: 100 }, (_, index) =>
		block(index < 20 ? "named-callout" : "named-cta", {
			heading: `Content ${index}`,
			...(index < 20
				? {
						detail: richDocument([
							{
								type: "paragraph",
								version: 1,
								children: [{ type: "text", version: 1, text: "Inline nested content" }],
							},
						]),
					}
				: {}),
		})
	);
	const created = await page.request.post("/api/collections/block-names", {
		data: { title: "Named card performance", body: richDocument(children) },
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/block-names/${original.id}`);
	const names = bodyCards(page).getByRole("textbox", { name: "Block name", exact: true });
	await expect(names).toHaveCount(100);
	const editors = page.locator('.ridu-richtext-content[contenteditable="true"]');
	await expect(editors).toHaveCount(21);
	const editorInstances = await editors.count();
	const input = names.first();
	await input.evaluate((element) => {
		const samples: number[] = [];
		Object.assign(window, { riduNameTypingSamples: samples });
		element.addEventListener("input", () => {
			const start = performance.now();
			requestAnimationFrame(() =>
				requestAnimationFrame(() => samples.push(performance.now() - start))
			);
		});
	});
	for (const character of "Footer newsletter signup") {
		await input.press(character === " " ? "Space" : character);
		await page.evaluate(
			() =>
				new Promise<void>((resolve) =>
					requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
				)
		);
	}
	await expect(input).toHaveValue("Footer newsletter signup");
	await expect(input).toBeFocused();
	const samples = await page.evaluate(
		() => (window as Window & { riduNameTypingSamples: number[] }).riduNameTypingSamples
	);
	const p95 = [...samples].sort((a, b) => a - b)[Math.ceil(samples.length * 0.95) - 1]!;
	await testInfo.attach("block-name-performance.json", {
		body: JSON.stringify({
			cards: 100,
			nestedBodies: 20,
			editorInstances,
			samples,
			typingP95MS: p95,
		}),
		contentType: "application/json",
	});
	console.log(
		`BLOCK_NAME_BROWSER_PERFORMANCE ${JSON.stringify({ cards: 100, nestedBodies: 20, editorInstances, typingP95MS: p95 })}`
	);
	expect(samples).toHaveLength("Footer newsletter signup".length);
	expect(p95).toBeLessThan(100);
	await expect(editors).toHaveCount(editorInstances);
});

test("100 mixed cards measure inline editor typing and block actions", async ({
	page,
}, testInfo) => {
	test.setTimeout(60_000);
	await loginAsEditor(page);
	const children = Array.from({ length: 100 }, (_, index) =>
		index < 20
			? block("callout", {
					title: `Callout ${index}`,
					detail: richDocument([
						{
							type: "paragraph",
							version: 1,
							children: [{ type: "text", version: 1, text: "A nested rich-text body" }],
						},
					]),
				})
			: block("cta", { label: `CTA ${index}` })
	);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: { title: "Measured block fixture", body: richDocument(children) },
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	const editors = page.locator('.ridu-richtext-content[contenteditable="true"]');
	const navigationStart = Date.now();
	await page.goto(`/admin/collections/block-articles/${original.id}`);
	await expect(bodyCards(page)).toHaveCount(100);
	await expect(editors).toHaveCount(42);
	const coldMountMS = Date.now() - navigationStart;
	const warmStart = Date.now();
	await page.reload();
	await expect(bodyCards(page)).toHaveCount(100);
	await expect(editors).toHaveCount(42);
	const warmMountMS = Date.now() - warmStart;
	const editorInstances = await editors.count();
	const editor = bodyEditor(page);
	await page
		.locator(".ridu-richtext-editor")
		.filter({ has: editor })
		.locator(":scope > .ridu-richtext-footer")
		.click();
	await editor.press("Backspace");
	await editor.evaluate((element) => {
		const samples: number[] = [];
		Object.assign(window, { riduPhase4TypingSamples: samples });
		element.addEventListener("keydown", () => {
			const start = performance.now();
			requestAnimationFrame(() =>
				requestAnimationFrame(() => samples.push(performance.now() - start))
			);
		});
	});
	for (const character of "measured typing input") {
		await editor.press(character === " " ? "Space" : character);
		await page.evaluate(
			() =>
				new Promise<void>((resolve) =>
					requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
				)
		);
	}
	const typing = await page.evaluate(
		() => (window as Window & { riduPhase4TypingSamples: number[] }).riduPhase4TypingSamples
	);
	const insertions: number[] = [];
	await editor.evaluate((element) => {
		const samples: number[] = [];
		Object.assign(window, { riduPhase4InsertSamples: samples });
		element.addEventListener(
			"keydown",
			(event) => {
				if (event.key !== "Enter" || event.target !== element) return;
				const option = document.querySelector('[role="option"][aria-selected="true"]');
				if (option?.textContent?.trim() !== "Callout") return;
				const start = performance.now();
				requestAnimationFrame(() =>
					requestAnimationFrame(() => samples.push(performance.now() - start))
				);
			},
			{ capture: true }
		);
	});
	for (let index = 0; index < 3; index++) {
		const start = Date.now();
		const inserted = await insertBlock(page, editor, "Callout");
		await inserted
			.getByRole("textbox", { name: "Callout title", exact: true })
			.fill(`Measured insertion ${index}`);
		await expect(bodyCards(page)).toHaveCount(101 + index);
		await page.evaluate(
			() =>
				new Promise<void>((resolve) =>
					requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
				)
		);
		insertions.push(Date.now() - start);
	}
	await expect(editors).toHaveCount(48);
	const editorInstancesAfterInsert = await editors.count();
	const actions: number[] = [];
	for (let index = 0; index < 8; index++) {
		const select = bodyCards(page).first().getByRole("button", { name: "Select Callout block" });
		const start = Date.now();
		await select.press("Alt+Shift+ArrowDown");
		await page.evaluate(
			() =>
				new Promise<void>((resolve) =>
					requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
				)
		);
		actions.push(Date.now() - start);
	}
	const insertionPaint = await page.evaluate(
		() => (window as Window & { riduPhase4InsertSamples: number[] }).riduPhase4InsertSamples
	);
	const percentile = (samples: number[]) =>
		[...samples].sort((a, b) => a - b)[Math.ceil(samples.length * 0.95) - 1];
	const result = {
		fixture: "100 mixed cards / 20 nested bodies",
		editorInstances,
		editorInstancesAfterInsert,
		coldMountMS,
		warmMountMS,
		insertP95MS: percentile(insertionPaint),
		insertSamples: insertionPaint,
		insertWorkflowWallTimes: insertions,
		typingP95MS: percentile(typing),
		reorderP95MS: percentile(actions),
		typingSamples: typing,
		reorderSamples: actions,
	};
	await testInfo.attach("rich-text-performance.json", {
		body: JSON.stringify(result, null, 2),
		contentType: "application/json",
	});
	console.log(`RICHTEXT_BROWSER_PERFORMANCE ${JSON.stringify(result)}`);
	expect(insertionPaint).toHaveLength(3);
	expect(result.typingP95MS).toBeLessThan(100);
	expect(result.reorderP95MS).toBeLessThan(200);
	expect(result.insertP95MS).toBeLessThan(200);
});
