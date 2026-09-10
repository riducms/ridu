import { expect, test, type Page } from "./fixture";
import {
	chooseContentLocale,
	documentSaveButton,
	loginAsEditor,
	observePageErrors,
} from "./helpers";

const collection = "primitive-products";
const list = (page: Page, path: string) => page.locator(`[data-field-path="${path}"]`).first();
const item = (page: Page, path: string, index: number) =>
	list(page, path).getByRole("textbox").nth(index);
async function save(page: Page, id: string) {
	const response = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/${collection}/${id}`
	);
	await documentSaveButton(page).click();
	const saved = await response;
	expect(saved.ok(), await saved.text()).toBe(true);
	return (await saved.json()).doc;
}
async function create(page: Page, data: Record<string, unknown>) {
	const response = await page.request.post(`/api/collections/${collection}?locale=en&draft=true`, {
		data: { title: "Primitive product", sellingPoints: ["Oak"], ...data },
	});
	expect(response.ok(), await response.text()).toBe(true);
	return (await response.json()).doc;
}

test("primitive lists support keyboard editing, duplicate values, custom editors, save/reload, duplicate and version restore", async ({
	page,
}) => {
	test.setTimeout(60_000);
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	await page.goto(`/admin/collections/${collection}/create`);
	await page.locator('input[name="title"]').fill("Oak chair");
	const points = list(page, "sellingPoints");
	await points.getByRole("button", { name: "Add item", exact: true }).click();
	await expect(item(page, "sellingPoints", 0)).toBeFocused();
	await item(page, "sellingPoints", 0).fill("Solid oak");
	await points.getByRole("button", { name: "Add item", exact: true }).click();
	await item(page, "sellingPoints", 1).fill("Solid oak");
	await points.getByRole("button", { name: "Add item", exact: true }).click();
	await item(page, "sellingPoints", 2).fill("Five-year warranty");
	await item(page, "sellingPoints", 2).press("Alt+ArrowUp");
	await expect(item(page, "sellingPoints", 1)).toBeFocused();
	for (const value of ["0", "8", "8"]) {
		await list(page, "availableSizes")
			.getByRole("button", { name: "Add item", exact: true })
			.click();
		await list(page, "availableSizes").getByRole("textbox").last().fill(value);
	}
	await page.locator('[data-local-editor="text-list"]').fill("Hand made\nHand made");
	await expect(item(page, "readOnlyPoints", 0)).toHaveAttribute("readonly");
	await expect(
		list(page, "readOnlyPoints").getByRole("button", { name: "Add item", exact: true })
	).toBeDisabled();
	await documentSaveButton(page).click();
	await expect(page).toHaveURL(new RegExp(`/admin/collections/${collection}/(?!create)[^/?]+`));
	const id = new URL(page.url()).pathname.split("/").at(-1)!;
	await page.reload();
	await expect(item(page, "sellingPoints", 1)).toHaveValue("Five-year warranty");
	await expect(item(page, "availableSizes", 0)).toHaveValue("0");
	await expect(item(page, "lockedSizes", 0)).toHaveAttribute("readonly");
	await expect(list(page, "privatePoints")).toHaveCount(0);
	const original = (await (await page.request.get(`/api/collections/${collection}/${id}`)).json())
		.doc;
	expect(original.sellingPoints).toEqual(["Solid oak", "Five-year warranty", "Solid oak"]);
	expect(original.availableSizes).toEqual([0, 8, 8]);
	expect(original.editorPoints).toEqual(["Hand made", "Hand made"]);
	await item(page, "sellingPoints", 0).fill("Changed");
	await save(page, id);
	// Snapshot restoration also writes protected fields, so use the authorized administrator.
	const administrator = await page.request.post("/api/auth/users/login", {
		data: { email: "admin@riducms.test", password: "ridu-admin" },
	});
	expect(administrator.ok(), await administrator.text()).toBe(true);
	await page.goto(`/admin/collections/${collection}/${id}/versions/${original._revision}`);
	await page.getByRole("button", { name: /^Restore(?: as draft)?$/, exact: true }).click();
	await expect(item(page, "sellingPoints", 0)).toHaveValue("Solid oak");
	await page.getByRole("button", { name: "More actions" }).click();
	await page.getByRole("button", { name: "Duplicate", exact: true }).last().click();
	await expect(page.locator('input[name="title"]')).toHaveValue("Oak chair");
	await expect(item(page, "availableSizes", 0)).toHaveValue("0");
	await expect(item(page, "sellingPoints", 2)).toHaveValue("Solid oak");
	expect(errors.pageErrors).toEqual([]);
});

test("nested, repeated and localized list edits preserve enclosing row identity and independent translations", async ({
	page,
}) => {
	await loginAsEditor(page);
	const original = await create(page, {
		details: { points: ["Group"], sizes: [0] },
		variants: [
			{ points: ["A", "A"], sizes: [8] },
			{ points: ["B"], sizes: [10] },
		],
		content: [{ blockType: "card", points: ["Card", "Last"], sizes: [12] }],
		localizedPoints: ["English", "English"],
		localizedSizes: [0, 8],
	});
	await page.goto(`/admin/collections/${collection}/${original.id}`);
	await item(page, "details.points", 0).fill("Group changed");
	await list(page, "variants.0.points")
		.getByRole("button", { name: "Move item 2 up", exact: true })
		.click();
	await list(page, "content.0.points")
		.getByRole("button", { name: "Remove item 1", exact: true })
		.click();
	await list(page, "variants")
		.getByRole("button", { name: "Open Row 1 actions", exact: true })
		.first()
		.click();
	await page.getByRole("menuitem", { name: "Move down", exact: true }).click();
	await item(page, "variants.1.sizes", 0).fill("0");
	const saved = await save(page, original.id);
	expect(saved.variants.map((row: { _key: string }) => row._key)).toEqual(
		original.variants.map((row: { _key: string }) => row._key).toReversed()
	);
	expect(saved.variants[1].points).toEqual(["A", "A"]);
	expect(saved.variants[1].sizes).toEqual([0]);
	expect(saved.content[0].points).toEqual(["Last"]);
	await chooseContentLocale(page, "French", "fr");
	await expect(item(page, "localizedPoints", 0)).toHaveValue("English");
	await item(page, "localizedPoints", 0).fill("Français");
	await item(page, "localizedSizes", 1).fill("12");
	await save(page, original.id);
	await chooseContentLocale(page, "English", "en");
	await expect(item(page, "localizedPoints", 0)).toHaveValue("English");
	await expect(item(page, "localizedSizes", 1)).toHaveValue("8");
	await chooseContentLocale(page, "French", "fr");
	await page.reload();
	await expect(item(page, "localizedPoints", 0)).toHaveValue("Français");
	await expect(item(page, "localizedSizes", 1)).toHaveValue("12");
});

for (const field of ["body", "localizedBody"] as const)
	test(`${field} primitive list editors preserve Apply/Cancel and reject incomplete numeric input`, async ({
		page,
	}) => {
		await loginAsEditor(page);
		const errors = observePageErrors(page);
		const original = await create(page, {
			[field]: {
				version: 1,
				root: {
					type: "root",
					version: 1,
					children: [
						{
							type: "block",
							version: 1,
							fields: { blockType: "card", points: ["Oak", "Oak"], sizes: [0, 8] },
						},
					],
				},
			},
		});
		await page.goto(`/admin/collections/${collection}/${original.id}`);
		if (field === "localizedBody") await chooseContentLocale(page, "French", "fr");
		const card = list(page, field).locator(".ridu-richtext-embedded-card").first();
		await card.getByRole("button", { name: "Edit", exact: true }).click();
		const drawer = page.getByRole("dialog", { name: "Edit Card", exact: true });
		const points = drawer.locator('[data-field-path$=".points"]').first();
		const sizes = drawer.locator('[data-field-path$=".sizes"]').first();
		await points.getByRole("button", { name: "Remove item 1", exact: true }).click();
		await drawer.getByRole("button", { name: "Cancel", exact: true }).click();
		await card.getByRole("button", { name: "Edit", exact: true }).click();
		await expect(points.getByRole("textbox")).toHaveCount(2);
		// The drawer schedules initial autofocus; let it finish before filling
		// another input so it cannot redirect Playwright's keyboard insertion.
		await expect(points.getByRole("textbox").first()).toBeFocused();
		await sizes.getByRole("textbox").nth(1).fill("1e-");
		await expect(sizes.getByRole("textbox").nth(1)).toHaveValue("1e-");
		await drawer.getByRole("button", { name: "Apply", exact: true }).click();
		await expect(drawer).toBeVisible();
		await expect(sizes).toContainText("item 2: enter a finite number");
		await sizes.getByRole("textbox").nth(1).fill("1e1");
		await points.getByRole("textbox").nth(1).fill("Warranty");
		await points.getByRole("button", { name: "Move item 2 up", exact: true }).click();
		await drawer.getByRole("button", { name: "Apply", exact: true }).click();
		const saved = await save(page, original.id);
		expect(saved[field].root.children[0].fields.points).toEqual(["Warranty", "Oak"]);
		expect(saved[field].root.children[0].fields.sizes).toEqual([0, 10]);
		await page.reload();
		await card.getByRole("button", { name: "Edit", exact: true }).click();
		await expect(points.getByRole("textbox").nth(0)).toHaveValue("Warranty");
		await expect(sizes.getByRole("textbox").nth(0)).toHaveValue("0");
		expect(errors.pageErrors).toEqual([]);
	});

test("authoritative list constraint issues carry one field target and clear after item reorder", async ({
	page,
}) => {
	await loginAsEditor(page);
	const original = await create(page, { serverPoints: ["valid", "other"] });
	await page.goto(`/admin/collections/${collection}/${original.id}`);
	await item(page, "serverPoints", 0).fill("expand");
	const response = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/${collection}/${original.id}`
	);
	await documentSaveButton(page).click();
	const rejected = await response;
	expect(rejected.status()).toBe(422);
	const issue = (await rejected.json()).error.issues.find(
		(issue: { path: string }) => issue.path === "serverPoints"
	);
	expect(issue).toMatchObject({
		path: "serverPoints",
		fieldId: expect.any(String),
		collectionId: collection,
		target: expect.any(String),
	});
	expect(issue.message).toMatch(/[Ii]tem 1/);
	await expect(item(page, "serverPoints", 0)).toHaveAttribute("aria-invalid", "true");
	await list(page, "serverPoints")
		.getByRole("button", { name: "Move item 1 down", exact: true })
		.click();
	await expect(item(page, "serverPoints", 1)).toHaveValue("expand");
	await expect(item(page, "serverPoints", 1)).not.toHaveAttribute("aria-invalid", "true");
	await save(page, original.id);
});
