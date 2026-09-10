import { expect, test, type Page } from "./fixture";
import {
	chooseContentLocale,
	documentSaveButton,
	loginAsEditor,
	observePageErrors,
} from "./helpers";

async function captureSupport(page: Page) {
	await page.evaluate(() => {
		window.addEventListener("ridu-test-editor-capture", (event) => {
			(window as unknown as { capturedEditor: unknown }).capturedEditor = (
				event as CustomEvent
			).detail;
		});
	});
}
async function applyCaptured(page: Page, value: string) {
	return page.evaluate((value) => {
		try {
			(window as unknown as { capturedEditor: { set(value: string): void } }).capturedEditor.set(
				value
			);
			return "written";
		} catch (error) {
			return error instanceof Error ? error.message : String(error);
		}
	}, value);
}

test("API-created ordinary array rows mount plugin editors and keep their identities through save", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/pages?draft=true", {
		data: {
			title: "Ordinary array editors",
			notes: [{ heading: "First note" }, { heading: "Second note" }],
			layout: [{ blockType: "hero", heading: "Page heading" }],
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	const keys = original.notes.map((row: { _key: string }) => row._key);
	expect(keys).toEqual([expect.stringMatching(/^ridu_.+/), expect.stringMatching(/^ridu_.+/)]);
	expect(keys[0]).not.toBe(keys[1]);
	await page.goto(`/admin/collections/pages/${original.id}`);
	const firstEditor = page.locator('[data-field-path="notes.0.body"] [contenteditable="true"]');
	await firstEditor.fill("A plugin field inside an ordinary array.");
	await expect(page.locator('input[name="notes.0.heading"]')).toHaveAttribute(
		"data-local-editor",
		"text"
	);
	await page
		.locator('[data-field-path="notes"]')
		.getByRole("button", { name: "Open First note actions", exact: true })
		.click();
	await page.getByRole("menuitem", { name: "Move down", exact: true }).click();
	const movedEditor = page.locator('[data-field-path="notes.1.body"] [contenteditable="true"]');
	await expect(movedEditor).toHaveText("A plugin field inside an ordinary array.");
	await movedEditor.fill("Edited after moving.");
	await page.locator('input[name="notes.1.heading"]').fill("Moved note");
	await documentSaveButton(page).click();
	await expect
		.poll(
			async () =>
				(await (await page.request.get(`/api/collections/pages/${original.id}`)).json()).doc.notes
		)
		.toMatchObject([
			{ _key: keys[1], heading: "Second note" },
			{
				_key: keys[0],
				heading: "Moved note",
				body: { root: { children: [{ children: [{ text: "Edited after moving." }] }] } },
			},
		]);
	await page.reload();
	await expect(movedEditor).toHaveText("Edited after moving.");
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("local scalar editors retain occurrence identity, revoke removed bindings and preserve server semantics", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/pages?draft=true", {
		data: {
			title: "Local editor identity",
			layout: [
				{
					blockType: "hero",
					heading: "First",
					links: [{ label: "Nested first" }, { label: "Nested second" }],
				},
				{ blockType: "hero", heading: "Second" },
			],
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const original = (await created.json()).doc;
	await page.goto(`/admin/collections/pages/${original.id}`);
	await captureSupport(page);
	const first = page.locator('[data-field-path="layout.0.heading"]');
	await expect(first.locator("input")).toHaveAttribute("data-local-editor", "text");
	await first.getByRole("button", { name: "Capture editor" }).click();
	const layout = page.locator('[data-field-path="layout"]').first();
	await layout.getByRole("button", { name: "Open First actions", exact: true }).click();
	await page.getByRole("menuitem", { name: "Move down", exact: true }).click();
	expect(await applyCaptured(page, "Moved first")).toBe("written");
	await expect(page.locator('input[name="layout.1.heading"]')).toHaveValue("Moved first");
	await expect(page.locator('input[name="layout.0.heading"]')).toHaveValue("Second");
	await documentSaveButton(page).click();
	await expect
		.poll(
			async () =>
				(await (await page.request.get(`/api/collections/pages/${original.id}`)).json()).doc
					.layout[1].heading
		)
		.toBe("Moved first");
	// Saving resets the form; a previously retained capability cannot edit the new baseline.
	expect(await applyCaptured(page, "Obsolete save")).toContain("stale");
	await page
		.locator('[data-field-path="layout.1.heading"]')
		.getByRole("button", { name: "Capture editor" })
		.click();
	await layout.getByRole("button", { name: "Open Moved first actions", exact: true }).click();
	await page.getByRole("menuitem", { name: "Remove", exact: true }).click();
	expect(await applyCaptured(page, "Wrong replacement")).toContain("stale");
	await expect(page.locator('input[name="layout.0.heading"]')).toHaveValue("Second");
	await page.setViewportSize({ width: 390, height: 844 });
	await page.locator('input[name="layout.0.heading"]').focus();
	await expect(page.locator('input[name="layout.0.heading"]')).toBeFocused();
	const invalid = await page.request.patch(`/api/collections/pages/${original.id}`, {
		data: { layout: [{ ...original.layout[0], heading: 123 }] },
	});
	expect(invalid.status()).toBe(422);
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("captured local editor capabilities cannot cross document or locale navigation", async ({
	page,
}) => {
	await loginAsEditor(page);
	const ids: string[] = [];
	for (const title of ["First editor document", "Second editor document"]) {
		const response = await page.request.post("/api/collections/pages?draft=true", {
			data: { title, layout: [{ blockType: "hero", heading: title }] },
		});
		expect(response.ok(), await response.text()).toBe(true);
		ids.push((await response.json()).doc.id);
	}
	await page.goto(`/admin/collections/pages/${ids[0]}`);
	await captureSupport(page);
	await page.getByRole("button", { name: "Capture editor" }).click();
	await page
		.getByRole("navigation", { name: "Admin navigation" })
		.getByRole("link", { name: "Pages", exact: true })
		.click();
	await page.getByRole("link", { name: "Second editor document", exact: true }).click();
	await expect(page.locator('input[name="layout.0.heading"]')).toHaveValue(
		"Second editor document"
	);
	expect(await applyCaptured(page, "Wrong document")).toContain("stale");
	await page.getByRole("button", { name: "Capture editor" }).click();
	await chooseContentLocale(page, "French", "fr");
	await expect(page.locator('input[name="layout.0.heading"]')).toHaveValue(
		"Second editor document"
	);
	expect(await applyCaptured(page, "Wrong locale")).toContain("stale");
});
