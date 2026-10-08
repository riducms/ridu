import { expect, test } from "@playwright/test";

import { createClient } from "../../contracts/editor_app/src/lib/ridu.generated";

// tests/contracts/editor_app/src/routes/posts/[id] edits a post's richtext.Field and saves it to the
// project's Go server through the generated client.
function createPost() {
	const ridu = createClient({ baseURL: String(test.info().project.metadata.riduURL) });
	return ridu.create("posts", {
		title: "A post",
		body: {
			version: 1,
			root: {
				type: "root",
				children: [{ type: "paragraph", children: [{ type: "text", text: "Saved first" }] }],
			},
		},
	});
}

test("saves what the editor writes through REST and renders it", async ({ page }) => {
	const post = await createPost();
	await page.goto(`/posts/${post.id}`);
	const body = page.getByRole("textbox", { name: "Body" });
	await expect(body).toHaveAttribute("contenteditable", "true");

	await page.getByRole("button", { name: "Insert paragraph" }).click();
	await page.keyboard.type("# A heading");
	await page.keyboard.press("Enter");
	await page.keyboard.type("- An item");
	await page.keyboard.press("Enter");
	await page.keyboard.press("Enter");
	await page.keyboard.type("[Ridu](https://riducms.com) ");
	await page.getByRole("button", { name: "Save" }).click();

	// The Go field validated the document, and the page renders the saved copy with ./svelte.
	const saved = page.getByTestId("saved");
	await expect(saved.getByRole("heading", { name: "A heading" })).toBeVisible();
	await expect(saved.getByRole("listitem")).toHaveText("An item");
	await expect(saved.getByRole("link", { name: "Ridu" })).toHaveAttribute(
		"href",
		"https://riducms.com"
	);
	await expect(page.getByRole("alert")).toHaveCount(0);
	const ridu = createClient({ baseURL: String(test.info().project.metadata.riduURL) });
	const stored = await ridu.find("posts", post.id);
	expect(stored.body?.root.children.map((node) => node.type)).toEqual([
		"paragraph",
		"heading",
		"list",
		"paragraph",
	]);
});

test("follows the app's Sass configuration and custom properties", async ({ page }) => {
	const post = await createPost();
	await page.goto(`/posts/${post.id}`);
	// theme.scss configures $radius and $background, and sets --ridu-richtext-link.
	const toolbar = page.locator(".ridu-richtext-fixed-toolbar");
	await expect(toolbar).toHaveCSS("border-radius", "10px");
	await expect(toolbar).toHaveCSS("background-color", "rgb(255, 251, 242)");

	await page.getByRole("button", { name: "Insert paragraph" }).click();
	await page.keyboard.type("[Ridu](https://riducms.com) ");
	await expect(page.locator(".ridu-richtext-link")).toHaveCSS("color", "rgb(180, 83, 9)");
});
