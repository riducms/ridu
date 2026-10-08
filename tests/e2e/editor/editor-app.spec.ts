import { expect, test, type Page } from "@playwright/test";

// tests/contracts/editor_app uses @riducms/plugin-richtext/editor in a SvelteKit app with SSR on.

/** Console errors and warnings, including Svelte's development warnings, and uncaught errors. */
function trackProblems(page: Page) {
	const problems: string[] = [];
	page.on("console", (message) => {
		if (message.type() === "error" || message.type() === "warning") problems.push(message.text());
	});
	page.on("pageerror", (error) => problems.push(error.message));
	return problems;
}

test("renders the document on the server and edits it in the browser", async ({
	page,
	request,
}) => {
	const response = await request.get("/");
	expect(response.ok()).toBe(true);
	const html = await response.text();
	expect(html).toContain("data-ridu-richtext-static");
	// A document the editor can't open shows recovery on the server too, instead of failing.
	expect(html).toContain("ridu-richtext-recovery");
	expect(html).toContain("Rendered on the server");
	expect(html).toContain("Écrivez une note");
	expect(html).not.toContain("contenteditable");

	const problems = trackProblems(page);
	await page.goto("/");

	const story = page.getByRole("textbox", { name: "Story" });
	await expect(story).toHaveAttribute("contenteditable", "true");
	await expect(page.locator("[data-ridu-richtext-static]")).toHaveCount(0);
	await story.click();
	await page.keyboard.press("End");
	await page.keyboard.type(" and edited");
	await expect(page.getByTestId("story")).toContainText("Rendered on the server and edited");

	// The default floating toolbar appears over a selection.
	await page.keyboard.press("Shift+Home");
	await expect(page.locator(".ridu-richtext-floating-toolbar")).toBeVisible();
	expect(problems).toEqual([]);
});

/** The box of each editor, its placeholder and every block of its document. */
function measureEditors(page: Page) {
	return page.locator(".ridu-richtext-editor").evaluateAll((editors) =>
		editors.map((editor) => {
			const parts = [
				editor,
				...editor.querySelectorAll(".ridu-richtext-placeholder"),
				...editor.querySelectorAll(".ridu-richtext-content > :not(.ridu-richtext-placeholder)"),
			];
			return parts.flatMap((part) => {
				const { top, left, width, height } = part.getBoundingClientRect();
				return [top, left, width, height].map(Math.round);
			});
		})
	);
}

test("keeps the server copy's layout when the editor mounts", async ({ browser, page }) => {
	const server = await browser.newContext({ javaScriptEnabled: false });
	const serverPage = await server.newPage();
	await serverPage.goto("/");
	await expect(serverPage.locator("[data-ridu-richtext-static]")).toHaveCount(4);
	const serverLayout = await measureEditors(serverPage);
	await server.close();

	await page.goto("/");
	await expect(page.locator("[contenteditable=true]")).toHaveCount(4);
	await expect.poll(() => measureEditors(page)).toEqual(serverLayout);
});

test("edits a document the app passes one-way", async ({ page }) => {
	const problems = trackProblems(page);
	await page.goto("/");
	const summary = page.getByRole("textbox", { name: "Summary" });
	await expect(summary).toHaveAttribute("contenteditable", "true");
	await summary.click();
	await page.keyboard.press("End");
	await page.keyboard.type(" and edited");
	await expect(page.getByTestId("summary")).toContainText("Passed one-way and edited");
	expect(problems).toEqual([]);
});

test("uses the app's language, messages and toolbar choice", async ({ page }) => {
	const problems = trackProblems(page);
	await page.goto("/");
	const note = page.getByRole("textbox", { name: "Note" });
	await expect(note).toHaveAttribute("contenteditable", "true");
	await expect(note).toHaveAttribute("aria-placeholder", "Écrivez une note");

	await note.click();
	await page.keyboard.type("/");
	await expect(page.getByRole("option", { name: "Liste à puces" })).toBeVisible();
	await page.keyboard.press("Escape");

	// The "/" stays, so fast typing reopens and closes the menu within milliseconds. bits-ui
	// before 2.19.5 then read a destroyed layer and Svelte warned derived_inert.
	await page.keyboard.type("Une note");

	// toolbar="fixed" shows the toolbar above the text and none over a selection.
	await page.keyboard.press("Shift+Home");
	await expect(page.locator(".ridu-richtext-fixed-toolbar-slot [role=toolbar]")).toBeVisible();
	await expect(page.locator(".ridu-richtext-floating-toolbar")).toHaveCount(0);
	expect(problems).toEqual([]);
});

test.describe("on a phone", () => {
	test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

	test("sizes the toolbar for fingers and keeps it in view with a keyboard", async ({ page }) => {
		// A phone's keyboard scrolls the visible area within the page; this stands in for one.
		await page.addInitScript(() => {
			Object.defineProperty(VisualViewport.prototype, "offsetTop", {
				get: () => (window as { keyboardOffset?: number }).keyboardOffset ?? 0,
			});
		});
		await page.goto("/");
		// The app's header covers the top 60px, so the toolbar sticks below it.
		await page.addStyleTag({ content: ":root { --ridu-richtext-sticky-offset: 60px; }" });
		const slot = page.locator(".ridu-richtext-fixed-toolbar-slot");
		await expect(slot).toHaveCSS("top", "60px");
		const button = slot.getByRole("button", { name: "Gras" });
		await expect(button).toBeVisible();
		expect(await button.boundingBox()).toMatchObject({ width: 44, height: 44 });

		await page.evaluate(() => {
			(window as { keyboardOffset?: number }).keyboardOffset = 120;
			window.visualViewport?.dispatchEvent(new Event("scroll"));
		});
		// The keyboard scrolled the visible area past the header, so the toolbar follows it.
		await expect(slot).toHaveCSS("top", "120px");
	});
});
