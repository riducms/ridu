import { createClient } from "@riducms/sdk";
import type { AdminConfig } from "../../../admin/src/core/api/admin-client";
import { expect, test, type Page } from "./fixture";
import { loginAsEditor, observePageErrors } from "./helpers";

async function reloadDirtyEditor(page: Page) {
	page.once("dialog", (dialog) => dialog.accept());
	await page.reload();
}

test("the restored checkpoint toast discards changes and the next reload shows the saved document", async ({
	page,
}) => {
	const errors = observePageErrors(page);
	await loginAsEditor(page);
	await page.goto("/admin/collections/categories/categories_4");
	const name = page.getByLabel("Name", { exact: true });
	await name.fill("Unsaved recovered category");
	await reloadDirtyEditor(page);
	await expect(name).toHaveValue("Unsaved recovered category");
	const toast = page.locator("[data-sonner-toast]").filter({ hasText: /Restored unsaved changes/ });
	await expect(toast).toBeVisible();
	const discard = toast.getByRole("button", { name: "Discard changes", exact: true });
	await discard.focus();
	await discard.press("Enter");
	await expect(name).toHaveValue("News");
	expect(
		await page.evaluate(() =>
			Object.keys(sessionStorage).filter((key) => key.startsWith("ridu:form-recovery:"))
		)
	).toEqual([]);
	await page.reload();
	await expect(name).toHaveValue("News");
	await expect(page.getByText(/Restored unsaved changes/)).toHaveCount(0);
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

for (const choice of ["Keep yours", "Load latest"] as const) {
	test(`a newer SDK publication requires comparison and an explicit ${choice} choice`, async ({
		page,
		adminServer,
	}) => {
		const errors = observePageErrors(page);
		await page.clock.install();
		await loginAsEditor(page);
		const cookies = await page.context().cookies(adminServer.url);
		const client = createClient<AdminConfig>({
			baseURL: adminServer.url,
			headers: { Cookie: cookies.map(({ name, value }) => `${name}=${value}`).join("; ") },
		});
		const initial = await client.create(
			"posts",
			{ title: "Saved before checkpoint", summary: "Initial summary" },
			{ draft: true, locale: "en" }
		);
		await page.goto(`/admin/collections/posts/${initial.id}?locale=en`);
		const title = page.locator('input[name="title"]');
		await title.fill("Your old unsaved title");
		await reloadDirtyEditor(page);
		await expect(title).toHaveValue("Your old unsaved title");
		const published = await client.publishChanges(
			"posts",
			initial.id,
			{ title: "New SDK publication", summary: "New SDK summary" },
			{ revision: initial._revision as number, locale: "en" }
		);
		expect(published._revision).toBe((initial._revision as number) + 1);
		if (choice === "Load latest") await page.setViewportSize({ width: 390, height: 844 });
		await reloadDirtyEditor(page);
		const comparison = page.getByRole("dialog", { name: "Review unsaved changes" });
		await expect(comparison).toBeVisible();
		await expect(title).toHaveValue("New SDK publication");
		await expect(title).toBeDisabled();
		await expect(comparison).toContainText("Saved before checkpoint");
		await expect(comparison).toContainText("Your old unsaved title");
		await expect(comparison).toContainText("New SDK publication");
		await expect(comparison).toContainText("New SDK summary");
		await expect(page.getByText(/Restored unsaved changes/)).toHaveCount(0);
		expect(await comparison.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(
			true
		);
		await page.keyboard.press("Escape");
		const review = page.getByRole("button", { name: "Compare unsaved changes", exact: true });
		await expect(review).toBeFocused();
		await review.press("Enter");
		await expect(comparison).toBeVisible();
		const choose = comparison.getByRole("button", { name: choice, exact: true });
		await choose.focus();
		await choose.press("Enter");
		await expect(comparison).toBeHidden();
		await expect(title).toBeEnabled();
		await expect(title).toHaveValue(
			choice === "Keep yours" ? "Your old unsaved title" : "New SDK publication"
		);
		await expect(page.getByLabel("Summary — English", { exact: true })).toHaveValue(
			"New SDK summary"
		);
		const saved = await client.find("posts", initial.id, { locale: "en" });
		expect(saved.title).toBe("New SDK publication");
		if (choice === "Keep yours") {
			await reloadDirtyEditor(page);
			await expect(comparison).toBeHidden();
			await expect(title).toHaveValue("Your old unsaved title");
			await page
				.locator("[data-sonner-toast]")
				.filter({ hasText: /Restored unsaved changes/ })
				.getByRole("button", { name: "Discard changes", exact: true })
				.click();
		}
		await page.reload();
		await expect(title).toHaveValue("New SDK publication");
		await expect(comparison).toBeHidden();
		expect(errors.pageErrors).toEqual([]);
		expect(errors.consoleErrors).toEqual([]);
	});
}
