import { expect, test } from "./fixture";
import { loginAsEditor, observePageErrors } from "./helpers";

test("the GraphQL playground runs operations with the admin session and documents the schema", async ({
	page,
}) => {
	const navigation = await loginAsEditor(page);
	const errors = observePageErrors(page);

	await navigation.getByRole("link", { name: "GraphQL" }).click();
	await expect(page).toHaveURL(/\/admin\/graphql$/);
	await expect(page.getByRole("heading", { name: "GraphQL playground", level: 1 })).toBeVisible();

	// The starter query targets a real collection list, so it runs without editing.
	const query = page.getByRole("textbox", { name: "Query" });
	await expect(query).toContainText("totalDocs");
	await query.click();
	await page.keyboard.press("ControlOrMeta+Enter");
	const response = page.getByRole("textbox", { name: "Response" });
	await expect(response).toContainText('"data"');
	await expect(response).toContainText('"totalDocs"');
	await response.focus();
	await expect(response).toBeFocused();
	await expect(page.getByText(/^200 · \d+ ms$/)).toBeVisible();

	// Variables must be one JSON object; the message clears after the next edit.
	const variables = page.getByRole("textbox", { name: "Variables" });
	await variables.click();
	await page.keyboard.type("[1]");
	await page.getByRole("button", { name: /^Run/ }).click();
	await expect(page.getByRole("alert")).toHaveText("Variables must be a single JSON object.");
	await variables.click();
	await page.keyboard.press("ControlOrMeta+A");
	await page.keyboard.press("Backspace");
	await expect(page.getByRole("alert")).toHaveCount(0);

	// Server errors come back in the ordinary GraphQL response shape.
	await query.click();
	await page.keyboard.press("ControlOrMeta+A");
	await page.keyboard.type("{ notAField }");
	await page.getByRole("button", { name: /^Run/ }).click();
	await expect(response).toContainText('"errors"');

	// The docs explorer starts at the root types and follows type links.
	await page.getByRole("button", { name: "Docs" }).click();
	const docs = page.getByRole("complementary", { name: "Docs" });
	await docs.getByRole("button", { name: "Query", exact: true }).click();
	await expect(docs.getByRole("heading", { name: "Query", level: 2 })).toBeFocused();
	await docs.getByRole("searchbox", { name: "Search types and fields" }).fill("totalDocs");
	await expect(docs.getByRole("button", { name: /\.totalDocs$/ }).first()).toBeVisible();
	await docs.getByRole("button", { name: "Close docs" }).click();
	await expect(docs).toHaveCount(0);

	// The last session is restored on return.
	await page.reload();
	await expect(query).toContainText("notAField");
	expect(errors).toEqual({ consoleErrors: [], pageErrors: [] });
});

test("the GraphQL playground schema is withheld from signed-out visitors", async ({ page }) => {
	const response = await page.request.get("/api/admin/loaders/graphql-playground?route=%2Fgraphql");
	expect(response.status()).toBe(403);
});

test("a cancelled GraphQL run cannot publish a late response", async ({ page }) => {
	await loginAsEditor(page);
	let requestStarted!: () => void;
	let releaseRequest!: () => void;
	let requestHandled!: () => void;
	const started = new Promise<void>((resolve) => {
		requestStarted = resolve;
	});
	const released = new Promise<void>((resolve) => {
		releaseRequest = resolve;
	});
	const handled = new Promise<void>((resolve) => {
		requestHandled = resolve;
	});
	await page.route("**/api/graphql", async (route) => {
		requestStarted();
		await released;
		try {
			await route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({ data: { late: true } }),
			});
		} catch {
			// The browser may discard the intercepted route as soon as AbortController fires.
		} finally {
			requestHandled();
		}
	});

	await page.goto("/admin/graphql");
	await page.getByRole("button", { name: /^Run/ }).click();
	await started;
	await page.getByRole("button", { name: "Cancel" }).click();
	const empty = page.getByText("Run an operation to see its response here.");
	await expect(empty).toBeVisible();
	releaseRequest();
	await handled;
	await expect(empty).toBeVisible();
	await expect(page.getByText('"late": true')).toHaveCount(0);
});

test("a failed playground chunk offers recovery without an unhandled rejection", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const chunk = /\/playground-[^/]+\.js$/;
	await page.route(chunk, (route) => route.fulfill({ status: 503, body: "Unavailable" }));
	await page.goto("/admin/graphql");
	const failure = page
		.getByRole("alert")
		.filter({ hasText: "The playground could not be loaded." });
	await expect(failure).toBeVisible();
	expect(errors.pageErrors).toEqual([]);
	await page.unroute(chunk);
	await failure.getByRole("button", { name: "Reload page" }).click();
	await expect(page.getByRole("heading", { name: "GraphQL playground", level: 1 })).toBeVisible();
	expect(errors.pageErrors).toEqual([]);
});
