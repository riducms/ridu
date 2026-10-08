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

test("keeps its own styles under the app's unlayered reset", async ({ browser, page }) => {
	// tests/contracts/editor_app/src/app.css resets lists, headings, links, buttons and
	// pseudo-elements and outlines focus outside any cascade layer, as UnoCSS's preflight does, and
	// resets lists in a layer declared before Ridu's, as Tailwind 4 does. Its `app` layer recolors
	// quotes.
	const styles = (target: Page) =>
		target
			.locator(".ridu-richtext-editor")
			.filter({ hasText: "A quote" })
			.evaluate((editor) => {
				const style = (selector: string, pseudoElement?: string) =>
					getComputedStyle(editor.querySelector(selector)!, pseudoElement);
				const checkbox =
					".ridu-richtext-list-item-checked, .ridu-richtext-list-item-unchecked, ul[data-list-type='check'] > li";
				return {
					bullets: style("ul").listStyleType,
					listIndent: style("li").marginInlineStart,
					spacing: style("p").marginBottom,
					heading: style("h2").fontSize,
					checkbox: style(checkbox, "::before").borderTopWidth,
					quote: style("blockquote").borderInlineStartColor,
				};
			});
	const expected = {
		bullets: "disc",
		listIndent: "40px",
		spacing: "8.8px",
		heading: "25px",
		checkbox: "1px",
		quote: "rgb(180, 83, 9)",
	};

	const server = await browser.newContext({ javaScriptEnabled: false });
	const serverPage = await server.newPage();
	await serverPage.goto("/");
	expect(await styles(serverPage)).toEqual(expected);
	await server.close();

	await page.goto("/");
	const story = page.getByRole("textbox", { name: "Story" });
	await expect(story).toHaveAttribute("contenteditable", "true");
	expect(await styles(page)).toEqual(expected);
	// The field draws no second outline inside the app's own focus style.
	await story.click();
	await expect(story).toHaveCSS("outline-style", "none");

	// Popups render at the end of the page, outside the editor, and keep their own styles too.
	await page.keyboard.press("ControlOrMeta+A");
	await expect(page.locator(".ridu-richtext-floating-toolbar")).toHaveCSS("padding-top", "4px");
	// A reset loaded after the editor's stylesheet wins ties on specificity, as `[type="submit"]`
	// and one class are.
	await page.addStyleTag({
		content: `button, [type="button"], [type="submit"] {
			background-color: transparent;
			background-image: none;
		}
		::placeholder {
			color: rgb(255, 0, 0);
		}`,
	});
	await page.keyboard.press("ControlOrMeta+K");
	const dialog = page.getByRole("dialog", { name: "Edit link" });
	await expect(dialog.getByRole("button", { name: "Save changes" })).not.toHaveCSS(
		"background-color",
		"rgba(0, 0, 0, 0)"
	);

	await page.keyboard.press("Escape");
	await story.locator("p").first().hover();
	await page
		.getByRole("toolbar", { name: "Block actions" })
		.getByRole("button", { name: "Add block", exact: true })
		.click();
	const search = page
		.getByRole("dialog", { name: "Insert block" })
		.getByRole("combobox", { name: "Filter blocks" });
	const placeholder = await search.evaluate(
		(input) => getComputedStyle(input, "::placeholder").color
	);
	expect(placeholder).not.toBe("rgb(255, 0, 0)");
});

test("saves the lists Markdown shortcuts make", async ({ page }) => {
	const problems = trackProblems(page);
	await page.goto("/");
	const story = page.getByRole("textbox", { name: "Story" });
	await expect(story).toHaveAttribute("contenteditable", "true");
	await story.click();
	await page.keyboard.press("End");
	// Lexical records a `*` or `+` bullet on the list, which the document doesn't store.
	await page.keyboard.press("Enter");
	await page.keyboard.type("* Starred");
	await page.keyboard.press("Enter");
	await page.keyboard.press("Enter");
	await page.keyboard.type("Between");
	await page.keyboard.press("Enter");
	await page.keyboard.type("+ Plus");

	const lists = async () => {
		const document = JSON.parse((await page.getByTestId("story").textContent()) ?? "null") as {
			root: { children: { type: string; children: { children: { text: string }[] }[] }[] };
		};
		return document.root.children
			.filter((node) => node.type === "list")
			.map((list) => list.children.map((item) => item.children[0]?.text));
	};
	await expect.poll(lists).toEqual([["Starred"], ["Plus"]]);
	expect(problems).toEqual([]);
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
