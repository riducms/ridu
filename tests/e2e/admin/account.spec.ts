import { expect, test } from "./fixture";

import { chooseRiduSelect, documentSaveButton, expectRiduSelectValue } from "./helpers";

test("account profile, theme preferences, reset, and force unlock are available", async ({
	page,
}) => {
	await page.goto("/admin/login");
	let releaseLoginTheme!: () => void;
	let markLoginThemeStarted!: () => void;
	let markLoginThemeFinished!: () => void;
	const loginThemeRelease = new Promise<void>((resolve) => (releaseLoginTheme = resolve));
	const loginThemeStarted = new Promise<void>((resolve) => (markLoginThemeStarted = resolve));
	const loginThemeFinished = new Promise<void>((resolve) => (markLoginThemeFinished = resolve));
	await page.route("**/api/preferences/theme", async (route) => {
		if (route.request().method() !== "GET") {
			await route.continue();
			return;
		}
		markLoginThemeStarted();
		await loginThemeRelease;
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({ value: "system" }),
		});
		markLoginThemeFinished();
	});
	await page.getByLabel("Email address").fill("admin@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-admin");
	await page.getByRole("button", { name: "Sign in" }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor({ state: "visible" });

	await page.getByRole("button", { name: /Open account menu for Ridu Administrator/ }).click();
	await page.getByRole("link", { name: "Profile & preferences" }).click();
	await chooseRiduSelect(page, page.getByLabel("Theme"), "Dark");
	await expectRiduSelectValue(page.getByLabel("Theme"), "Dark");
	await expect(page).toHaveURL(/\/admin\/account$/);
	await loginThemeStarted;
	await expectRiduSelectValue(page.getByLabel("Theme"), "Dark");
	releaseLoginTheme();
	await loginThemeFinished;
	await expectRiduSelectValue(page.getByLabel("Theme"), "Dark");
	await page.unroute("**/api/preferences/theme");
	await chooseRiduSelect(page, page.getByLabel("Theme"), "System");
	await expectRiduSelectValue(page.getByLabel("Theme"), "System");
	let concurrentThemeWriteCount = 0;
	let releaseFailedThemeWrite!: () => void;
	let markFailedThemeWriteStarted!: () => void;
	const failedThemeWriteRelease = new Promise<void>(
		(resolve) => (releaseFailedThemeWrite = resolve)
	);
	const failedThemeWriteStarted = new Promise<void>(
		(resolve) => (markFailedThemeWriteStarted = resolve)
	);
	await page.route("**/api/preferences/theme", async (route) => {
		if (route.request().method() !== "PUT") {
			await route.continue();
			return;
		}
		const writeNumber = ++concurrentThemeWriteCount;
		if (writeNumber === 1) {
			markFailedThemeWriteStarted();
			await failedThemeWriteRelease;
			await route.fulfill({
				status: 500,
				contentType: "application/json",
				body: JSON.stringify({ error: { code: "internal", message: "forced theme failure" } }),
			});
			return;
		}
		await route.continue();
	});
	await chooseRiduSelect(page, page.getByLabel("Theme"), "Dark");
	await failedThemeWriteStarted;
	await chooseRiduSelect(page, page.getByLabel("Theme"), "Light");
	const successfulConcurrentTheme = page.waitForResponse(
		(response) =>
			response.request().method() === "PUT" &&
			response.url().includes("/api/preferences/theme") &&
			response.ok()
	);
	releaseFailedThemeWrite();
	await successfulConcurrentTheme;
	await expectRiduSelectValue(page.getByLabel("Theme"), "Light");
	const concurrentStoredTheme = await page.evaluate(async () => {
		const response = await fetch("/api/preferences/theme");
		if (!response.ok) throw new Error(`read concurrent theme: ${response.status}`);
		return (await response.json()) as { value: unknown };
	});
	expect(concurrentStoredTheme.value).toBe("light");
	await page.unroute("**/api/preferences/theme");
	await expect(page.getByLabel("Name", { exact: true })).toHaveValue("Ridu Administrator");
	const accountTabs = page.getByRole("tablist", { name: "Profile and Security sections" });
	const securityTab = accountTabs.getByRole("tab", { name: "Security", exact: true });
	await securityTab.click();
	const securityPanelID = await securityTab.getAttribute("aria-controls");
	expect(securityPanelID).toBeTruthy();
	await expect(page.locator(`#${securityPanelID}`)).toHaveAttribute("role", "tabpanel");
	await expect(page.locator(`#${securityPanelID}`)).toBeVisible();
	await expect(page.getByLabel("Private Notes", { exact: true })).toBeVisible();

	await chooseRiduSelect(page, page.getByLabel("Theme"), "Light");
	await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
	await page.reload();
	await expectRiduSelectValue(page.getByLabel("Theme"), "Light");
	await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
	let releaseThemeWrite!: () => void;
	let markThemeWriteStarted!: () => void;
	const themeWriteRelease = new Promise<void>((resolve) => (releaseThemeWrite = resolve));
	const themeWriteStarted = new Promise<void>((resolve) => (markThemeWriteStarted = resolve));
	await page.route("**/api/preferences/theme", async (route) => {
		if (route.request().method() !== "PUT") {
			await route.continue();
			return;
		}
		markThemeWriteStarted();
		await themeWriteRelease;
		const response = await route.fetch();
		await route.fulfill({ response });
	});
	let preferenceResetCount = 0;
	await page.route("**/api/preferences", async (route) => {
		if (route.request().method() !== "DELETE") {
			await route.fallback();
			return;
		}
		preferenceResetCount += 1;
		await route.continue();
	});
	await chooseRiduSelect(page, page.getByLabel("Theme"), "Dark");
	await themeWriteStarted;
	await page.getByRole("button", { name: "Reset all preferences" }).click();
	await page.waitForTimeout(100);
	expect(preferenceResetCount).toBe(0);
	releaseThemeWrite();
	await expect(page.getByText("Admin preferences reset.")).toBeVisible();
	expect(preferenceResetCount).toBe(1);
	await expectRiduSelectValue(page.getByLabel("Theme"), "System");
	await page.unroute("**/api/preferences");
	await page.unroute("**/api/preferences/theme");
	await page.reload();
	await expectRiduSelectValue(page.getByLabel("Theme"), "System");

	await page
		.getByRole("navigation", { name: "Admin navigation" })
		.getByRole("link", { name: "Users" })
		.click();
	await page.getByRole("link", { name: /Ridu Editor/ }).click();
	await page.getByRole("button", { name: "More actions" }).click();
	await expect(page.getByRole("button", { name: "Force unlock" })).toBeVisible();
	await page.getByRole("button", { name: "Force unlock" }).click();
	await expect(page.getByText("Account unlocked.")).toBeVisible();

	await page.getByRole("button", { name: /Open account menu for Ridu Administrator/ }).click();
	await page.getByRole("link", { name: "Profile & preferences" }).click();
	await chooseRiduSelect(page, page.getByLabel("Theme"), "Dark");
	await expect
		.poll(async () => {
			return page.evaluate(async () => {
				const response = await fetch("/api/preferences/theme");
				if (!response.ok) return undefined;
				return ((await response.json()) as { value: unknown }).value;
			});
		})
		.toBe("dark");
	let releaseOldSessionTheme!: () => void;
	let markOldSessionThemeStarted!: () => void;
	let markOldSessionThemeFinished!: () => void;
	let releaseNewSessionTheme!: () => void;
	let markNewSessionThemeStarted!: () => void;
	let markNewSessionThemeFinished!: () => void;
	let crossSessionThemeWriteCount = 0;
	const oldSessionThemeRelease = new Promise<void>((resolve) => (releaseOldSessionTheme = resolve));
	const oldSessionThemeStarted = new Promise<void>(
		(resolve) => (markOldSessionThemeStarted = resolve)
	);
	const oldSessionThemeFinished = new Promise<void>(
		(resolve) => (markOldSessionThemeFinished = resolve)
	);
	const newSessionThemeRelease = new Promise<void>((resolve) => (releaseNewSessionTheme = resolve));
	const newSessionThemeStarted = new Promise<void>(
		(resolve) => (markNewSessionThemeStarted = resolve)
	);
	const newSessionThemeFinished = new Promise<void>(
		(resolve) => (markNewSessionThemeFinished = resolve)
	);
	await page.route("**/api/preferences/theme", async (route) => {
		if (route.request().method() !== "PUT") {
			await route.continue();
			return;
		}
		const writeNumber = ++crossSessionThemeWriteCount;
		if (writeNumber === 1) {
			markOldSessionThemeStarted();
			await oldSessionThemeRelease;
		} else {
			markNewSessionThemeStarted();
			await newSessionThemeRelease;
		}
		await route.continue();
		if (writeNumber === 1) markOldSessionThemeFinished();
		else markNewSessionThemeFinished();
	});
	let crossSessionResetCount = 0;
	await page.route("**/api/preferences", async (route) => {
		if (route.request().method() !== "DELETE") {
			await route.fallback();
			return;
		}
		crossSessionResetCount += 1;
		await route.continue();
	});
	await chooseRiduSelect(page, page.getByLabel("Theme"), "Light");
	await oldSessionThemeStarted;
	await page.getByRole("button", { name: /Open account menu for Ridu Administrator/ }).click();
	await page.getByRole("button", { name: "Sign out" }).click();
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in" }).click();
	await page.getByRole("button", { name: /Open account menu for Ridu Editor/ }).click();
	await page.getByRole("link", { name: "Profile & preferences" }).click();
	await expectRiduSelectValue(page.getByLabel("Theme"), "System");
	await chooseRiduSelect(page, page.getByLabel("Theme"), "Dark");
	await newSessionThemeStarted;
	await expectRiduSelectValue(page.getByLabel("Theme"), "Dark");
	await page.waitForTimeout(100);
	expect(crossSessionResetCount).toBe(0);
	const oldSessionThemeResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "PUT" && response.url().includes("/api/preferences/theme")
	);
	releaseOldSessionTheme();
	await oldSessionThemeFinished;
	await oldSessionThemeResponse;
	await expectRiduSelectValue(page.getByLabel("Theme"), "Dark");
	expect(crossSessionResetCount).toBe(0);
	const newSessionThemeResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "PUT" && response.url().includes("/api/preferences/theme")
	);
	releaseNewSessionTheme();
	await newSessionThemeFinished;
	const savedThemeResponse = await newSessionThemeResponse;
	expect(savedThemeResponse.ok()).toBe(true);
	await expectRiduSelectValue(page.getByLabel("Theme"), "Dark");
	const storedTheme = await page.evaluate(async () => {
		const response = await fetch("/api/preferences/theme");
		if (!response.ok) throw new Error(`read editor theme: ${response.status}`);
		return (await response.json()) as { value: unknown };
	});
	expect(storedTheme.value).toBe("dark");
	await page.getByRole("button", { name: "Reset all preferences" }).click();
	await expect(page.getByText("Admin preferences reset.")).toBeVisible();
	expect(crossSessionResetCount).toBe(1);
	await page.unroute("**/api/preferences");
	await page.unroute("**/api/preferences/theme");
});

test("plugins can wrap auth and account views and compose the authenticated shell", async ({
	page,
}) => {
	await page.goto("/admin/login");
	await expect(page.getByText("Custom login wrapper", { exact: true })).toBeVisible();
	await expect(page.getByText("Ridu extension", { exact: true })).toBeVisible();
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in" }).click();
	await expect(page.getByText("Ridu editorial kitchen sink plugin shell")).toBeVisible();
	await expect(page.getByText("Plugin header", { exact: true })).toBeVisible();
	await expect(page.getByRole("button", { name: "Plugin action" })).toBeVisible();
	await expect(
		page.getByRole("link", { name: "Ridu editorial kitchen sink", exact: true })
	).toBeVisible();
	await page.goto("/admin/collections/posts");
	await expect(page.getByText("Plugin collectionList view", { exact: true })).toBeVisible();
	await page.goto("/admin/collections/posts/create");
	await expect(page.getByText("Plugin collectionCreate view", { exact: true })).toBeVisible();
	await page.goto("/admin/collections/posts");
	await page.locator("tbody a").first().click();
	await expect(page.getByText("Plugin collectionEdit view", { exact: true })).toBeVisible();
	await page.goto("/admin/globals/site-settings");
	await expect(page.getByText("Plugin global view", { exact: true })).toBeVisible();
	await page.goto("/admin/does-not-exist");
	await expect(page.getByText("Plugin notFound view", { exact: true })).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "This admin page does not exist." })
	).toBeVisible();

	await page.getByRole("button", { name: /Open account menu for Ridu Editor/ }).click();
	await expect(page.getByRole("button", { name: "Plugin settings" })).toBeVisible();
	await page.getByRole("link", { name: "Profile & preferences" }).click();
	await expect(page.getByText("Custom profile wrapper for Ridu Editor")).toBeVisible();
	await expect(page.getByLabel("Name", { exact: true })).toHaveValue("Ridu Editor");

	await page.getByRole("button", { name: /Open account menu for Ridu Editor/ }).click();
	await page.getByRole("button", { name: "Plugin sign out" }).click();
	await expect(page).toHaveURL(/\/admin\/login$/);
	await expect(page.getByText("Custom login wrapper", { exact: true })).toBeVisible();
});

test("auth user creation validates and stores a usable password", async ({ page }) => {
	const consoleErrors: string[] = [];
	page.on("console", (message) => {
		if (message.type() === "error") consoleErrors.push(message.text());
	});
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("admin@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-admin");
	await page.getByRole("button", { name: "Sign in" }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor({ state: "visible" });

	await page.goto("/admin/collections/users/create");
	await expect(page.getByRole("group", { name: "Credentials" })).toBeVisible();
	await page.getByLabel("Name", { exact: true }).fill("Browser Credential User");
	await page.getByLabel("Email", { exact: true }).fill("credential-user@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("credential-secret");
	await page.getByRole("textbox", { name: "Confirm password", exact: true }).fill("does-not-match");
	await documentSaveButton(page).click();
	await expect(page.getByRole("alert")).toHaveText("Passwords do not match.");
	await expect(page.getByRole("textbox", { name: "Password", exact: true })).toBeFocused();

	await page
		.getByRole("textbox", { name: "Confirm password", exact: true })
		.fill("credential-secret");
	await documentSaveButton(page).click();
	await expect(page).toHaveURL(/\/admin\/collections\/users\/users_/);
	await expect(page.getByText("User successfully created.")).toBeVisible();

	await page.evaluate(async () => {
		await fetch("/api/auth/logout", { method: "POST" });
	});
	await page.goto("/admin/login");
	await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
	consoleErrors.length = 0;
	await page.getByLabel("Email address").fill("credential-user@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("credential-secret");
	await page.getByRole("button", { name: "Sign in" }).click();
	await expect(
		page.getByRole("button", { name: /Open account menu for Browser Credential User/ })
	).toBeVisible();
	expect(consoleErrors).toEqual([]);
});
