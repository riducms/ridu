import { expect, test } from "./fixture";
import { loginAsEditor, observePageErrors, uploadFixturePng } from "./helpers";

for (const direction of ["ltr", "rtl"] as const) {
	test(`bulk workspaces fit desktop and narrow screens in ${direction}`, async ({ page }) => {
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
		await page.goto("/admin/collections/posts");
		await expect(page.locator("html")).toHaveAttribute("dir", direction);
		await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
		await page.getByRole("checkbox", { name: /Welcome to Ridu/ }).check();
		await page
			.locator(".ridu-list-selection")
			.getByRole("button", { name: direction === "rtl" ? "تحرير" : "Edit", exact: true })
			.click();

		const editor = page.locator(".ridu-bulk-edit-drawer");
		await expect(editor).toBeVisible();
		await expect(editor).toHaveCSS("animation-name", "none");
		for (const width of [1280, 390]) {
			await page.setViewportSize({ width, height: 844 });
			const bounds = await editor.boundingBox();
			expect(bounds).not.toBeNull();
			expect(bounds!.x).toBeGreaterThanOrEqual(0);
			expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
			await expect(editor.locator(".ridu-bulk-edit-sidebar button")).toBeInViewport();
			if (width === 1280) {
				expect(bounds!.width).toBeLessThan(width);
				expect(direction === "rtl" ? bounds!.x : width - bounds!.x - bounds!.width).toBe(0);
			}
		}
		await page.keyboard.press("Escape");
		await expect(editor).toBeHidden();

		await page.goto("/admin/collections/media/upload");
		await page
			.locator('input[type="file"]')
			.setInputFiles([{ name: "layout.png", mimeType: "image/png", buffer: uploadFixturePng }]);
		const upload = page.locator(".ridu-bulk-upload");
		await expect(upload.locator(".ridu-upload-name input")).toHaveValue("layout.png");
		for (const width of [390, 1280]) {
			await page.setViewportSize({ width, height: 844 });
			const layout = await upload.evaluate((element) => ({
				left: element.getBoundingClientRect().left,
				right: element.getBoundingClientRect().right,
				overflow: document.documentElement.scrollWidth > innerWidth,
			}));
			expect(layout.left).toBeGreaterThanOrEqual(0);
			expect(layout.right).toBeLessThanOrEqual(width);
			expect(layout.overflow).toBe(false);
			await expect(
				upload.getByRole("button", { name: direction === "rtl" ? "حفظ" : "Save", exact: true })
			).toBeInViewport();
		}
		expect(errors.pageErrors).toEqual([]);
	});
}
