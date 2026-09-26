import { expect, test } from "@playwright/test";

test("empty collection guides first-user creation and then closes setup", async ({
	page,
	context,
}) => {
	await page.addInitScript(() => {
		Reflect.set(window, "__riduLoadingInsertions", [] as string[]);
		const selector = '[data-ridu-loading-surface],[data-slot="skeleton"]';
		new MutationObserver((records) => {
			const insertions = Reflect.get(window, "__riduLoadingInsertions") as string[];
			for (const record of records) {
				for (const node of record.addedNodes) {
					if (!(node instanceof Element)) continue;
					if (node.matches(selector) || node.querySelector(selector)) insertions.push(node.tagName);
				}
			}
		}).observe(document, { childList: true, subtree: true });
	});
	await page.goto("/admin/login");
	await expect(page).toHaveURL(/\/admin\/create-first-user$/);
	await expect(page.getByRole("heading", { name: "Welcome", exact: true })).toBeVisible();
	expect(await page.evaluate(() => Reflect.get(window, "__riduLoadingInsertions"))).toEqual([]);
	await expect
		.poll(() => page.evaluate(() => performance.getEntriesByName("ridu:admin-route-ready").length))
		.toBe(1);

	await page.getByRole("button", { name: "Create account" }).click();
	await expect(page.locator("#ridu-first-user-password-error")).toBeVisible();
	await expect(page.getByRole("textbox", { name: "Password", exact: true })).toBeFocused();

	await page.getByLabel("Name", { exact: true }).fill("Ada Admin");
	await page.getByLabel("Email", { exact: true }).fill("ada@example.test");
	await expect(page.getByLabel("Role", { exact: true })).toHaveValue("administrator");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("short");
	await page.getByRole("button", { name: "Create account" }).click();
	await expect(page.locator("#ridu-first-user-password-error")).toContainText("at least");
	await page.getByRole("textbox", { name: "Confirm password", exact: true }).fill("s");
	await expect(page.locator("#ridu-first-user-password-error")).toContainText("at least");

	await page.getByRole("textbox", { name: "Password", exact: true }).fill("correct-horse");
	await page.getByRole("textbox", { name: "Confirm password", exact: true }).fill("wrong-horse");
	await page.getByRole("button", { name: "Create account" }).click();
	await expect(page.getByRole("alert")).toContainText("The passwords do not match.");
	await expect(page.getByRole("textbox", { name: "Confirm password", exact: true })).toBeFocused();
	await expect(page.locator("#ridu-first-user-password-confirmation-error")).toContainText(
		"The passwords do not match."
	);

	await page.getByRole("textbox", { name: "Confirm password", exact: true }).fill("correct-horse");
	await page.getByRole("button", { name: "Create account" }).click();
	await expect(page).toHaveURL(/\/admin\/?$/);
	await expect(page.getByRole("button", { name: "Open account menu for Ada Admin" })).toBeVisible();

	await context.clearCookies();
	await page.goto("/admin/create-first-user");
	await expect(page).toHaveURL(/\/admin\/login$/);
	await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
	expect(await page.evaluate(() => Reflect.get(window, "__riduLoadingInsertions"))).toEqual([]);
});
