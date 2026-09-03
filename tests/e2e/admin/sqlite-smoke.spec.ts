import { expect, test } from "./fixture";

import { documentSaveButton, loginAsEditor } from "./helpers";

test("SQLite backs the ordinary admin create and edit flow", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	await expect(page.getByRole("link", { name: "Welcome to Ridu", exact: true })).toBeVisible();

	await page.goto("/admin/collections/posts/create");
	await page.locator('input[name="title"]').fill("SQLite smoke post");
	await page
		.getByLabel("Summary — English", { exact: true })
		.fill("Created through the embedded adapter smoke test.");
	await documentSaveButton(page).click();
	await expect(page).toHaveURL(
		/\/admin\/collections\/posts\/(?!create(?:\?|$))[^/?]+(?:\?locale=en)?$/
	);

	const documentID = new URL(page.url()).pathname.split("/").at(-1);
	expect(documentID).toBeTruthy();
	let response = await page.request.get(`/api/collections/posts/${documentID}`);
	expect(response.ok()).toBe(true);
	expect((await response.json()).doc.title).toBe("SQLite smoke post");

	await page.locator('input[name="title"]').fill("SQLite smoke post updated");
	await documentSaveButton(page).click();
	await page.reload();
	await expect(page.locator('input[name="title"]')).toHaveValue("SQLite smoke post updated");
	response = await page.request.get(`/api/collections/posts/${documentID}`);
	expect(response.ok()).toBe(true);
	expect((await response.json()).doc.title).toBe("SQLite smoke post updated");
});
