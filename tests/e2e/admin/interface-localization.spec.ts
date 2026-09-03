import { expect, test, type Page } from "./fixture";

import { chooseRiduSelect, expectRiduSelectValue } from "./helpers";

test("interface language, timezone, plugin messages, persistence, and RTL work together", async ({
	page,
}) => {
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("admin@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-admin");
	await page.getByRole("button", { name: "Sign in" }).click();
	await page
		.getByRole("button", { name: /Open account menu for Ridu Administrator/ })
		.waitFor({ state: "visible" });
	await setAdminPreferences(page, "en", "Europe/London");
	await page.reload();
	await page.getByRole("button", { name: /Open account menu for Ridu Administrator/ }).click();
	await page.getByRole("link", { name: "Profile & preferences" }).click();

	await chooseRiduSelect(page, page.getByLabel("Interface language"), "Français");
	await expect(page.locator("html")).toHaveAttribute("lang", "fr");
	await expect(page.locator("html")).toHaveAttribute("dir", "ltr");
	await expect(page.getByRole("heading", { name: "Profil et préférences" })).toBeVisible();

	await chooseRiduSelect(page, page.getByLabel("Fuseau horaire"), "Paris");
	await expect
		.poll(async () =>
			page.evaluate(async () => {
				const response = await fetch("/api/preferences/admin-timezone");
				if (!response.ok) return undefined;
				return ((await response.json()) as { value: unknown }).value;
			})
		)
		.toBe("Europe/Paris");

	await page.reload();
	await expect(page.locator("html")).toHaveAttribute("lang", "fr");
	await expectRiduSelectValue(page.getByLabel("Langue de l’interface"), "Français");
	await expectRiduSelectValue(page.getByLabel("Fuseau horaire"), "Paris");

	await chooseRiduSelect(page, page.getByLabel("Langue de l’interface"), "العربية");
	await expect(page.locator("html")).toHaveAttribute("lang", "ar");
	await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
	await expect(page.locator("html")).toHaveAttribute("data-admin-language", "ar");

	await page.goto("/admin");
	await expect(page.getByRole("heading", { name: "النشاط التحريري" })).toBeVisible();
	await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
});

async function setAdminPreferences(page: Page, language: string, timeZone: string) {
	await page.evaluate(
		async ({ language, timeZone }) => {
			const write = async (key: string, value: string) => {
				const response = await fetch(`/api/preferences/${key}`, {
					method: "PUT",
					headers: { "content-type": "application/json" },
					body: JSON.stringify({ value }),
				});
				if (!response.ok) throw new Error(`Could not reset ${key}: ${response.status}`);
			};
			await Promise.all([write("admin-language", language), write("admin-timezone", timeZone)]);
			localStorage.setItem("ridu:admin-language", language);
			localStorage.setItem("ridu:admin-timezone", timeZone);
		},
		{ language, timeZone }
	);
}
