import { chromium, expect } from "@playwright/test";
const origin = "http://127.0.0.1:3417";
const browser = await chromium.launch({ headless: true, channel: "chrome" });
const page = await browser.newPage();
page.setDefaultTimeout(10000);
const observations: unknown[] = [];
const clear = () => page.request.delete(`${origin}/evidence`);
async function record(name: string, extra: Record<string, unknown> = {}) {
	const events = await (await page.request.get(`${origin}/evidence`)).json();
	observations.push({ name, ...extra, events, text: await page.locator("body").innerText() });
	await Bun.write(".evidence/context-observations.json", JSON.stringify(observations, null, 2));
	console.log(
		name,
		JSON.stringify(
			events
				.filter((event: Record<string, unknown>) => event.phase === "start")
				.map((event: Record<string, unknown>) => ({
					label: event.label,
					value: event.value,
					previousValue: event.previousValue,
					event: event.event,
					locale: event.locale,
				}))
		)
	);
}
try {
	await clear();
	await page.goto(`${origin}/admin/collections/products/1?locale=fr`);
	await expect(page.locator('input[name="translation"]')).toBeVisible();
	await page.waitForTimeout(1000);
	await record("French initial load", {
		translationInput: await page.locator('input[name="translation"]').inputValue(),
	});
	await clear();
	await page.locator('input[name="translation"]').fill("invalid");
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await page.waitForTimeout(1000);
	await record("French failed save");
	await clear();
	await page.locator('input[name="translation"]').fill("valid");
	await page.waitForTimeout(1000);
	await record("French advisory after failure");
	await clear();
	const malformed = await page.request.patch(`${origin}/api/products/1?locale=en`, {
		data: { sku: { forged: true } },
	});
	await record("malformed text object REST", {
		status: malformed.status(),
		response: await malformed.json(),
	});
	await clear();
	const missing = await page.request.post(`${origin}/api/products?locale=en`, {
		data: { supplier: "acme" },
	});
	await record("missing text REST create", {
		status: missing.status(),
		response: await missing.json(),
	});
	await clear();
	const emptyNumber = await page.request.patch(`${origin}/api/products/1?locale=en`, {
		data: { sku: "invalid", quantity: "not-a-number" },
	});
	await record("malformed number REST", {
		status: emptyNumber.status(),
		response: await emptyNumber.json(),
	});
} finally {
	await browser.close();
}
