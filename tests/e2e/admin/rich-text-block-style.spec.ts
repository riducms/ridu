import type { SchemaManifest } from "@riducms/protocol";
import { bodyEditor, insertBlock, bodyCards, block, richDocument } from "./rich-text-block-fixture";
import { expect, test } from "./fixture";
import { loginAsEditor, useAdminRuntimeFallback } from "./helpers";

test("embedded block groups use inset layout without group clipboard actions", async ({ page }) => {
	await loginAsEditor(page);
	await page.goto("/admin/collections/block-articles/create");
	const card = await insertBlock(page, bodyEditor(page), "Callout");
	const group = card.locator(".ridu-group-field").filter({ hasText: "Appearance" });
	await expect(group).toBeVisible();
	await expect(group).toHaveCSS("margin-left", "-20px");
	await expect(group).toHaveCSS("padding-left", "20px");
	await expect(group).toHaveCSS("border-top-width", "1px");
	await expect(group.locator(".ridu-group-field__header")).toHaveCSS("border-bottom-width", "0px");
	await expect(group.locator(".ridu-group-field__title")).toHaveCSS("font-size", "20px");
	await expect(group.getByRole("button", { name: "Open Appearance actions" })).toHaveCount(0);
	await expect(card.getByRole("button", { name: "Open Links actions" })).toBeVisible();
});

for (const theme of ["light", "dark"] as const) {
	test(`block header, name and disclosure match Payload geometry in ${theme}`, async ({ page }) => {
		await loginAsEditor(page);
		const preference = await page.request.put("/api/preferences/theme", { data: { value: theme } });
		expect(preference.ok()).toBe(true);
		await page.evaluate((theme) => localStorage.setItem("ridu:theme", theme), theme);
		await page.emulateMedia({ reducedMotion: "reduce" });
		await page.goto("/admin/collections/block-articles/create");
		const card = await insertBlock(page, bodyEditor(page), "CTA");
		const header = card.locator(".ridu-richtext-block__header");
		const name = card.getByRole("textbox", { name: "Block name", exact: true });
		const label = card.locator(".ridu-richtext-block__label");
		const content = card.locator(".ridu-richtext-block__content");
		await expect(card.getByRole("button", { name: "Duplicate", exact: true })).toHaveCount(0);
		await expect(header).toHaveCSS("height", "40px");
		await expect(header).toHaveCSS("padding-left", "8px");
		await expect(label).toHaveCSS("font-size", "13px");
		await expect(label).toHaveCSS("letter-spacing", "0.32px");
		await expect(label).toHaveCSS("height", "24px");
		await expect(name).toHaveCSS("font-size", "12.5px");
		await page.mouse.move(0, 0);
		await expect(card).toHaveCSS(
			"border-top-color",
			theme === "dark" ? "rgb(74, 74, 74)" : "rgb(208, 208, 208)"
		);

		await card.getByRole("button", { name: "Select CTA block" }).press("Tab");
		await expect(name).toBeFocused();
		await expect(name).not.toHaveCSS("box-shadow", "none");
		await name.fill(
			"A long block name that must fit a narrow editor without pushing its controls away"
		);
		await card.getByRole("textbox", { name: /CTA label/ }).fill("Retained field input");
		for (const width of [1280, 390]) {
			await page.setViewportSize({ width, height: 844 });
			expect(await card.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(
				true
			);
			await expect(card.getByRole("button", { name: "Remove", exact: true })).toBeVisible();
			await card.getByRole("button", { name: "Collapse CTA", exact: true }).click();
			await expect(content).toHaveAttribute("inert", "");
			await expect(content).toHaveCSS("height", "0px");
			await expect(card).toHaveCSS("height", "42px");
			await card.getByRole("button", { name: "Expand CTA", exact: true }).press("Enter");
			await expect(content).not.toHaveAttribute("inert", "");
			await expect(card.getByRole("textbox", { name: /CTA label/ })).toHaveValue(
				"Retained field input"
			);
			await expect(name).toHaveValue(
				"A long block name that must fit a narrow editor without pushing its controls away"
			);
		}
	});
}

test("long configured block labels keep header controls reachable on narrow screens", async ({
	page,
}) => {
	await loginAsEditor(page);
	const created = await page.request.post("/api/collections/block-articles?draft=true", {
		data: { title: "Long block label", body: richDocument([block("cta", { label: "CTA text" })]) },
	});
	expect(created.ok(), await created.text()).toBe(true);
	const document = (await created.json()).doc;
	await useAdminRuntimeFallback(page);
	const longLabel =
		"A very long configured block type label that exceeds the width of a mobile editor";
	await page.route("**/api/schema", async (route) => {
		const response = await route.fetch();
		const body: { schema: SchemaManifest } = await response.json();
		let changed = 0;
		// Change only the presentation label in this otherwise real manifest.
		const visit = (value: unknown) => {
			if (value === null || typeof value !== "object") return;
			const record = value as Record<string, unknown>;
			if (record.slug === "cta" && record.labels && typeof record.labels === "object") {
				(record.labels as Record<string, unknown>).singular = longLabel;
				changed++;
			}
			for (const child of Object.values(record)) visit(child);
		};
		visit(body.schema);
		expect(changed).toBeGreaterThan(0);
		await route.fulfill({ response, json: body });
	});
	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto(`/admin/collections/block-articles/${document.id}`);
	const card = bodyCards(page).first();
	await expect(card.locator(".ridu-richtext-block__label")).toHaveText(longLabel);
	expect(await card.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
	const remove = card.getByRole("button", { name: "Remove", exact: true });
	const toggle = card.getByRole("button", { name: `Collapse ${longLabel}`, exact: true });
	for (const button of [remove, toggle]) {
		const cardBox = await card.boundingBox();
		const box = await button.boundingBox();
		expect(box!.x).toBeGreaterThanOrEqual(cardBox!.x);
		expect(box!.x + box!.width).toBeLessThanOrEqual(cardBox!.x + cardBox!.width);
	}
	await toggle.click();
	await expect(card).toHaveAttribute("data-collapsed", "true");
	await card.getByRole("button", { name: `Expand ${longLabel}`, exact: true }).press("Enter");
	await expect(card.getByRole("textbox", { name: /CTA label/ })).toHaveValue("CTA text");
});
