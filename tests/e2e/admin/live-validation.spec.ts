import type { LiveValidationEnvelope } from "@riducms/protocol";
import { expect, test, type Locator, type Page } from "./fixture";
import {
	chooseContentLocale,
	documentSaveButton,
	loginAsEditor,
	observePageErrors,
} from "./helpers";

function nextCheck(page: Page) {
	return page.waitForResponse(
		(response) =>
			response.request().method() === "POST" &&
			new URL(response.url()).pathname === "/api/collections/live-validation/validate"
	);
}

async function expectInvalidBorder(control: Locator) {
	await expect
		.poll(() =>
			control.evaluate((element) => {
				const probe = element.cloneNode(false) as HTMLElement;
				const neutral = element.cloneNode(false) as HTMLElement;
				for (const candidate of [probe, neutral]) {
					candidate.removeAttribute("disabled");
					candidate.removeAttribute("readonly");
					candidate.style.position = "fixed";
					candidate.style.inset = "-100px auto auto -100px";
					document.body.append(candidate);
				}
				neutral.setAttribute("aria-invalid", "false");
				const expected = getComputedStyle(probe).borderColor;
				const neutralColor = getComputedStyle(neutral).borderColor;
				probe.remove();
				neutral.remove();
				const styles = getComputedStyle(element);
				const actual = styles.borderColor;
				const focusVisible = element.matches(":focus-visible");
				const focusOutlined = styles.outlineStyle !== "none" && styles.outlineWidth !== "0px";
				const focusTreatment = !focusVisible || (!focusOutlined && styles.boxShadow !== "none");
				return actual === expected && expected !== neutralColor && focusTreatment
					? "match"
					: `actual=${actual}; invalid=${expected}; neutral=${neutralColor}; readonly=${element.hasAttribute("readonly")}; disabled=${element.matches(":disabled")}; focus=${focusVisible}; outline=${styles.outlineStyle} ${styles.outlineWidth}; shadow=${styles.boxShadow}`;
			})
		)
		.toBe("match");
}

const customEditorDescription = "Use the selected supplier prefix in this custom editor.";
const customEditorIssue = "SKU must start with G- for the selected supplier";
const richTextPrompt = "Write body copy, or type / for commands…";

async function fieldMessageIDs(control: Locator) {
	const controlID = await control.getAttribute("id");
	if (controlID === null) throw new Error("field control has no ID");
	return {
		description: `${controlID}-description`,
		error: `${controlID}-error`,
	};
}

async function expectInvalidMessages(
	page: Page,
	control: Locator,
	ids: Awaited<ReturnType<typeof fieldMessageIDs>>
) {
	await expect(control).toHaveAttribute("aria-invalid", "true");
	await expect(control).toHaveAttribute("aria-describedby", `${ids.description} ${ids.error}`);
	await expect(control).toHaveAttribute("aria-errormessage", ids.error);
	await expect(page.locator(`[id="${ids.description}"]`)).toHaveText(customEditorDescription);
	const error = page.locator(`[id="${ids.error}"]`);
	await expect(error).toHaveText(customEditorIssue);
	await expect(error).toBeVisible();
	await expect(error).toHaveCSS("position", "absolute");
	await expect(error).toHaveCSS("pointer-events", "none");
	const [controlBounds, errorBounds] = await Promise.all([
		control.boundingBox(),
		error.boundingBox(),
	]);
	if (controlBounds === null || errorBounds === null)
		throw new Error("field control or error callout has no layout box");
	expect(errorBounds.y + errorBounds.height).toBeLessThanOrEqual(controlBounds.y);
	expect(
		Math.abs(errorBounds.x + errorBounds.width - (controlBounds.x + controlBounds.width))
	).toBeLessThanOrEqual(1);
}

async function expectDescriptionOnly(
	page: Page,
	control: Locator,
	ids: Awaited<ReturnType<typeof fieldMessageIDs>>
) {
	await expect(control).not.toHaveAttribute("aria-invalid", "true");
	await expect(control).toHaveAttribute("aria-describedby", ids.description);
	await expect(control).not.toHaveAttribute("aria-errormessage");
	await expect(page.locator(`[id="${ids.description}"]`)).toHaveText(customEditorDescription);
	await expect(page.locator(`[id="${ids.error}"]`)).toHaveCount(0);
}

async function createProduct(page: Page, data: Record<string, unknown> = {}) {
	const created = await page.request.post("/api/collections/live-validation?locale=en", {
		data: { title: "Live product", supplier: "acme", sku: "A-123", ...data },
	});
	expect(created.ok(), await created.text()).toBe(true);
	return (await created.json()).doc;
}

test("checkbox feedback uses the same anchored callout without replacing its description", async ({
	page,
}) => {
	await loginAsEditor(page);
	const product = await createProduct(page);
	await page.goto(`/admin/collections/live-validation/${product.id}`);
	const checkbox = page.getByRole("checkbox", { name: "Confirmed", exact: true });
	const ids = await fieldMessageIDs(checkbox);
	await expect(checkbox).toBeChecked();
	await expect(page.locator(`[id="${ids.description}"]`)).toHaveText(
		"Confirms that this document is ready for review."
	);
	const check = nextCheck(page);
	await checkbox.click();
	expect((await check).ok()).toBe(true);
	await expect(checkbox).toHaveAttribute("aria-invalid", "true");
	await expect(checkbox).toHaveAttribute("aria-describedby", `${ids.description} ${ids.error}`);
	await expect(checkbox).toHaveAttribute("aria-errormessage", ids.error);
	const error = page.locator(`[id="${ids.error}"]`);
	await expect(error).toHaveText("Confirm this document before saving");
	await expect(error).toHaveCSS("position", "absolute");
	await expect(error).toHaveCSS("pointer-events", "none");
	await expect(page.locator(`[id="${ids.description}"]`)).toHaveText(
		"Confirms that this document is ready for review."
	);
});

async function rowAction(page: Page, field: string, row: number, action: string) {
	await page
		.locator(`[data-field-path="${field}"]`)
		.first()
		.getByRole("button", { name: `Open Row ${row + 1} actions`, exact: true })
		.first()
		.click();
	await page.getByRole("menuitem", { name: action, exact: true }).click();
}

test("supplier-dependent live feedback reaches ordinary and configured editors without authorizing Save", async ({
	page,
}) => {
	await loginAsEditor(page);
	const errors = observePageErrors(page);
	const product = await createProduct(page, { customSKU: "A-custom" });
	await page.goto(`/admin/collections/live-validation/${product.id}`);
	const sku = page.locator('input[name="sku"]');
	const custom = page.locator('input[name="customSKU"]');
	await expect(custom).toHaveAttribute("data-local-editor", "text");
	const customMessages = await fieldMessageIDs(custom);
	await expectDescriptionOnly(page, custom, customMessages);
	const body = page.locator('[data-field-path="body"]');
	const bodyEditor = body.locator(".ridu-richtext-content");
	const bodyFooter = body.getByRole("button", { name: /^Add block —/ });
	await expect(bodyEditor).toHaveAttribute("contenteditable", "true");
	await expect(bodyFooter).toBeEnabled();
	await expect(body.getByText(richTextPrompt, { exact: true })).toBeVisible();
	await expect(body.getByText("No content", { exact: true })).toHaveCount(0);
	let check = nextCheck(page);
	await sku.fill("wrong");
	expect((await check).ok()).toBe(true);
	await expect(sku).toHaveAttribute("aria-invalid", "true");
	await expect(
		page.getByText("SKU must start with A- for the selected supplier", { exact: true })
	).toBeVisible();
	check = nextCheck(page);
	await sku.fill("A-corrected");
	await check;
	await expect(sku).not.toHaveAttribute("aria-invalid", "true");
	check = nextCheck(page);
	await page.locator('input[name="supplier"]').fill("globex");
	await check;
	await expect(sku).toHaveAttribute("aria-invalid", "true");
	await expect(
		page.getByText("SKU must start with G- for the selected supplier", { exact: true })
	).toBeVisible();
	check = nextCheck(page);
	await custom.fill("wrong");
	await check;
	await expectInvalidMessages(page, custom, customMessages);
	await custom.blur();
	await expectInvalidBorder(custom);
	check = nextCheck(page);
	await sku.fill("G-corrected");
	await check;
	await expect(sku).not.toHaveAttribute("aria-invalid", "true");
	let saveStarted!: () => void;
	let releaseSave!: () => void;
	const pendingSave = new Promise<void>((resolve) => {
		saveStarted = resolve;
	});
	const continueSave = new Promise<void>((resolve) => {
		releaseSave = resolve;
	});
	await page.route(`**/api/collections/live-validation/${product.id}*`, async (route) => {
		if (
			route.request().method() === "PATCH" &&
			new URL(route.request().url()).pathname === `/api/collections/live-validation/${product.id}`
		) {
			saveStarted();
			await continueSave;
		}
		await route.continue();
	});
	const save = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === `/api/collections/live-validation/${product.id}`
	);
	const saveButton = documentSaveButton(page);
	await expect(saveButton).toHaveText("Save");
	const saveClick = saveButton.click();
	await pendingSave;
	try {
		await expect(saveButton).toHaveText("Save");
		await expect(saveButton).toHaveAttribute("aria-busy", "true");
		await expect(saveButton).toBeDisabled();
		await expect(
			page.locator('[data-field-path="customSKU"]').getByText("Read only", { exact: true })
		).toHaveCount(0);
		await expect(custom).toBeDisabled();
		await expectInvalidMessages(page, custom, customMessages);
		await expectInvalidBorder(custom);
		await expect(bodyEditor).toHaveAttribute("contenteditable", "false");
		await expect(bodyFooter).toBeVisible();
		await expect(bodyFooter).toBeDisabled();
		await expect(body.getByText(richTextPrompt, { exact: true })).toBeVisible();
		await expect(body.getByText("No content", { exact: true })).toHaveCount(0);
	} finally {
		releaseSave();
	}
	await saveClick;
	expect((await save).status()).toBe(422);
	await expect(custom).toBeFocused();
	await expectInvalidMessages(page, custom, customMessages);
	await expectInvalidBorder(custom);
	await expect(bodyEditor).toHaveAttribute("contenteditable", "true");
	await expect(bodyFooter).toBeEnabled();
	await expect(body.getByText(richTextPrompt, { exact: true })).toBeVisible();
	await expect(body.getByText("No content", { exact: true })).toHaveCount(0);
	check = nextCheck(page);
	await custom.fill("G-custom");
	await check;
	await expectDescriptionOnly(page, custom, customMessages);
	const persisted = (
		await (await page.request.get(`/api/collections/live-validation/${product.id}`)).json()
	).doc;
	expect(persisted.supplier).toBe("acme");
	expect(persisted.sku).toBe("A-123");
	expect(errors.pageErrors).toEqual([]);
});

test("live checks expose pending, network failure and retry without leaking callback failures", async ({
	page,
}) => {
	await loginAsEditor(page);
	const product = await createProduct(page);
	await page.goto(`/admin/collections/live-validation/${product.id}`);
	const sku = page.locator('input[name="sku"]');
	const feedback = page.locator('[data-live-validation-path="sku"]');
	let release!: () => void;
	const pending = new Promise<void>((resolve) => {
		release = resolve;
	});
	let first = true;
	await page.route("**/api/collections/live-validation/validate*", async (route) => {
		if (!first) {
			await route.continue();
			return;
		}
		first = false;
		await pending;
		await route.abort("failed");
	});
	await sku.fill("A-checked");
	await expect(feedback).toHaveAttribute("data-live-validation", "pending");
	await expect(feedback.getByText("Checking…", { exact: true })).toBeVisible();
	release();
	await expect(feedback).toHaveAttribute("data-live-validation", "failed");
	await expect(
		feedback.getByText("This check could not be completed.", { exact: true })
	).toBeVisible();
	let check = nextCheck(page);
	await feedback.getByRole("button", { name: "Retry check", exact: true }).click();
	expect((await check).ok()).toBe(true);
	await expect(feedback).toHaveAttribute("data-live-validation", "checked");
	await expect(sku).not.toHaveAttribute("aria-invalid", "true");
	check = nextCheck(page);
	await sku.fill("server-error");
	expect((await check).status()).toBe(500);
	await expect(feedback).toHaveAttribute("data-live-validation", "failed");
	await expect(page.getByText(/private supplier service credential/)).toHaveCount(0);
});

for (const field of ["sections", "content"] as const)
	test(`live ${field} aggregate issues keep nested row identity through reorder and deletion`, async ({
		page,
	}) => {
		await loginAsEditor(page);
		const product = await createProduct(page, {
			[field]: [
				{
					...(field === "content" ? { blockType: "card" } : {}),
					supplier: "acme",
					sku: "A-first",
					links: [{ url: "Original URL" }, { url: "Other URL" }],
				},
				{
					...(field === "content" ? { blockType: "note" } : {}),
					supplier: "globex",
					sku: "G-other",
				},
			],
		});
		await page.goto(`/admin/collections/live-validation/${product.id}`);
		let check = nextCheck(page);
		await page.locator(`input[name="${field}.0.links.0.url"]`).fill("invalid");
		const feedback: LiveValidationEnvelope = await (await check).json();
		const issue = feedback.evaluations
			.flatMap((evaluation) => evaluation.issues)
			.find((entry) => entry.code === "url")!;
		expect(JSON.parse(issue.target!)).toEqual(
			expect.arrayContaining([product[field][0]._key, product[field][0].links[0]._key])
		);
		await expect(page.locator(`input[name="${field}.0.links.0.url"]`)).toHaveAttribute(
			"aria-invalid",
			"true"
		);
		check = nextCheck(page);
		await rowAction(page, `${field}.0.links`, 0, "Move down");
		await check;
		check = nextCheck(page);
		await rowAction(page, field, 0, "Move down");
		await check;
		await expect(page.locator(`input[name="${field}.1.links.1.url"]`)).toHaveValue("invalid");
		await expect(page.locator(`input[name="${field}.1.links.1.url"]`)).toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await expect(page.locator(`input[name="${field}.1.links.0.url"]`)).not.toHaveAttribute(
			"aria-invalid",
			"true"
		);
		check = nextCheck(page);
		await rowAction(page, `${field}.1.links`, 1, "Remove");
		await check;
		await expect(page.locator(`input[name="${field}.1.links.0.url"]`)).not.toHaveAttribute(
			"aria-invalid",
			"true"
		);
		await expect(page.locator(`input[name="${field}.1.links.1.url"]`)).toHaveCount(0);
	});

test("live localized feedback uses the edited exact locale and expires on locale changes", async ({
	page,
}) => {
	await loginAsEditor(page);
	const product = await createProduct(page, { localizedSKU: "A-English" });
	await page.goto(`/admin/collections/live-validation/${product.id}`);
	await chooseContentLocale(page, "French", "fr");
	const input = page.locator('input[name="localizedSKU"]');
	await expect(input).toHaveValue("A-English");
	const check = nextCheck(page);
	await input.fill("G-French");
	const feedback: LiveValidationEnvelope = await (await check).json();
	expect(feedback.evaluations.flatMap((evaluation) => evaluation.issues)).toEqual(
		expect.arrayContaining([
			expect.objectContaining({ path: "localizedSKU", locale: "fr", code: "supplier_sku" }),
		])
	);
	await expect(input).toHaveAttribute("aria-invalid", "true");
	// The existing editor requires saving or undoing changes before switching
	// locale. Undo this translation without turning its fallback into stored input.
	await expect(page.getByLabel("Content locale", { exact: true })).toBeDisabled();
	await input.fill("A-English");
	await expect(page.getByLabel("Content locale", { exact: true })).toBeEnabled();
	await chooseContentLocale(page, "English", "en");
	await expect(input).toHaveValue("A-English");
	await expect(input).not.toHaveAttribute("aria-invalid", "true");
});

test("packaged generic embedded fields receive live feedback through the standard host", async ({
	page,
}) => {
	await loginAsEditor(page);
	const product = await createProduct(page, {
		outline: {
			outline: [
				{
					kind: "widget",
					content: { schema: "card", uid: "card-a", supplier: "acme", sku: "A-first" },
				},
				{
					kind: "widget",
					content: { schema: "card", uid: "card-b", supplier: "globex", sku: "G-second" },
				},
			],
		},
	});
	await page.goto(`/admin/collections/live-validation/${product.id}`);
	const first = page.locator('[data-outline-key="card-a"] input[name$=".sku"]');
	const second = page.locator('[data-outline-key="card-b"] input[name$=".sku"]');
	let check = nextCheck(page);
	await first.fill("wrong");
	expect((await check).ok()).toBe(true);
	await expect(first).toHaveAttribute("aria-invalid", "true");
	await expect(second).not.toHaveAttribute("aria-invalid", "true");
	check = nextCheck(page);
	await page.getByRole("button", { name: "Reverse outline cards", exact: true }).click();
	await check;
	await expect(first).toHaveValue("wrong");
	await expect(first).toHaveAttribute("aria-invalid", "true");
	await expect(second).not.toHaveAttribute("aria-invalid", "true");
});

test("global ordinary and detached fields use the global live-validation transport", async ({
	page,
}) => {
	await loginAsEditor(page);
	const saved = await page.request.patch("/api/globals/validation-settings", {
		data: {
			supplier: "acme",
			sku: "A-global",
			body: {
				version: 1,
				root: {
					type: "root",
					version: 1,
					children: [
						{
							type: "block",
							version: 1,
							fields: { blockType: "card", supplier: "acme", sku: "A-card" },
						},
					],
				},
			},
		},
	});
	expect(saved.ok(), await saved.text()).toBe(true);
	await page.goto("/admin/globals/validation-settings");
	const nextGlobalCheck = () =>
		page.waitForResponse(
			(response) =>
				response.request().method() === "POST" &&
				new URL(response.url()).pathname === "/api/globals/validation-settings/validate"
		);
	let check = nextGlobalCheck();
	await page.locator('input[name="sku"]').fill("wrong");
	let response = await check;
	expect(response.ok(), await response.text()).toBe(true);
	expect(response.request().postDataJSON().id).toBeUndefined();
	const feedback: LiveValidationEnvelope = await response.json();
	expect(feedback.evaluations[0]?.issues[0]).toMatchObject({
		globalId: "global-validation-settings",
		path: "sku",
	});
	await expect(page.locator('input[name="sku"]')).toHaveAttribute("aria-invalid", "true");
	const card = page.locator('[data-field-path="body"] .ridu-richtext-embedded-card').first();
	await card.getByRole("button", { name: "Edit", exact: true }).click();
	const drawer = page.getByRole("dialog", { name: "Edit Card", exact: true });
	check = nextGlobalCheck();
	await drawer.locator('input[name$=".sku"]').fill("wrong");
	response = await check;
	expect(response.ok(), await response.text()).toBe(true);
	expect(response.request().postDataJSON().id).toBeUndefined();
	expect(response.request().postDataJSON().embedded).toHaveLength(1);
	await expect(drawer.locator('input[name$=".sku"]')).toHaveAttribute("aria-invalid", "true");
	await drawer.getByRole("button", { name: "Cancel", exact: true }).click();
	const write = page.waitForResponse(
		(response) =>
			response.request().method() === "PATCH" &&
			new URL(response.url()).pathname === "/api/globals/validation-settings"
	);
	await documentSaveButton(page).click();
	expect((await write).status()).toBe(422);
});

for (const field of ["body", "localizedBody"] as const)
	test(`live ${field} embedded feedback precedes Apply, cancels cleanly and leaves parent Save authoritative`, async ({
		page,
	}) => {
		await loginAsEditor(page);
		const product = await createProduct(page, {
			[field]: {
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
								sku: "A-original",
								links: [{ url: "Original URL" }],
							},
						},
					],
				},
			},
		});
		await page.goto(`/admin/collections/live-validation/${product.id}`);
		if (field === "localizedBody") await chooseContentLocale(page, "French", "fr");
		const card = page.locator(`[data-field-path="${field}"] .ridu-richtext-embedded-card`).first();
		await card.getByRole("button", { name: "Edit", exact: true }).click();
		const drawer = page.getByRole("dialog", { name: "Edit Card", exact: true });
		const url = drawer.locator('input[name$=".links.0.url"]');
		let check = nextCheck(page);
		await url.fill("invalid");
		expect((await check).ok()).toBe(true);
		await expect(url).toHaveAttribute("aria-invalid", "true");
		await drawer.getByRole("button", { name: "Cancel", exact: true }).click();
		await expect(drawer).toHaveCount(0);
		await card.getByRole("button", { name: "Edit", exact: true }).click();
		await expect(url).toHaveValue("Original URL");
		await expect(url).not.toHaveAttribute("aria-invalid", "true");
		check = nextCheck(page);
		await url.fill("invalid");
		await check;
		await expect(url).toHaveAttribute("aria-invalid", "true");
		await drawer.getByRole("button", { name: "Apply", exact: true }).click();
		await expect(drawer).toHaveCount(0);
		const save = page.waitForResponse(
			(response) =>
				response.request().method() === "PATCH" &&
				new URL(response.url()).pathname === `/api/collections/live-validation/${product.id}`
		);
		await documentSaveButton(page).click();
		expect((await save).status()).toBe(422);
		if (!(await drawer.isVisible()))
			await card.getByRole("button", { name: "Edit", exact: true }).click();
		await expect(url).toHaveAttribute("aria-invalid", "true");
	});
