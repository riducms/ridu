import { expect, test } from "./fixture";
import { loginAsEditor } from "./helpers";

test("sidebar collapse persists, groups work by keyboard, and active links follow routes", async ({
	page,
}) => {
	await page.setViewportSize({ width: 1280, height: 800 });
	const navigation = await loginAsEditor(page);
	await expect(page.locator("#ridu-admin-navigation")).toHaveCSS("width", "275px");
	const group = navigation.locator("summary").filter({ hasText: "Content" });
	await expect(group).toHaveCSS("font-size", "13px");
	await expect(group).toHaveCSS("line-height", "20px");
	await group.press("Enter");
	await expect(navigation.getByRole("link", { name: "Posts", exact: true })).toBeHidden();
	await group.press("Enter");
	await navigation.getByRole("link", { name: "Posts", exact: true }).click();
	await expect(navigation.getByRole("link", { name: "Posts", exact: true })).toHaveAttribute(
		"aria-current",
		"page"
	);
	await page.getByRole("button", { name: "Close navigation", exact: true }).click();
	const open = page.getByRole("button", { name: "Open navigation", exact: true });
	await expect(open).toBeFocused();
	await expect(navigation).toBeHidden();
	await expect
		.poll(async () => (await page.request.get("/api/preferences/navigation")).json())
		.toMatchObject({ value: { open: false } });
	await page.reload();
	await expect(navigation).toBeHidden();
	await open.press("Enter");
	await expect(navigation).toBeVisible();
	await expect(page.getByRole("button", { name: /Open account menu/ })).toBeVisible();
});

test("mobile navigation fills the viewport, contains focus, and closes on escape and route selection", async ({
	page,
}) => {
	await loginAsEditor(page);
	await page.setViewportSize({ width: 390, height: 844 });
	const open = page.getByRole("button", { name: "Open navigation", exact: true });
	await open.click();
	const dialog = page.getByRole("dialog", { name: "Admin navigation" });
	await expect(dialog).toHaveCSS("width", "390px");
	await expect(dialog).toHaveCSS("height", "844px");
	const close = dialog.getByRole("button", { name: "Close navigation", exact: true });
	await expect(close).toBeFocused();
	await close.press("Shift+Tab");
	await expect
		.poll(() => dialog.evaluate((element) => element.contains(document.activeElement)))
		.toBe(true);
	await page.keyboard.press("Escape");
	await expect(dialog).toBeHidden();
	await expect(open).toBeFocused();
	await open.click();
	await dialog.getByRole("link", { name: "Posts", exact: true }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts\?locale=en$/);
	await expect(dialog).toBeHidden();
	await expect(open).toBeEnabled();
	await open.click();
	await dialog.getByRole("link", { name: "Plugin navigation link", exact: true }).click();
	await expect(page).toHaveURL(/\/admin\/plugin-contract$/);
	await expect(dialog).toBeHidden();
	await open.click();
	await page.setViewportSize({ width: 1280, height: 800 });
	await expect(dialog).toBeHidden();
	await expect(page.getByRole("navigation", { name: "Admin navigation" })).toBeVisible();
	await page.setViewportSize({ width: 390, height: 844 });
	await expect(dialog).toBeHidden();
	await expect(open).toBeVisible();
});

test("light RTL navigation mirrors the shell and respects reduced motion", async ({ page }) => {
	await page.setViewportSize({ width: 1280, height: 800 });
	await page.emulateMedia({ reducedMotion: "reduce" });
	await loginAsEditor(page);
	for (const [key, value] of [
		["theme", "light"],
		["admin-language", "ar"],
	]) {
		const response = await page.request.put(`/api/preferences/${key}`, { data: { value } });
		expect(response.ok()).toBe(true);
	}
	await page.reload();
	await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
	await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
	const sidebar = page.locator("#ridu-admin-navigation");
	await expect(sidebar).toBeVisible();
	await expect.poll(async () => (await sidebar.boundingBox())?.x).toBe(1005);
	await expect(page.locator(".ridu-shell")).toHaveCSS("background-color", "rgb(255, 255, 255)");
	await expect
		.poll(() =>
			page
				.locator(".ridu-shell__body")
				.evaluate((element) => parseFloat(getComputedStyle(element).transitionDuration))
		)
		.toBeLessThanOrEqual(0.00001);
	await expect(page.locator(".ridu-nav__link").first()).toHaveCSS("color", "rgb(47, 47, 47)");
	await page.locator(".ridu-shell__toggle button").click();
	await expect(sidebar).toBeHidden();
	await page.setViewportSize({ width: 390, height: 844 });
	await page.locator(".ridu-shell__header .ridu-nav-toggle").click();
	await expect(page.getByRole("dialog")).toHaveAttribute("dir", "rtl");
	await expect(page.getByRole("dialog")).toHaveCSS("animation-name", "none");
});
