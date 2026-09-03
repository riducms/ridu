import { expect, test } from "./fixture";

import {
	chooseContentLocale,
	chooseRiduSelect,
	documentSaveButton,
	expectRiduSelectValue,
} from "./helpers";

test("localized authoring preserves fallback provenance and isolates locale writes", async ({
	page,
}) => {
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in" }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor({ state: "visible" });
	const locale = page.getByLabel("Content locale", { exact: true });
	await expect(locale).toBeVisible();
	await expectRiduSelectValue(locale, "English");
	await page.setViewportSize({ width: 390, height: 844 });
	await expect(locale).toBeVisible();
	await page.setViewportSize({ width: 1280, height: 720 });
	await page.getByRole("link", { name: "Posts", exact: true }).click();
	await expect(locale).toBeVisible();
	await page.getByRole("link", { name: "Dashboard", exact: true }).click();
	await expect(locale).toBeVisible();
	await page.goto("/admin/account");
	await expect(locale).toBeVisible();
	await page.goto("/admin/globals/site-settings");
	await expect(locale).toBeVisible();
	await page.goto("/admin/plugin-contract");
	await expect(locale).toBeVisible();

	const { postID } = await page.evaluate(async () => {
		const response = await fetch("/api/collections/posts?locale=en", {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify({
				title: "English localization proof",
				summary: "English fallback summary",
				slug: "localization-proof",
			}),
		});
		if (!response.ok) throw new Error(`localized fixture create failed with ${response.status}`);
		const envelope = (await response.json()) as { doc?: { id?: string } };
		if (typeof envelope.doc?.id !== "string") throw new Error("localized fixture returned no id");
		const targetResponse = await fetch("/api/collections/posts?locale=en", {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify({
				title: "English relationship target",
				summary: "English target summary",
				slug: "localized-relationship-target",
			}),
		});
		if (!targetResponse.ok)
			throw new Error(`localized relationship target create failed with ${targetResponse.status}`);
		const targetEnvelope = (await targetResponse.json()) as { doc?: { id?: string } };
		if (typeof targetEnvelope.doc?.id !== "string")
			throw new Error("localized relationship target returned no id");
		const targetUpdate = await fetch(`/api/collections/posts/${targetEnvelope.doc.id}?locale=fr`, {
			method: "PATCH",
			headers: { "content-type": "application/json" },
			body: JSON.stringify({
				title: "Cible relationnelle française",
				summary: "Résumé de la cible française",
			}),
		});
		if (!targetUpdate.ok)
			throw new Error(`localized relationship target update failed with ${targetUpdate.status}`);
		return { postID: envelope.doc.id };
	});

	await page.goto(`/admin/collections/posts/${postID}?locale=fr`);
	await expectRiduSelectValue(locale, "French");
	await expect(page.getByLabel("Title — French", { exact: true })).toHaveValue(
		"English localization proof"
	);
	await expect(page.getByLabel("Slug", { exact: true })).toBeVisible();
	await expect(page.getByText("Inherited from English", { exact: true }).first()).toBeVisible();
	await page.getByRole("button", { name: "Parent", exact: true }).click();
	await page.getByLabel("Search Posts").fill("Cible relationnelle française");
	await expect(
		page.getByRole("option", { name: "Cible relationnelle française", exact: true })
	).toBeVisible();
	await page.keyboard.press("Escape");

	await page.locator('input[name="title"]').fill("Preuve de localisation française");
	await page.locator('textarea[name="summary"]').fill("Résumé français isolé");
	await expect(locale).toBeDisabled();
	await expect(page.locator('input[name="title"]')).toHaveValue("Preuve de localisation française");
	await documentSaveButton(page).click();
	await expect(page.getByText("Updated successfully.")).toBeVisible();
	await expect(page.getByText("Inherited from English", { exact: true })).toHaveCount(0);
	await expect(page.getByLabel("Title — French", { exact: true })).toBeVisible();
	await page.goto(`/admin/collections/posts/${postID}/versions?locale=fr`);
	await expect(locale).toBeVisible();
	await expectRiduSelectValue(locale, "French");
	await page.goto(`/admin/collections/posts/${postID}?locale=fr`);

	await chooseContentLocale(page, "English", "en");
	await expect(page).toHaveURL(new RegExp(`${postID}\\?locale=en$`));
	await expect(page.getByLabel("Title — English", { exact: true })).toBeVisible();
	await expect(page.locator('input[name="title"]')).toHaveValue("English localization proof");
	await chooseContentLocale(page, "French", "fr");
	await expect(page.locator('input[name="title"]')).toHaveValue("Preuve de localisation française");
	await page.getByRole("link", { name: "Dashboard", exact: true }).click();
	await expectRiduSelectValue(locale, "French");
	await page.reload();
	await expectRiduSelectValue(locale, "French");
	const usersRequest = page.waitForRequest((request) => {
		const url = new URL(request.url());
		return request.method() === "GET" && url.pathname === "/api/collections/users";
	});
	await page.getByRole("link", { name: "Users", exact: true }).click();
	expect(new URL((await usersRequest).url()).searchParams.get("locale")).toBe("fr");
	await expect(page).toHaveURL(/\/admin\/collections\/users\?locale=fr$/);
	await page.getByRole("link", { name: "Posts", exact: true }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts\?locale=fr$/);
	await page.goto(`/admin/collections/posts/${postID}?locale=fr`);

	await chooseRiduSelect(page, page.getByLabel("Copy localized values"), /^English en$/);
	await expect(page.getByText("Localized values from en now populate fr.")).toBeVisible();
	await expect(page.locator('input[name="title"]')).toHaveValue("English localization proof");
});

test("a locale in the post-login destination wins before shell access is evaluated", async ({
	page,
}) => {
	const destination = "/collections/posts?locale=fr";
	await page.goto(`/admin/login?redirect=${encodeURIComponent(destination)}`);
	await page.getByLabel("Email address").fill("admin@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-admin");
	await page.getByRole("button", { name: "Sign in" }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts\?locale=fr$/);
	await expectRiduSelectValue(page.getByLabel("Content locale", { exact: true }), "French");
});
