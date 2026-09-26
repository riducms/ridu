import { expect, test } from "./fixture";
import { loginAsEditor, observePageErrors } from "./helpers";

for (const feature of [
	"date",
	"preview",
	"relationship",
	"api",
	"versions",
	"bulk-upload",
] as const) {
	test(`failed ${feature} chunk offers recovery without an unhandled rejection`, async ({
		page,
	}) => {
		await loginAsEditor(page);
		const errors = observePageErrors(page);
		const chunk = {
			date: /\/date-value-control-[^/]+\.js$/,
			preview: /\/upload-control-[^/]+\.js$/,
			relationship: /\/reference-browser-[^/]+\.js$/,
			api: /\/document-api-view-[^/]+\.js$/,
			versions: /\/version-history-route-[^/]+\.js$/,
			"bulk-upload": /\/bulk-upload-route-[^/]+\.js$/,
		}[feature];
		const message = {
			date: "The date control could not be loaded.",
			preview: "The asset preview could not be loaded.",
			relationship: "The relationship browser could not be loaded.",
			api: "The API view could not be loaded.",
			versions: "Version history could not be loaded.",
			"bulk-upload": "The bulk upload workspace could not be loaded.",
		}[feature];
		await page.route(chunk, (route) => route.fulfill({ status: 503, body: "Unavailable" }));
		if (feature === "preview") {
			await page.goto("/admin/collections/media");
			await page.getByRole("link", { name: "Warm orange Ridu cover", exact: true }).click();
		} else if (feature === "versions") {
			await page.goto("/admin/collections/pages/pages_10/versions");
		} else if (feature === "bulk-upload") {
			await page.goto("/admin/collections/media/upload");
		} else {
			await page.goto(`/admin/collections/posts/posts_9${feature === "api" ? "/api" : ""}`);
			if (feature === "relationship") {
				await page
					.locator('[data-field-path="relatedPosts"]')
					.getByRole("combobox", { name: "Related posts", exact: true })
					.click();
				await page.getByRole("button", { name: /^Browse all posts/ }).click();
			}
		}
		const failure = page.getByRole("alert").filter({ hasText: message });
		await expect(failure).toBeVisible();
		await expect(failure.getByRole("button", { name: "Reload page" })).toBeVisible();
		expect(errors.pageErrors).toEqual([]);
		await page.unroute(chunk);
		await failure.getByRole("button", { name: "Reload page" }).click();
		await expect(failure).toHaveCount(0);
		if (feature === "preview") {
			await expect(page.getByRole("region", { name: "Asset preview" })).toBeVisible();
		} else if (feature === "relationship") {
			await page
				.locator('[data-field-path="relatedPosts"]')
				.getByRole("combobox", { name: "Related posts", exact: true })
				.click();
			await page.getByRole("button", { name: /^Browse all posts/ }).click();
			await expect(page.getByRole("dialog", { name: "Add related posts" })).toBeVisible();
		} else if (feature === "date") {
			await expect(page.getByRole("button", { name: "Open calendar" })).toBeVisible();
		} else if (feature === "versions") {
			await expect(page.getByRole("table", { name: "Version history" })).toBeVisible();
		} else if (feature === "bulk-upload") {
			await expect(page.getByRole("button", { name: "Select a file", exact: true })).toBeVisible();
		} else {
			await expect(page.getByRole("button", { name: "Copy URL", exact: true })).toBeVisible();
		}
		expect(errors.pageErrors).toEqual([]);
	});
}

test("failed bulk editor chunk can close and recover without losing the collection", async ({
	page,
}) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	const errors = observePageErrors(page);
	const chunk = /\/bulk-editor-[^/]+\.js$/;
	await page.route(chunk, (route) => route.fulfill({ status: 503, body: "Unavailable" }));
	await page.getByRole("checkbox", { name: "Select Welcome to Ridu" }).check();
	const edit = page
		.getByRole("region", { name: "Bulk actions" })
		.getByRole("button", { name: "Edit", exact: true });
	await edit.click();

	const failure = page.getByRole("dialog", {
		name: "The bulk edit workspace could not be loaded.",
	});
	await expect(failure).toBeVisible();
	await failure.getByRole("button", { name: "Close", exact: true }).click();
	await expect(failure).toBeHidden();
	await expect(edit).toBeFocused();
	await edit.click();
	await expect(failure).toBeVisible();
	await page.unroute(chunk);
	await failure.getByRole("button", { name: "Reload page", exact: true }).click();

	await page.getByRole("checkbox", { name: "Select Welcome to Ridu" }).check();
	await edit.click();
	await expect(page.getByRole("dialog", { name: "Editing 1 Post" })).toBeVisible();
	expect(errors.pageErrors).toEqual([]);
});

test("date-time picker keeps its time column at desktop and narrow widths", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts/posts_9");

	for (const viewport of [
		{ width: 1280, height: 800, minimumTimeWidth: 109 },
		{ width: 390, height: 844, minimumTimeWidth: 84 },
	]) {
		await page.setViewportSize({ width: viewport.width, height: viewport.height });
		const trigger = page.getByRole("button", { name: "Open calendar" }).first();
		await trigger.click();

		const popup = page.locator(".ridu-date-popup");
		const timeColumn = popup.locator(".ridu-date-picker__time");
		await expect(timeColumn).toBeVisible();
		await popup.evaluate(async (element) => {
			await Promise.all(element.getAnimations().map((animation) => animation.finished));
		});
		const layout = await popup.evaluate((element) => {
			const picker = element.querySelector<HTMLElement>(".ridu-date-picker");
			const time = element.querySelector<HTMLElement>(".ridu-date-picker__time");
			if (picker === null || time === null)
				throw new Error("Date-time picker layout is incomplete");
			const popupBox = element.getBoundingClientRect();
			const pickerBox = picker.getBoundingClientRect();
			const timeBox = time.getBoundingClientRect();
			const style = getComputedStyle(element);
			return {
				gap: style.columnGap,
				paddingInlineStart: style.paddingInlineStart,
				paddingInlineEnd: style.paddingInlineEnd,
				pickerLeft: pickerBox.left,
				pickerRight: pickerBox.right,
				popupLeft: popupBox.left,
				popupRight: popupBox.right,
				timeWidth: timeBox.width,
			};
		});

		expect(layout).toMatchObject({
			gap: "0px",
			paddingInlineStart: "0px",
			paddingInlineEnd: "0px",
		});
		expect(layout.timeWidth).toBeGreaterThanOrEqual(viewport.minimumTimeWidth);
		expect(layout.pickerLeft).toBeGreaterThanOrEqual(layout.popupLeft - 1);
		expect(layout.pickerRight).toBeLessThanOrEqual(layout.popupRight + 1);
		expect(layout.popupLeft).toBeGreaterThanOrEqual(0);
		expect(layout.popupRight).toBeLessThanOrEqual(viewport.width);

		await page.keyboard.press("Escape");
		await expect(trigger).toHaveAttribute("aria-expanded", "false");
	}
});
