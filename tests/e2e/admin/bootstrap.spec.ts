import { expect, test } from "@playwright/test";

test("empty collection guides first-user creation and then closes setup", async ({
	page,
	context,
}) => {
	await page.goto("/admin/login");
	await expect(page).toHaveURL(/\/admin\/create-first-user$/);
	await expect(page.getByRole("heading", { name: "Welcome", exact: true })).toBeVisible();

	await page.getByLabel("Name", { exact: true }).fill("Ada Admin");
	await page.getByLabel("Email", { exact: true }).fill("ada@example.test");
	await expect(page.getByLabel("Role", { exact: true })).toHaveValue("administrator");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("correct-horse");
	await page.getByRole("textbox", { name: "Confirm password", exact: true }).fill("wrong-horse");
	await page.getByRole("button", { name: "Create account" }).click();
	await expect(page.getByRole("alert")).toContainText("The passwords do not match.");
	await expect(page.getByRole("textbox", { name: "Password", exact: true })).toBeFocused();

	await page.getByRole("textbox", { name: "Confirm password", exact: true }).fill("correct-horse");
	await page.getByRole("button", { name: "Create account" }).click();
	await expect(page).toHaveURL(/\/admin\/?$/);
	await expect(page.getByRole("button", { name: "Open account menu for Ada Admin" })).toBeVisible();

	await context.clearCookies();
	await page.goto("/admin/create-first-user");
	await expect(page).toHaveURL(/\/admin\/login$/);
	await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
});
