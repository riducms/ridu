import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { expect, test, type APIRequestContext } from "@playwright/test";

import { documentSaveButton, observePageErrors } from "./helpers";

const apiURL = requiredEnvironment("RIDU_MONGODB_GENERATED_API_URL").replace(/\/$/, "");
const projectRoot = requiredEnvironment("RIDU_MONGODB_GENERATED_PROJECT_ROOT");
const developmentLog = requiredEnvironment("RIDU_MONGODB_GENERATED_LOG");
const postsSource = join(projectRoot, "content", "posts.go");

type SchemaEnvelope = {
	schema: {
		collections: Array<{
			slug: string;
			fields: Array<{ name: string; index?: boolean; unique: boolean }>;
		}>;
	};
};

type DocumentEnvelope = {
	doc: {
		id: string;
		title?: string;
		summary?: string | null;
		content?: { version?: number; root?: unknown } | null;
	};
};

test("a generated MongoDB starter survives ordinary authoring and safe schema reloads", async ({
	page,
	context,
}) => {
	test.setTimeout(180_000);
	const { consoleErrors, pageErrors } = observePageErrors(page);

	await page.goto("/admin/login");
	await expect(page).toHaveURL(/\/admin\/create-first-user$/);
	await expect(page.getByRole("heading", { name: "Welcome", exact: true })).toBeVisible();

	await page.getByLabel("Email", { exact: true }).fill("admin@mongodb-generated.test");
	await page
		.getByRole("textbox", { name: "Password", exact: true })
		.fill("mongodb-generated-password");
	await page
		.getByRole("textbox", { name: "Confirm password", exact: true })
		.fill("mongodb-generated-password");
	await page.getByRole("button", { name: "Create account" }).click();
	await expect(page).toHaveURL(/\/admin\/?$/);

	await page.goto("/admin/collections/posts/create");
	await page.getByLabel("Title", { exact: true }).fill("Generated MongoDB authoring");
	const content = page.getByRole("textbox", { name: "Content" });
	await content.fill("Rich text survives the ordinary generated-project path.");
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

	await page.getByLabel("Title", { exact: true }).fill("Generated MongoDB authoring edited");
	await content.fill("Rich text was edited through the generated MongoDB admin.");
	const updateResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/posts/${postID}`
	);
	await documentSaveButton(page).click();
	expect((await updateResponse).ok()).toBe(true);
	await page.reload();
	await expect(page.getByLabel("Title", { exact: true })).toHaveValue(
		"Generated MongoDB authoring edited"
	);
	await expect(page.getByRole("textbox", { name: "Content" })).toContainText(
		"Rich text was edited through the generated MongoDB admin."
	);
	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);

	await context.clearCookies();
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("admin@mongodb-generated.test");
	await page
		.getByRole("textbox", { name: "Password", exact: true })
		.fill("mongodb-generated-password");
	const loginResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === "/api/auth/users/login"
	);
	await page.getByRole("button", { name: "Sign in" }).click();
	expect((await loginResponse).ok()).toBe(true);
	await expect(page).toHaveURL(/\/admin\/?$/);
	await expect
		.poll(async () => (await page.request.get(`${apiURL}/api/auth/me`)).status())
		.toBe(200);
	// Clearing the cookie deliberately makes the admin's session probe return
	// 401 once. Keep that expected boundary out of the later console-health
	// assertion without hiding errors from setup or authenticated authoring.
	consoleErrors.length = 0;
	pageErrors.length = 0;
	await page.goto(`/admin/collections/posts/${postID}`);
	await expect(page).toHaveURL(new RegExp(`/admin/collections/posts/${postID}$`));
	await expect(page.getByLabel("Title", { exact: true })).toHaveValue(
		"Generated MongoDB authoring edited"
	);
	await expect(page.getByRole("textbox", { name: "Content" })).toContainText(
		"Rich text was edited through the generated MongoDB admin."
	);

	const originalSource = await readFile(postsSource, "utf8");
	const titleDefinition = 'field.Text("title", field.Required()),';
	expect(originalSource).toContain(titleDefinition);
	const indexedSummaryDefinition = 'field.Text("summary", field.Index()),';
	const additiveSource = originalSource.replace(
		titleDefinition,
		`${titleDefinition}\n\t\t\t${indexedSummaryDefinition}`
	);
	await writeFile(postsSource, additiveSource);

	await expect
		.poll(() => summaryIndexContract(page.request), {
			message: "the accepted additive schema should be served after hot reload",
			timeout: 90_000,
		})
		.toBe("index=true unique=false");
	await expect(page.getByLabel("Summary", { exact: true })).toBeVisible({ timeout: 45_000 });
	await expect(page.getByLabel("Title", { exact: true })).toHaveValue(
		"Generated MongoDB authoring edited"
	);
	await expect(page.getByRole("textbox", { name: "Content" })).toContainText(
		"Rich text was edited through the generated MongoDB admin."
	);
	await page.getByLabel("Summary", { exact: true }).fill("Indexed additive reload");
	const summaryUpdateResponse = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/posts/${postID}`
	);
	await documentSaveButton(page).click();
	expect((await summaryUpdateResponse).ok()).toBe(true);

	const retainedAfterReload = await readPost(page.request, postID!);
	expect(retainedAfterReload.title).toBe("Generated MongoDB authoring edited");
	expect(retainedAfterReload.summary).toBe("Indexed additive reload");
	expect(JSON.stringify(retainedAfterReload.content)).toContain(
		"Rich text was edited through the generated MongoDB admin."
	);

	const uniqueSummaryDefinition = 'field.Text("summary", field.Unique()),';
	await writeFile(
		postsSource,
		additiveSource.replace(indexedSummaryDefinition, uniqueSummaryDefinition)
	);
	await expect
		.poll(async () => await readFile(developmentLog, "utf8"), {
			message: "incompatible physical index drift should reject the replacement",
			timeout: 90_000,
		})
		.toMatch(/\[ERROR\] \[ridu\] Reload rejected;.*MongoDB physical index drift/s);

	await expect
		.poll(() => summaryIndexContract(page.request), {
			message: "the last-good server should keep serving the accepted schema",
		})
		.toBe("index=true unique=false");
	const retainedAfterRejection = await readPost(page.request, postID!);
	expect(retainedAfterRejection.title).toBe("Generated MongoDB authoring edited");
	expect(retainedAfterRejection.summary).toBe("Indexed additive reload");
	const readiness = await page.request.get(`${apiURL}/readyz`);
	expect(readiness.ok()).toBe(true);

	expect(consoleErrors).toEqual([]);
	expect(pageErrors).toEqual([]);
});

async function summaryIndexContract(request: APIRequestContext) {
	const response = await request.get(`${apiURL}/api/schema`);
	if (!response.ok()) return `status=${response.status()}`;
	const envelope = (await response.json()) as SchemaEnvelope;
	const summary = envelope.schema.collections
		.find((collection) => collection.slug === "posts")
		?.fields.find((field) => field.name === "summary");
	if (summary === undefined) return "missing";
	return `index=${summary.index === true} unique=${summary.unique === true}`;
}

async function readPost(request: APIRequestContext, id: string) {
	const response = await request.get(`${apiURL}/api/collections/posts/${encodeURIComponent(id)}`);
	expect(response.ok()).toBe(true);
	return ((await response.json()) as DocumentEnvelope).doc;
}

function requiredEnvironment(name: string) {
	const value = process.env[name]?.trim();
	if (!value) throw new Error(`${name} is required`);
	return value;
}
