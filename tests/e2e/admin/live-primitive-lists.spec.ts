import type { LiveValidationEnvelope } from "@riducms/protocol";
import { expect, test, type Page } from "./fixture";
import {
	chooseContentLocale,
	documentSaveButton,
	loginAsEditor,
	observePageErrors,
} from "./helpers";

const collection = "live-validation";
const list = (page: Page, path: string) => page.locator(`[data-field-path="${path}"]`).first();
const item = (page: Page, path: string, index = 0) =>
	list(page, path).getByRole("textbox").nth(index);
const feedback = (page: Page, path: string) =>
	page.locator(`[data-live-validation-path="${path}"]`);

function nextCheck(page: Page, path?: string) {
	return page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === `/api/collections/${collection}/validate` &&
			(!path || response.request().postDataJSON().fields.includes(path))
	);
}

async function create(page: Page, data: Record<string, unknown>) {
	const response = await page.request.post(`/api/collections/${collection}?locale=en`, {
		data: { title: "Live primitive product", supplier: "acme", ...data },
	});
	expect(response.ok(), await response.text()).toBe(true);
	return (await response.json()).doc;
}

async function rowAction(page: Page, path: string, row: number, action: string) {
	await list(page, path)
		.getByRole("button", { name: `Open Row ${row + 1} actions`, exact: true })
		.first()
		.click();
	await page.getByRole("menuitem", { name: action, exact: true }).click();
}

test("ordinary and custom list checks refresh for siblings while Save validates independently", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const product = await create(page, {
		supplierCodes: ["A-first", "A-first"],
		packSizes: [0, 20],
		customCodes: ["A-custom"],
	});
	await page.goto(`/admin/collections/${collection}/${product.id}`);
	let check = nextCheck(page, "supplierCodes");
	await item(page, "supplierCodes").fill("wrong");
	const initial: LiveValidationEnvelope = await (await check).json();
	const issue = initial.evaluations.flatMap((entry) => entry.issues)[0]!;
	expect(issue).toMatchObject({ path: "supplierCodes", code: "supplier_codes" });
	// A primitive list has one field target, with no invented per-item identity.
	expect(JSON.parse(issue.target!)).toEqual([issue.fieldId]);
	await expect(list(page, "supplierCodes")).toContainText("Item 1: code must start with A-");
	check = nextCheck(page, "supplierCodes");
	await item(page, "supplierCodes").fill("A-corrected");
	await check;
	await expect(item(page, "supplierCodes")).not.toHaveAttribute("aria-invalid", "true");
	check = nextCheck(page, "packSizes");
	await item(page, "packSizes", 1).fill("30");
	await check;
	check = nextCheck(page, "supplierCodes");
	await page.locator('input[name="supplier"]').fill("globex");
	await check;
	await expect(list(page, "supplierCodes")).toContainText("code must start with G-");
	await expect(list(page, "packSizes")).toContainText("pack size must be between 0 and 10");
	check = nextCheck(page, "customCodes");
	const custom = list(page, "customCodes").locator('[data-local-editor="text-list"]');
	await custom.fill("wrong\nwrong");
	await check;
	await expect(custom).toHaveAttribute("aria-invalid", "true");
	await expect(feedback(page, "customCodes")).toHaveAttribute("data-live-validation", "checked");
	const write = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/${collection}/${product.id}`
	);
	await documentSaveButton(page).click();
	const rejected = await write;
	expect(rejected.status()).toBe(422);
	expect((await rejected.json()).error.issues).toEqual(
		expect.arrayContaining([
			expect.objectContaining({ code: "supplier_codes", path: "supplierCodes" }),
			expect.objectContaining({ code: "supplier_pack_sizes", path: "packSizes" }),
			expect.objectContaining({ code: "supplier_codes", path: "customCodes" }),
		])
	);
	const stored = (
		await (await page.request.get(`/api/collections/${collection}/${product.id}`)).json()
	).doc;
	expect(stored.supplier).toBe("acme");
	expect(stored.supplierCodes).toEqual(["A-first", "A-first"]);
	expect(stored.packSizes).toEqual([0, 20]);
	expect(errors.pageErrors).toEqual([]);
});

test("unfinished numeric list input is skipped without coercion and resumes after correction", async ({
	page,
}) => {
	await loginAsEditor(page);
	const product = await create(page, { packSizes: [0, 8] });
	await page.goto(`/admin/collections/${collection}/${product.id}`);
	const requests: unknown[] = [];
	page.on("request", (request) => {
		if (new URL(request.url()).pathname === `/api/collections/${collection}/validate`)
			requests.push(request.postDataJSON());
	});
	await item(page, "packSizes", 1).fill("1e-");
	await expect(feedback(page, "packSizes")).toHaveAttribute("data-live-validation", "skipped");
	await expect(item(page, "packSizes", 1)).toHaveValue("1e-");
	await item(page, "packSizes", 1).press("Tab");
	expect(requests).toEqual([]);
	let check = nextCheck(page, "packSizes");
	await item(page, "packSizes", 1).fill("1e2");
	const response = await check;
	expect(response.request().postDataJSON().data.packSizes).toEqual([0, 100]);
	const result: LiveValidationEnvelope = await response.json();
	expect(result.evaluations[0]).toMatchObject({ status: "checked", issues: [] });
	check = nextCheck(page, "packSizes");
	await item(page, "packSizes", 1).fill("101");
	await check;
	await expect(list(page, "packSizes")).toContainText(
		"Item 2: pack size must be between 0 and 100"
	);
	check = nextCheck(page, "packSizes");
	await list(page, "packSizes").getByRole("button", { name: "Remove item 2", exact: true }).click();
	await check;
	await expect(item(page, "packSizes")).not.toHaveAttribute("aria-invalid", "true");
});

test("equal duplicate item reorders supersede pending work and old responses cannot replace newer feedback", async ({
	page,
}) => {
	await loginAsEditor(page);
	const product = await create(page, { supplierCodes: ["A-duplicate", "A-duplicate"] });
	await page.goto(`/admin/collections/${collection}/${product.id}`);
	let release!: () => void;
	let captured!: () => void;
	let delivered!: () => void;
	const pending = new Promise<void>((resolve) => {
		release = resolve;
	});
	const firstCaptured = new Promise<void>((resolve) => {
		captured = resolve;
	});
	const firstDelivered = new Promise<void>((resolve) => {
		delivered = resolve;
	});
	let requests = 0;
	await page.route(`**/api/collections/${collection}/validate*`, async (route) => {
		const position = ++requests;
		const response = await route.fetch();
		if (position === 1) {
			captured();
			await pending;
			try {
				await route.fulfill({ response });
			} finally {
				delivered();
			}
		} else {
			await route.fulfill({ response });
		}
	});
	try {
		await item(page, "supplierCodes").fill("wrong");
		await item(page, "supplierCodes", 1).fill("wrong");
		await firstCaptured;
		await expect(feedback(page, "supplierCodes")).toHaveAttribute(
			"data-live-validation",
			"pending"
		);
		let check = nextCheck(page, "supplierCodes");
		await list(page, "supplierCodes")
			.getByRole("button", { name: "Move item 1 down", exact: true })
			.click();
		await check;
		expect(requests).toBe(2);
		await expect(feedback(page, "supplierCodes")).toHaveAttribute(
			"data-live-validation",
			"checked"
		);
		check = nextCheck(page, "supplierCodes");
		await page.locator('input[name="supplier"]').fill("globex");
		await check;
		await expect(list(page, "supplierCodes")).toContainText("code must start with G-");
		release();
		await firstDelivered;
		await expect(list(page, "supplierCodes")).toContainText("code must start with G-");
		await expect(list(page, "supplierCodes")).not.toContainText("code must start with A-");
	} finally {
		release();
	}
});

for (const container of ["sections", "content"] as const)
	test(`live primitive lists inside ${container} and nested arrays retain enclosing identity`, async ({
		page,
	}) => {
		await loginAsEditor(page);
		const product = await create(page, {
			[container]: [
				{
					...(container === "content" ? { blockType: "card" } : {}),
					supplier: "acme",
					supplierCodes: ["A-row"],
					packSizes: [0],
					links: [
						{ supplier: "globex", supplierCodes: ["G-first"], packSizes: [1] },
						{ supplier: "acme", supplierCodes: ["A-other"], packSizes: [0] },
					],
				},
				{
					...(container === "content" ? { blockType: "note" } : {}),
					supplier: "acme",
					supplierCodes: ["A-second"],
				},
			],
		});
		await page.goto(`/admin/collections/${collection}/${product.id}`);
		let check = nextCheck(page, `${container}.0.links.0.supplierCodes`);
		await item(page, `${container}.0.links.0.supplierCodes`).fill("wrong");
		const result: LiveValidationEnvelope = await (await check).json();
		const issue = result.evaluations
			.flatMap((entry) => entry.issues)
			.find((entry) => entry.code === "supplier_codes")!;
		expect(JSON.parse(issue.target!)).toEqual(
			expect.arrayContaining([product[container][0]._key, product[container][0].links[0]._key])
		);
		if (container === "content") expect(JSON.parse(issue.target!)).toContain("card");
		check = nextCheck(page, `${container}.0.links.0.packSizes`);
		await item(page, `${container}.0.links.0.packSizes`).fill("11");
		await check;
		check = nextCheck(page);
		await rowAction(page, `${container}.0.links`, 0, "Move down");
		await check;
		check = nextCheck(page);
		await rowAction(page, container, 0, "Move down");
		await check;
		await expect(item(page, `${container}.1.links.1.supplierCodes`)).toHaveValue("wrong");
		await expect(item(page, `${container}.1.links.1.supplierCodes`)).toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await expect(item(page, `${container}.1.links.1.packSizes`)).toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await expect(item(page, `${container}.1.links.0.supplierCodes`)).not.toHaveAttribute(
			"aria-invalid",
			"true"
		);
		check = nextCheck(page);
		await rowAction(page, `${container}.1.links`, 1, "Remove");
		await check;
		await expect(item(page, `${container}.1.links.0.supplierCodes`)).not.toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await expect(list(page, `${container}.1.links.1.supplierCodes`)).toHaveCount(0);
	});

test("localized list feedback belongs to the exact edited locale and expires when the locale changes", async ({
	page,
}) => {
	await loginAsEditor(page);
	const product = await create(page, { localizedCodes: ["A-English"], localizedSizes: [0, 8] });
	await page.goto(`/admin/collections/${collection}/${product.id}`);
	await chooseContentLocale(page, "French", "fr");
	await expect(item(page, "localizedCodes")).toHaveValue("A-English");
	let check = nextCheck(page, "localizedCodes");
	await item(page, "localizedCodes").fill("wrong");
	let result: LiveValidationEnvelope = await (await check).json();
	expect(result.evaluations.flatMap((entry) => entry.issues)).toContainEqual(
		expect.objectContaining({ path: "localizedCodes", locale: "fr", code: "supplier_codes" })
	);
	check = nextCheck(page, "localizedSizes");
	await item(page, "localizedSizes", 1).fill("101");
	result = await (await check).json();
	expect(result.evaluations.flatMap((entry) => entry.issues)).toContainEqual(
		expect.objectContaining({ path: "localizedSizes", locale: "fr", code: "supplier_pack_sizes" })
	);
	await item(page, "localizedCodes").fill("A-English");
	await item(page, "localizedSizes", 1).fill("8");
	await expect(page.getByLabel("Content locale", { exact: true })).toBeEnabled();
	await chooseContentLocale(page, "English", "en");
	await expect(item(page, "localizedCodes")).toHaveValue("A-English");
	await expect(item(page, "localizedCodes")).not.toHaveAttribute("aria-invalid", "true");
	await expect(item(page, "localizedSizes", 1)).not.toHaveAttribute("aria-invalid", "true");
});

for (const owner of ["body", "localizedBody"] as const)
	test(`${owner} list feedback uses the embedded host across Cancel, Apply and authoritative Save`, async ({
		page,
	}) => {
		await loginAsEditor(page);
		const errors = observePageErrors(page);
		const product = await create(page, {
			[owner]: {
				version: 1,
				root: {
					type: "root",
					version: 1,
					children: [
						{
							type: "block",
							version: 1,
							fields: {
								blockType: "card",
								supplier: "acme",
								supplierCodes: ["A-original", "A-original"],
								packSizes: [0, 8],
								customCodes: ["A-custom"],
							},
						},
					],
				},
			},
		});
		await page.goto(`/admin/collections/${collection}/${product.id}`);
		if (owner === "localizedBody") await chooseContentLocale(page, "French", "fr");
		const card = list(page, owner).locator(".ridu-richtext-embedded-card").first();
		await card.getByRole("button", { name: "Edit", exact: true }).click();
		const drawer = page.getByRole("dialog", { name: "Edit Card", exact: true });
		const codes = drawer.locator('[data-field-path$=".supplierCodes"]').first();
		const sizes = drawer.locator('[data-field-path$=".packSizes"]').first();
		const custom = drawer.locator('[data-local-editor="text-list"]');
		let check = nextCheck(page);
		await codes.getByRole("textbox").first().fill("wrong");
		let response = await check;
		expect(response.request().postDataJSON().embedded).toHaveLength(1);
		const result: LiveValidationEnvelope = await response.json();
		expect(result.evaluations.flatMap((entry) => entry.issues)).toContainEqual(
			expect.objectContaining({
				code: "supplier_codes",
				...(owner === "localizedBody" ? { locale: "fr" } : {}),
			})
		);
		await expect(codes).toContainText("code must start with A-");
		await sizes.getByRole("textbox").nth(1).fill("1e-");
		await expect(drawer.locator('[data-live-validation-path$=".packSizes"]')).toHaveAttribute(
			"data-live-validation",
			"skipped"
		);
		await drawer.getByRole("button", { name: "Apply", exact: true }).click();
		await expect(drawer).toBeVisible();
		await expect(sizes).toContainText("item 2: enter a finite number");
		await drawer.getByRole("button", { name: "Cancel", exact: true }).click();
		await card.getByRole("button", { name: "Edit", exact: true }).click();
		await expect(codes.getByRole("textbox").first()).toHaveValue("A-original");
		await expect(codes.getByRole("textbox").first()).not.toHaveAttribute("aria-invalid", "true");
		await expect(sizes.getByRole("textbox").nth(1)).toHaveValue("8");
		check = nextCheck(page);
		await custom.fill("wrong\nwrong");
		await check;
		await expect(custom).toHaveAttribute("aria-invalid", "true");
		check = nextCheck(page);
		await sizes.getByRole("textbox").nth(1).fill("101");
		await check;
		await expect(sizes).toContainText("pack size must be between 0 and 100");
		await drawer.getByRole("button", { name: "Apply", exact: true }).click();
		await expect(drawer).toHaveCount(0);
		const write = page.waitForResponse(
			(response) =>
				response.request().method() === "PATCH" &&
				new URL(response.url()).pathname === `/api/collections/${collection}/${product.id}`
		);
		await documentSaveButton(page).click();
		const rejected = await write;
		expect(rejected.status()).toBe(422);
		expect((await rejected.json()).error.issues).toEqual(
			expect.arrayContaining([
				expect.objectContaining({ code: "supplier_codes" }),
				expect.objectContaining({ code: "supplier_pack_sizes" }),
			])
		);
		expect(errors.pageErrors).toEqual([]);
	});

test("packaged embedded list editors preserve feedback on their enclosing stable identity", async ({
	page,
}) => {
	await loginAsEditor(page);
	const product = await create(page, {
		outline: {
			outline: [
				{
					kind: "widget",
					content: {
						schema: "card",
						uid: "list-a",
						supplier: "acme",
						supplierCodes: ["A-first"],
						packSizes: [0],
					},
				},
				{
					kind: "widget",
					content: {
						schema: "card",
						uid: "list-b",
						supplier: "globex",
						supplierCodes: ["G-other"],
						packSizes: [1],
					},
				},
			],
		},
	});
	await page.goto(`/admin/collections/${collection}/${product.id}`);
	const first = page
		.locator('[data-outline-key="list-a"] [data-field-path$=".supplierCodes"]')
		.first();
	const second = page
		.locator('[data-outline-key="list-b"] [data-field-path$=".supplierCodes"]')
		.first();
	let check = nextCheck(page);
	await first.getByRole("textbox").fill("wrong");
	await check;
	await expect(first.getByRole("textbox")).toHaveAttribute("aria-invalid", "true");
	check = nextCheck(page);
	await page.getByRole("button", { name: "Reverse outline cards", exact: true }).click();
	await check;
	await expect(first.getByRole("textbox")).toHaveValue("wrong");
	await expect(first.getByRole("textbox")).toHaveAttribute("aria-invalid", "true");
	await expect(second.getByRole("textbox")).not.toHaveAttribute("aria-invalid", "true");
});
