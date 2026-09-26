import { expect, test } from "./fixture";
import { loginAsEditor, observePageErrors } from "./helpers";

for (const direction of ["ltr", "rtl"] as const) {
	test(`rich-text menus, link drawer and SEO fit desktop and narrow screens in ${direction}`, async ({
		page,
	}) => {
		await loginAsEditor(page);
		const errors = observePageErrors(page);
		const theme = direction === "ltr" ? "light" : "dark";
		await page.evaluate(
			async ({ direction, theme }) => {
				for (const [key, value] of [
					["theme", theme],
					["admin-language", direction === "rtl" ? "ar" : "en"],
				]) {
					const response = await fetch(`/api/preferences/${key}`, {
						method: "PUT",
						headers: { "content-type": "application/json" },
						body: JSON.stringify({ value }),
					});
					if (!response.ok) throw new Error(`Could not set ${key}: ${response.status}`);
					localStorage.setItem(`ridu:${key}`, value);
				}
			},
			{ direction, theme }
		);
		await page.emulateMedia({ reducedMotion: "reduce" });
		await page.goto("/admin/collections/posts/create");
		await expect(page.locator("html")).toHaveAttribute("dir", direction);
		await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
		const editor = page.getByRole("textbox", { name: "Content" });

		for (const width of [1280, 390]) {
			await page.setViewportSize({ width, height: 844 });
			await editor.fill("Selection controls");
			await editor.press("ControlOrMeta+A");
			const toolbar = page.locator(".ridu-richtext-floating-toolbar");
			await expect(toolbar).toBeVisible();
			const toolbarBox = await toolbar.boundingBox();
			expect(toolbarBox!.x).toBeGreaterThanOrEqual(0);
			expect(toolbarBox!.x + toolbarBox!.width).toBeLessThanOrEqual(width);
			await page.keyboard.press("ControlOrMeta+K");
			const drawer = page.locator(".ridu-richtext-link-drawer");
			await expect(drawer).toBeVisible();
			const drawerBox = await drawer.boundingBox();
			expect(drawerBox!.x).toBeGreaterThanOrEqual(0);
			expect(drawerBox!.x + drawerBox!.width).toBeLessThanOrEqual(width);
			const drawerOverflow = await drawer.evaluate(
				(element) => element.scrollWidth > element.clientWidth
			);
			expect(drawerOverflow).toBe(false);
			await page.keyboard.press("Escape");
			await expect(drawer).toBeHidden();

			await editor.fill("");
			await page.keyboard.type("/");
			const menu = page.locator(".ridu-richtext-menu-popover");
			await expect(menu).toBeVisible();
			const menuBox = await menu.boundingBox();
			expect(menuBox!.width).toBeCloseTo(200, 0);
			expect(menuBox!.x).toBeGreaterThanOrEqual(0);
			expect(menuBox!.x + menuBox!.width).toBeLessThanOrEqual(width);
			await page.keyboard.press("Escape");
		}

		await page.goto("/admin/collections/pages/create");
		await page.getByRole("tab", { name: "SEO", exact: true }).click();
		for (const width of [390, 1280]) {
			await page.setViewportSize({ width, height: 844 });
			const preview = page.locator(".ridu-seo-preview-card");
			await expect(preview).toBeVisible();
			const bounds = await preview.boundingBox();
			expect(bounds!.width).toBeLessThanOrEqual(600);
			expect(bounds!.x).toBeGreaterThanOrEqual(0);
			expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
			expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(
				false
			);
		}
		expect(errors.pageErrors).toEqual([]);
		expect(errors.consoleErrors).toEqual([]);
	});
}
