import { expect, test, type Page } from "./fixture";
import { loginAsEditor, observePageErrors } from "./helpers";

for (const width of [1440, 390]) {
	test(`history restores the primary list and document scrollers at ${width}px after delayed content`, async ({
		page,
	}) => {
		test.setTimeout(90_000);
		await loginAsEditor(page);
		await page.setViewportSize({ width, height: 760 });
		const errors = observePageErrors(page);
		for (let index = 0; index < 35; index++) {
			const response = await page.request.post("/api/collections/posts?locale=en&draft=true", {
				data: {
					title: `Scroll post ${String(index).padStart(2, "0")}`,
					summary: "Scroll restoration fixture",
				},
			});
			expect(response.ok(), await response.text()).toBe(true);
		}
		await page.goto("/admin/collections/posts?locale=en&limit=100&q=Scroll&sort=title");
		const results = page.locator('[data-slot="collection-list-results"]');
		await expect(results).toHaveAttribute("aria-busy", "false");
		const link = page.getByRole("link", { name: "Scroll post 25", exact: true });
		await link.scrollIntoViewIfNeeded();
		const listY = await position(page, width, "list");
		expect(listY).toBeGreaterThan(300);
		const listURL = page.url();
		await link.click();
		await expect(page.locator('input[name="title"]')).toHaveValue("Scroll post 25");
		await expect.poll(() => position(page, width, "document")).toBe(0);
		const documentURL = page.url();
		await scroll(page, width, "document", 320);
		const documentY = await position(page, width, "document");
		expect(documentY).toBeGreaterThan(100);

		let release!: () => void;
		let markStarted!: () => void;
		const pending = new Promise<void>((resolve) => {
			release = resolve;
		});
		const started = new Promise<void>((resolve) => {
			markStarted = resolve;
		});
		const endpoint = (url: URL) => url.pathname === "/admin/collections/posts";
		await page.route(endpoint, async (route) => {
			if (route.request().method() !== "GET") return route.fallback();
			markStarted();
			await pending;
			await route.continue();
		});
		await page.goBack();
		await started;
		await expect(page).toHaveURL(listURL);
		await expect(page.locator("main")).toHaveAttribute("inert", "");
		await expect(page.locator("main")).toHaveAttribute("aria-busy", "true");
		await expect(page.locator('input[name="title"]')).toHaveValue("Scroll post 25");
		await expect(results).toHaveCount(0);
		release();
		await expect(results).toHaveAttribute("aria-busy", "false");
		await expect.poll(() => position(page, width, "list")).toBeCloseTo(listY, 0);
		await page.unroute(endpoint);

		await page.goForward();
		await expect(page).toHaveURL(documentURL);
		await expect(page.locator('input[name="title"]')).toHaveValue("Scroll post 25");
		await expect.poll(() => position(page, width, "document")).toBeCloseTo(documentY, 0);

		if (width === 1440) {
			await page.getByRole("button", { name: "Live preview", exact: true }).click();
			await expect(page.getByRole("region", { name: "Live preview" })).toBeVisible();
			const fields = page.locator('[data-slot="document-fields-viewport"]');
			await fields.evaluate((element) => element.scrollTo(0, 260));
			const fieldsY = await fields.evaluate((element) => element.scrollTop);
			expect(fieldsY).toBeGreaterThan(100);
			const sidebar = page.getByRole("navigation", { name: "Admin navigation" });
			const sidebarY = await sidebar.evaluate((element) => {
				element.scrollTop = 80;
				return element.scrollTop;
			});
			await page.goBack();
			await expect(results).toHaveAttribute("aria-busy", "false");
			await expect.poll(() => position(page, width, "list")).toBeCloseTo(listY, 0);
			expect(await sidebar.evaluate((element) => element.scrollTop)).toBe(sidebarY);
			await page.goForward();
			await expect(page.locator('input[name="title"]')).toHaveValue("Scroll post 25");
			// Preview-open UI is route-local. Its primary fields position is restored
			// in the editor viewport when this history entry remounts without preview.
			await expect.poll(() => position(page, width, "document")).toBeCloseTo(fieldsY, 0);
		}
		expect(errors.pageErrors).toEqual([]);
		expect(errors.consoleErrors).toEqual([]);
	});
}

function position(page: Page, width: number, view: "list" | "document") {
	if (width < 768) return page.evaluate(() => window.scrollY);
	return page
		.locator(view === "list" ? "main" : '[data-slot="document-viewport"]')
		.evaluate((element) => element.scrollTop);
}

function scroll(page: Page, width: number, view: "list" | "document", y: number) {
	if (width < 768) return page.evaluate((top) => window.scrollTo(0, top), y);
	return page
		.locator(view === "list" ? "main" : '[data-slot="document-viewport"]')
		.evaluate((element, top) => element.scrollTo(0, top), y);
}
