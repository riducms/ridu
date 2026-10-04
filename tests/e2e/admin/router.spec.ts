import { expect, test, type Page } from "./fixture";
import { loginAsEditor, observePageErrors } from "./helpers";
import { block, bodyCards, richDocument } from "./rich-text-block-fixture";

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

for (const { width, preview } of [
	{ width: 1440, preview: false },
	{ width: 390, preview: false },
	{ width: 1440, preview: true },
]) {
	test(`saving and publishing a block article preserves ${preview ? "split preview" : `${width}px`} scroll`, async ({
		page,
	}) => {
		test.setTimeout(90_000);
		await loginAsEditor(page);
		await page.setViewportSize({ width, height: 760 });
		const errors = observePageErrors(page);
		const created = await page.request.post("/api/collections/block-articles?draft=true", {
			data: {
				title: "Scroll-safe block article",
				body: richDocument(
					Array.from({ length: 8 }, (_, index) => [
						block("callout", { title: `Callout ${index}` }),
						block("cta", { label: `CTA ${index}` }),
					]).flat()
				),
			},
		});
		expect(created.ok(), await created.text()).toBe(true);
		const article = (await created.json()).doc;
		const endpoint = `/api/collections/block-articles/${article.id}`;
		await page.goto(`/admin/collections/block-articles/${article.id}`);
		await expect(bodyCards(page)).toHaveCount(16);
		if (preview) {
			await page.getByRole("button", { name: "Live preview", exact: true }).click();
			await expect(page.getByRole("region", { name: "Live preview" })).toBeVisible();
		}

		await bodyCards(page)
			.first()
			.getByRole("textbox", { name: "Callout title", exact: true })
			.fill("Edited before saving");
		await scroll(page, width, "document", 720, preview);
		const beforeSave = await position(page, width, "document", preview);
		expect(beforeSave).toBeGreaterThan(300);
		const saveDraft = page.getByRole("button", { name: "Save draft", exact: true });
		const saved = page.waitForResponse(
			(response) =>
				response.request().method() === "PATCH" &&
				new URL(response.url()).pathname === endpoint &&
				new URL(response.url()).searchParams.get("draft") === "true"
		);
		const latestSave = await scrollWhileMutationPending(
			page,
			width,
			preview,
			endpoint,
			"PATCH",
			() => saveDraft.click(),
			beforeSave
		);
		expect((await saved).ok()).toBe(true);
		await expect(saveDraft).toHaveCount(0);
		await settleScroll(page);
		await expect.poll(() => position(page, width, "document", preview)).toBeCloseTo(latestSave, 0);

		const publish = page.getByRole("button", { name: "Publish changes", exact: true });
		await expect(publish).toBeEnabled();
		await scroll(page, width, "document", 720, preview);
		const beforePublish = await position(page, width, "document", preview);
		expect(beforePublish).toBeGreaterThan(300);
		const published = page.waitForResponse(
			(response) =>
				response.request().method() === "POST" &&
				new URL(response.url()).pathname === `${endpoint}/publish`
		);
		const latestPublish = await scrollWhileMutationPending(
			page,
			width,
			preview,
			`${endpoint}/publish`,
			"POST",
			() => publish.click(),
			beforePublish
		);
		expect((await published).ok()).toBe(true);
		await expect(page.locator(".ridu-document-metadata-value").first()).toHaveText("Published");
		await expect(publish).toBeDisabled();
		await settleScroll(page);
		await expect
			.poll(() => position(page, width, "document", preview))
			.toBeCloseTo(latestPublish, 0);

		await bodyCards(page)
			.nth(1)
			.getByRole("textbox", { name: /^CTA label/ })
			.fill("Edited before publishing");
		await scroll(page, width, "document", 720, preview);
		const beforeDirtyPublish = await position(page, width, "document", preview);
		expect(beforeDirtyPublish).toBeGreaterThan(300);
		await expect(publish).toBeEnabled();
		const publishedDirty = page.waitForResponse(
			(response) =>
				response.request().method() === "POST" &&
				new URL(response.url()).pathname === `${endpoint}/publish`
		);
		const latestDirtyPublish = await scrollWhileMutationPending(
			page,
			width,
			preview,
			`${endpoint}/publish`,
			"POST",
			() => publish.click(),
			beforeDirtyPublish
		);
		expect((await publishedDirty).ok()).toBe(true);
		await expect(publish).toHaveAttribute("aria-busy", "false");
		await expect(publish).toBeDisabled();
		await settleScroll(page);
		await expect
			.poll(() => position(page, width, "document", preview))
			.toBeCloseTo(latestDirtyPublish, 0);
		await expect(
			bodyCards(page)
				.nth(1)
				.getByRole("textbox", { name: /^CTA label/ })
		).toHaveValue("Edited before publishing");
		expect(errors.pageErrors).toEqual([]);
		expect(errors.consoleErrors).toEqual([]);
	});
}

test("a short block article keeps its near-bottom scroll after save and publish", async ({
	page,
}) => {
	await loginAsEditor(page);
	await page.setViewportSize({ width: 1440, height: 760 });
	const errors = observePageErrors(page);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: {
			title: "Short article scroll retention",
			body: richDocument([
				block("callout", { title: "Callout before save" }),
				block("cta", { label: "Read the article" }),
			]),
		},
	});
	expect(created.ok(), await created.text()).toBe(true);
	const article = (await created.json()).doc;
	const endpoint = `/api/collections/block-articles/${article.id}`;
	await page.goto(`/admin/collections/block-articles/${article.id}`);
	await expect(bodyCards(page)).toHaveCount(2);
	await bodyCards(page)
		.first()
		.getByRole("textbox", { name: "Callout title", exact: true })
		.fill("Edited near the bottom");

	const initial = await documentScrollGeometry(page);
	expect(initial.max).toBeGreaterThan(380);
	await scroll(page, 1440, "document", initial.max - 80);
	const beforeSave = await documentScrollGeometry(page);
	expect(beforeSave.max - beforeSave.top).toBeLessThanOrEqual(85);
	expect(beforeSave.top).toBeGreaterThan(300);
	const saveDraft = page.getByRole("button", { name: "Save draft", exact: true });
	const saved = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === endpoint &&
			new URL(response.url()).searchParams.get("draft") === "true"
	);
	await saveDraft.click();
	expect((await saved).ok()).toBe(true);
	await expect(saveDraft).toHaveCount(0);
	await settleScroll(page);
	const afterSave = await documentScrollGeometry(page);
	expect(afterSave.max).toBeGreaterThanOrEqual(beforeSave.top);
	expect(afterSave.top).toBeCloseTo(beforeSave.top, 0);

	const publish = page.getByRole("button", { name: "Publish changes", exact: true });
	await expect(publish).toBeEnabled();
	const beforePublish = await documentScrollGeometry(page);
	const published = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `${endpoint}/publish`
	);
	await publish.click();
	expect((await published).ok()).toBe(true);
	await expect(page.locator(".ridu-document-metadata-value").first()).toHaveText("Published");
	await expect(publish).toBeDisabled();
	await settleScroll(page);
	const afterPublish = await documentScrollGeometry(page);
	expect(afterPublish.max).toBeGreaterThanOrEqual(beforePublish.top);
	expect(afterPublish.top).toBeCloseTo(beforePublish.top, 0);
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

function position(page: Page, width: number, view: "list" | "document", preview = false) {
	if (width < 768) return page.evaluate(() => window.scrollY);
	return page.locator(primaryScroller(view, preview)).evaluate((element) => element.scrollTop);
}

function scroll(page: Page, width: number, view: "list" | "document", y: number, preview = false) {
	if (width < 768) return page.evaluate((top) => window.scrollTo(0, top), y);
	return page
		.locator(primaryScroller(view, preview))
		.evaluate((element, top) => element.scrollTo(0, top), y);
}

function primaryScroller(view: "list" | "document", preview: boolean) {
	if (view === "list") return "main";
	return preview ? '[data-slot="document-fields-viewport"]' : '[data-slot="document-viewport"]';
}

function documentScrollGeometry(page: Page) {
	return page.locator('[data-slot="document-viewport"]').evaluate((element) => ({
		top: element.scrollTop,
		max: element.scrollHeight - element.clientHeight,
	}));
}

function primaryScrollSize(page: Page, width: number, preview: boolean) {
	if (width < 768)
		return page.evaluate(() => {
			const element = document.scrollingElement;
			if (!element) throw new Error("The document has no primary scroller.");
			return { scrollHeight: element.scrollHeight, clientHeight: element.clientHeight };
		});
	return page.locator(primaryScroller("document", preview)).evaluate((element) => ({
		scrollHeight: element.scrollHeight,
		clientHeight: element.clientHeight,
	}));
}

async function scrollWhileMutationPending(
	page: Page,
	width: number,
	preview: boolean,
	pathname: string,
	method: "PATCH" | "POST",
	trigger: () => Promise<void>,
	before: number
) {
	let release!: () => void;
	let markStarted!: () => void;
	let markContinued!: () => void;
	let intercepted = false;
	const pending = new Promise<void>((resolve) => (release = resolve));
	const started = new Promise<void>((resolve) => (markStarted = resolve));
	const continued = new Promise<void>((resolve) => (markContinued = resolve));
	const endpoint = (url: URL) => url.pathname === pathname;
	const beforeSize = await primaryScrollSize(page, width, preview);
	await page.route(endpoint, async (route) => {
		if (route.request().method() !== method) return route.fallback();
		intercepted = true;
		markStarted();
		try {
			await pending;
			await route.continue();
		} finally {
			markContinued();
		}
	});
	try {
		await trigger();
		await started;
		await settleScroll(page);
		expect(await primaryScrollSize(page, width, preview)).toEqual(beforeSize);
		await scroll(page, width, "document", 1050, preview);
		const latest = await position(page, width, "document", preview);
		expect(latest).toBeGreaterThan(before + 100);
		return latest;
	} finally {
		release();
		if (intercepted) await continued;
		await page.unroute(endpoint);
	}
}

function settleScroll(page: Page) {
	return page.evaluate(
		() =>
			new Promise<void>((resolve) =>
				requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
			)
	);
}
