import { expect, type Locator, type Page } from "@playwright/test";

export const uploadFixturePng = Buffer.from(
	"iVBORw0KGgoAAAANSUhEUgAAABQAAAAUCAYAAACNiR0NAAAACXBIWXMAAAsTAAALEwEAmpwYAAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAOdEVYdFNvZnR3YXJlAEZpZ21hnrGWYwAAAQtJREFUeAGllIERgjAMRYMTOELdwBFwA90AJ4ENHAGcQDZwBEeADXCD+CvlLCGFgv/uH4W2r21oQrQgZt7DpbOhrXKgHO74J9vOaa0w6Qg3HJbty9YADVx4k1P4rCzyUMPgHS+VYGVspoDL0Vi8VMHO8AkqCQ0BB+V25xHgzgfuZsYX8Gsu+EmStHi8/W8a8AK3rm3g0sXMUIR2yqo1fEDzKsBR0OCRAa3wOMF3WqG5GH5jBGdo2h3X9C9QgC/uJ0j5oeklro2hDRrmaTtsli63y6ybf6VGuw/c/EllYb0CNcEEcOCHANsJNn9TnubwEz5SRDy0AiAXSWmtuC9hssAWvJDjS9AhvmUM6APCbObLQ83cDQAAAABJRU5ErkJggg==",
	"base64"
);

export async function chooseRiduSelect(page: Page, trigger: Locator, option: string | RegExp) {
	await trigger.click();
	await page.getByRole("option", { name: option, exact: typeof option === "string" }).click();
}

export async function expectRiduSelectValue(trigger: Locator, value: string | RegExp) {
	await expect(trigger).toContainText(value);
}

export async function chooseContentLocale(page: Page, locale: string, code: string) {
	await page.getByLabel("Content locale", { exact: true }).click();
	await page.getByRole("menuitem", { name: `${locale} (${code})`, exact: true }).click();
}

export async function setRiduSliderValue(slider: Locator, value: number) {
	await slider.focus();
	await slider.press("Home");
	const minimum = Number((await slider.getAttribute("aria-valuemin")) ?? 0);
	for (let current = minimum; current < value; current += 1) await slider.press("ArrowRight");
}

export async function expectRiduSliderValue(slider: Locator, value: number) {
	await expect(slider).toHaveAttribute("aria-valuenow", String(value));
}

export async function expectRiduDateTimeControl(control: Locator) {
	await expect(control).toHaveAttribute("role", "group");
	await expect(control.locator('input[type="datetime-local"]')).toHaveCount(0);
	await expect(control.locator('[data-segment="minute"]')).toHaveCount(1);
}

export function documentSaveButton(page: Page) {
	return page.getByRole("button", { name: /^(Save|Save draft|Publish|Publish changes)$/ }).first();
}

export async function selectRichText(page: Page, editor: Locator) {
	const textBlock = editor.locator("p").filter({ hasText: /\S/ }).first();
	const bounds = await textBlock.boundingBox();
	if (bounds === null) throw new Error("rich-text block has no layout box");
	await page.mouse.move(bounds.x + bounds.width - 2, bounds.y + bounds.height / 2);
	await page.mouse.down();
	await page.mouse.move(bounds.x + 2, bounds.y + bounds.height / 2, { steps: 10 });
	await page.mouse.up();
}

export async function selectRiduCalendarDay(page: Page, control: Locator) {
	await control.getByRole("button", { name: "Open calendar" }).click();
	const calendar = page.getByRole("application", { name: /^Select date/ });
	await calendar.getByRole("gridcell", { disabled: false }).nth(14).getByRole("button").click();
}

export function observePageErrors(page: Page) {
	const consoleErrors: string[] = [];
	const pageErrors: string[] = [];
	page.on("console", (message) => {
		if (message.type() === "error") consoleErrors.push(message.text());
	});
	page.on("pageerror", (error) => pageErrors.push(error.message));
	return { consoleErrors, pageErrors };
}

export async function loginAsEditor(page: Page) {
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	const loginResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === "/api/auth/users/login"
	);
	await page.getByRole("button", { name: "Sign in" }).click();
	expect((await loginResponse).ok()).toBe(true);
	const collectionsNavigation = page.getByRole("navigation", { name: "Admin navigation" });
	await collectionsNavigation.waitFor({ state: "visible" });
	await expect
		.poll(async () => {
			const response = await page.request.get("/api/auth/me");
			return response.status();
		})
		.toBe(200);
	return collectionsNavigation;
}
