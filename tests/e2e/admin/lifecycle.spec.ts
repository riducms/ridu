import { expect, test } from "./fixture";

import { documentSaveButton, loginAsEditor, observePageErrors } from "./helpers";

test("document duplication, trash restoration, and the mobile editor remain complete", async ({
	page,
}) => {
	test.setTimeout(45_000);
	const { consoleErrors, pageErrors } = observePageErrors(page);
	const collectionsNavigation = await loginAsEditor(page);
	consoleErrors.length = 0;
	await page.goto("/admin/collections/posts/create");
	await page.locator('input[name="title"]').fill("Browser lifecycle post");
	await page.getByLabel("Summary").fill("Lifecycle contract");
	await documentSaveButton(page).click();
	await expect(page).toHaveURL(
		/\/admin\/collections\/posts\/(?!create(?:\?|$))[^/?]+(?:\?locale=en)?$/
	);
	const directURL = page.url();
	await page.evaluate(async () => {
		const response = await fetch("/api/auth/users/login", {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify({ email: "admin@riducms.test", password: "ridu-admin" }),
		});
		if (!response.ok) throw new Error(`administrator login failed with ${response.status}`);
	});
	await page.reload();
	await expect(
		page.getByRole("button", { name: /Open account menu for Ridu Administrator/ })
	).toBeVisible();
	await page.getByRole("dialog").getByRole("button", { name: "Take over", exact: true }).click();
	await page.getByRole("button", { name: "More actions" }).click();
	await page.getByRole("button", { name: "Duplicate", exact: true }).last().click();
	await expect(page.locator('input[name="title"]')).toHaveValue("Copy of Browser lifecycle post");
	await page
		.getByRole("navigation", { name: "Breadcrumb" })
		.getByRole("link", { name: "Posts", exact: true })
		.click();
	await page.getByRole("checkbox", { name: "Select Copy of Browser lifecycle post" }).click();
	await page
		.getByRole("region", { name: "Bulk actions" })
		.getByRole("button", { name: "Delete", exact: true })
		.click();
	await page
		.getByRole("alertdialog", { name: "Delete 1 selected document?" })
		.getByRole("button", { name: "Delete", exact: true })
		.click();
	await expect(page.getByText("Copy of Browser lifecycle post", { exact: true })).toHaveCount(0);
	await page
		.getByRole("navigation", { name: "Collection views" })
		.getByRole("link", { name: "Trash", exact: true })
		.click();
	await expect(
		page
			.getByRole("navigation", { name: "Collection views" })
			.getByRole("button", { name: "Trash", exact: true })
	).toHaveAttribute("aria-current", "page");
	await expect(page.getByText("Copy of Browser lifecycle post", { exact: true })).toBeVisible();
	await expect(page.getByRole("columnheader", { name: "Actions", exact: true })).toHaveCount(0);
	const trashSearch = page.getByPlaceholder(/^Search by /);
	await trashSearch.fill("No matching trashed post");
	await expect(page.getByText("No Posts in trash.", { exact: true })).toBeVisible();
	await expect(page.getByRole("button", { name: "Empty trash", exact: true })).toBeEnabled();
	await page.getByRole("button", { name: "Empty trash", exact: true }).click();
	const emptyTrashDialog = page.getByRole("alertdialog", { name: "Empty posts trash?" });
	await expect(emptyTrashDialog).toContainText("including those outside the current filters");
	await emptyTrashDialog.getByRole("button", { name: "Cancel", exact: true }).click();
	await expect(emptyTrashDialog).toBeHidden();
	await trashSearch.fill("");
	await expect(page.getByText("Copy of Browser lifecycle post", { exact: true })).toBeVisible();
	await page.getByRole("button", { name: "Empty trash", exact: true }).click();
	await page
		.getByRole("alertdialog", { name: "Empty posts trash?" })
		.getByRole("button", { name: "Empty trash", exact: true })
		.click();
	await expect(emptyTrashDialog).toBeHidden();
	await expect(page.getByText("No Posts in trash.", { exact: true })).toBeVisible();
	await expect(page.getByRole("button", { name: "Empty trash", exact: true })).toBeDisabled();
	await expect(page.getByRole("navigation", { name: "Pagination" })).toHaveCount(0);
	await page.goto(directURL);

	await page.getByRole("button", { name: "More actions" }).click();
	await page.getByRole("button", { name: "Delete document", exact: true }).click();
	await expect(page.getByRole("heading", { name: "Move this document to trash?" })).toBeVisible();
	await page.getByRole("button", { name: "Move to trash" }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts(?:\?locale=en)?$/);
	await expect(page.getByText("Browser lifecycle post", { exact: true })).toHaveCount(0);
	await page
		.getByRole("navigation", { name: "Collection views" })
		.getByRole("link", { name: "Trash", exact: true })
		.click();
	await expect(
		page
			.getByRole("navigation", { name: "Collection views" })
			.getByRole("button", { name: "Trash", exact: true })
	).toHaveAttribute("aria-current", "page");
	await expect(page).toHaveURL(/\/admin\/collections\/posts\/trash(?:\?locale=en)?$/);
	await expect(page.getByText("Browser lifecycle post", { exact: true })).toBeVisible();
	await page.getByRole("checkbox", { name: "Select Browser lifecycle post" }).check();
	const permanentDeleteButton = page
		.getByRole("region", { name: "Bulk actions" })
		.getByRole("button", { name: "Delete", exact: true });
	await permanentDeleteButton.click();
	const permanentDeleteDialog = page.getByRole("alertdialog", {
		name: "Permanently delete 1 selected document?",
	});
	await expect(permanentDeleteDialog).toBeVisible();
	await page.keyboard.press("Escape");
	await expect(permanentDeleteDialog).toBeHidden();
	await expect(permanentDeleteButton).toBeFocused();
	await page.getByRole("checkbox", { name: "Select Browser lifecycle post" }).check();
	const restoreButton = page.getByRole("button", { name: "Restore", exact: true }).last();
	await restoreButton.click();
	const restoreDialog = page.getByRole("alertdialog", {
		name: "Restore 1 selected document?",
	});
	await expect(restoreDialog).toBeVisible();
	await expect(restoreDialog.getByRole("button", { name: "Cancel" })).toBeFocused();
	await page.keyboard.press("Escape");
	await expect(restoreDialog).toBeHidden();
	await expect(restoreButton).toBeFocused();
	await restoreButton.click();
	await restoreDialog.getByRole("button", { name: "Cancel" }).click();
	await expect(restoreDialog).toBeHidden();
	await expect(restoreButton).toBeFocused();
	await restoreButton.click();
	await restoreDialog.getByRole("button", { name: "Restore", exact: true }).click();
	await expect(page.getByText("No Posts in trash.", { exact: true })).toBeVisible();
	await expect(page.getByRole("button", { name: "Empty trash", exact: true })).toBeDisabled();
	await expect(page.getByRole("navigation", { name: "Pagination" })).toHaveCount(0);
	await page.goto(directURL);
	await expect(page.locator('input[name="title"]')).toHaveValue("Browser lifecycle post");

	await page.setViewportSize({ width: 390, height: 844 });
	await page.reload();
	await expect(collectionsNavigation).toBeHidden();
	await page.getByRole("button", { name: "Open navigation" }).click();
	await expect(
		page
			.getByRole("navigation", { name: "Admin navigation" })
			.getByRole("link", { name: "Posts", exact: true })
	).toBeVisible();
	await expect(documentSaveButton(page)).toBeVisible();
	await expect(page.getByRole("navigation", { name: "Document views" })).toBeVisible();
	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});
