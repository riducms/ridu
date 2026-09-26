import { expect, test } from "./fixture";

test("collection API reference stays lazy, documents real contracts and follows document identity", async ({
	page,
}) => {
	const referenceChunks: string[] = [];
	const mutations: string[] = [];
	page.on("request", (request) => {
		if (/api-reference-content-.*\.js/.test(request.url())) referenceChunks.push(request.url());
		if (
			request.url().includes("/api/collections/") &&
			!request.url().endsWith("/lock") &&
			!["GET", "HEAD"].includes(request.method())
		)
			mutations.push(request.url());
	});

	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in", exact: true }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor();
	await page.goto("/admin/collections/posts");
	const trigger = page.getByRole("button", { name: "API reference", exact: true });
	await expect(trigger).toBeVisible();
	expect(referenceChunks).toHaveLength(0);
	await trigger.click();
	const drawer = page.getByRole("dialog", { name: "API reference posts" });
	await expect(drawer.getByRole("heading", { name: "List / Search (posts)" })).toBeVisible();
	expect(referenceChunks.length).toBeGreaterThan(0);
	await expect(drawer.getByRole("region", { name: "Request example", exact: true })).toContainText(
		'"title"'
	);
	await drawer.getByRole("button", { name: "Create", exact: true }).click();
	await drawer.getByRole("tab", { name: "cURL", exact: true }).click();
	await expect(drawer.getByRole("region", { name: "Request example", exact: true })).toContainText(
		"--request POST"
	);
	await drawer.getByRole("tab", { name: "422", exact: true }).click();
	await expect(drawer.getByRole("region", { name: "JSON data" })).toContainText("validation");
	await drawer.getByRole("button", { name: "Delete", exact: true }).click();
	await expect(drawer).toContainText("moves this collection’s documents to trash");
	await page.keyboard.press("Escape");
	await expect(drawer).toBeHidden();
	await expect(trigger).toBeFocused();

	const post = page.getByRole("link", { name: "Welcome to Ridu", exact: true });
	const documentPath = await post.getAttribute("href");
	expect(documentPath).toBeTruthy();
	await post.click();
	await page.getByRole("link", { name: "API", exact: true }).click();
	await page.getByRole("button", { name: "API reference", exact: true }).click();
	await drawer.getByRole("button", { name: "Read", exact: true }).click();
	const documentID = new URL(documentPath!, page.url()).pathname.split("/").at(-1)!;
	await expect(drawer.getByRole("region", { name: "Request example", exact: true })).toContainText(
		documentID
	);

	await page.setViewportSize({ width: 390, height: 844 });
	await page.emulateMedia({ reducedMotion: "reduce" });
	const layout = await drawer.evaluate((element) => ({
		width: element.getBoundingClientRect().width,
		overflow: element.scrollWidth - element.clientWidth,
		animation: getComputedStyle(element).animationName,
	}));
	expect(layout.width).toBeLessThanOrEqual(390);
	expect(layout.overflow).toBe(0);
	expect(layout.animation).toBe("none");
	const body = drawer.locator(".ridu-api-reference__body");
	expect(await body.evaluate((element) => element.scrollWidth - element.clientWidth)).toBe(0);
	await drawer.getByRole("button", { name: "Close", exact: true }).click();
	await expect(drawer).toBeHidden();
	expect(mutations).toEqual([]);
});

test("API reference recovers a failed lazy load through a page reload", async ({ page }) => {
	await page.goto("/admin/login");
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in", exact: true }).click();
	await page.getByRole("navigation", { name: "Admin navigation" }).waitFor();
	await page.goto("/admin/collections/posts");
	await page.route("**/api-reference-content-*.js", (route) => route.abort(), { times: 1 });
	await page.getByRole("button", { name: "API reference", exact: true }).click();
	const drawer = page.getByRole("dialog", { name: "API reference posts" });
	await expect(drawer.getByRole("alert")).toBeVisible();
	await drawer.getByRole("button", { name: "Reload page", exact: true }).click();
	await expect(drawer).toBeHidden();
	await page.getByRole("button", { name: "API reference", exact: true }).click();
	await expect(drawer.getByRole("heading", { name: "List / Search (posts)" })).toBeVisible();
});
