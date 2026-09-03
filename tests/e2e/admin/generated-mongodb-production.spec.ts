import { writeFile } from "node:fs/promises";

import { expect, test } from "@playwright/test";

import { documentSaveButton, observePageErrors } from "./helpers";

const email = requiredEnvironment("RIDU_MONGODB_PRODUCTION_ADMIN_EMAIL");
const password = requiredEnvironment("RIDU_MONGODB_PRODUCTION_ADMIN_PASSWORD");
const resultPath = requiredEnvironment("RIDU_MONGODB_PRODUCTION_ADMIN_RESULT");

test("the release-built MongoDB admin preserves create and edit work across reload", async ({
	page,
}) => {
	const { consoleErrors, pageErrors } = observePageErrors(page);

	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill(email);
	await page.getByRole("textbox", { name: "Password", exact: true }).fill(password);
	const loginResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === "/api/auth/users/login"
	);
	await page.getByRole("button", { name: "Sign in" }).click();
	expect((await loginResponse).ok()).toBe(true);
	await expect(page).toHaveURL(/\/admin\/?$/);
	await expect.poll(async () => (await page.request.get("/api/auth/me")).status()).toBe(200);
	// The unauthenticated login route performs one intentional session probe.
	// Keep its 401 out of the authenticated authoring health assertion without
	// hiding any other console or page error from embedded-admin startup.
	expect(consoleErrors.filter((message) => !message.includes("401 (Unauthorized)"))).toEqual([]);
	expect(pageErrors).toEqual([]);
	consoleErrors.length = 0;
	pageErrors.length = 0;

	await page.goto("/admin/collections/posts/create");
	await page.getByLabel("Title", { exact: true }).fill("MongoDB production admin canary");
	await page
		.getByRole("textbox", { name: "Content" })
		.fill("Created through the release-built embedded admin.");
	const createResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === "/api/collections/posts"
	);
	await documentSaveButton(page).click();
	expect((await createResponse).ok()).toBe(true);
	await expect(page).toHaveURL(/\/admin\/collections\/posts\/[^/]+$/);
	const postID = new URL(page.url()).pathname.split("/").at(-1);
	expect(postID).toBeTruthy();

	await page.getByLabel("Title", { exact: true }).fill("MongoDB production admin canary edited");
	await page
		.getByRole("textbox", { name: "Content" })
		.fill("Edited through the release-built embedded admin.");
	const updateResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/posts/${postID}/publish`
	);
	await page.getByRole("button", { name: "Publish changes", exact: true }).click();
	expect((await updateResponse).ok()).toBe(true);

	await page.reload();
	await expect(page).toHaveURL(new RegExp(`/admin/collections/posts/${postID}$`));
	await expect(page.getByLabel("Title", { exact: true })).toHaveValue(
		"MongoDB production admin canary edited"
	);
	await expect(page.getByRole("textbox", { name: "Content" })).toContainText(
		"Edited through the release-built embedded admin."
	);
	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
	await writeFile(
		resultPath,
		JSON.stringify({
			postID,
			title: "MongoDB production admin canary edited",
			content: "Edited through the release-built embedded admin.",
		}) + "\n",
		{ mode: 0o600 }
	);
});

function requiredEnvironment(name: string) {
	const value = process.env[name]?.trim();
	if (!value) throw new Error(`${name} is required`);
	return value;
}
