import { chromium, expect } from "@playwright/test";

const origin = "http://127.0.0.1:3417";
const browser = await chromium.launch({ headless: true, channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1440, height: 1100 } });
page.setDefaultTimeout(10000);
const observations: unknown[] = [];
const consoleErrors: string[] = [];
page.on("pageerror", (error) => consoleErrors.push(error.message));
const requests: unknown[] = [];
page.on("request", (req) => {
	if (req.method() === "POST")
		requests.push({ at: Date.now(), url: req.url(), body: req.postData()?.slice(0, 160) });
});
async function clear() {
	await page.request.delete(`${origin}/evidence`);
}
async function record(name: string) {
	const events = await (await page.request.get(`${origin}/evidence`)).json();
	const entry = {
		name,
		events,
		at: Date.now(),
		text: await page.locator("body").innerText(),
		inputs: await page.locator("input").evaluateAll((nodes) =>
			nodes.map((node) => ({
				name: (node as HTMLInputElement).name,
				value: (node as HTMLInputElement).value,
				disabled: (node as HTMLInputElement).disabled,
				invalid: node.getAttribute("aria-invalid"),
			}))
		),
	};
	observations.push(entry);
	console.log(
		name,
		JSON.stringify(
			events.map((e: Record<string, unknown>) => ({
				phase: e.phase,
				label: e.label,
				event: e.event,
				value: e.value,
				result: e.result,
				locale: e.locale,
			}))
		)
	);
	await Bun.write(
		".evidence/browser-observations.json",
		JSON.stringify({ observations, requests, consoleErrors }, null, 2)
	);
	return events as Record<string, unknown>[];
}
async function settle(ms = 1000) {
	await page.waitForTimeout(ms);
}
try {
	await clear();
	await page.goto(`${origin}/admin/collections/products/1`);
	await expect(page.getByLabel("SKU", { exact: true })).toHaveValue("persisted");
	await settle();
	await record("initial document load");
	await clear();
	await page.getByLabel("SKU", { exact: true }).fill("invalid");
	await settle();
	await record("typing invalid SKU before submit");
	await page.getByLabel("Supplier", { exact: true }).focus();
	await settle();
	await record("blur invalid SKU before submit");
	await clear();
	await page.getByLabel("Supplier", { exact: true }).fill("blocked");
	await settle();
	await record("sibling supplier change before submit");
	await clear();
	await page.locator('input[name="code"]').fill("invalid");
	await settle();
	await record("embedded typing before parent submit");
	await page.locator('input[name="code"]').fill("valid");
	await settle();
	await clear();
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await settle();
	await expect(
		page.getByText("SKU: unavailable for supplier blocked", { exact: true })
	).toBeVisible();
	await record("failed document submission");
	await clear();
	await page.getByLabel("SKU", { exact: true }).fill("valid");
	await settle();
	await record("typing valid SKU while supplier remains blocked after submit");
	await clear();
	await page.getByLabel("Supplier", { exact: true }).fill("acme");
	await settle();
	await expect(
		page.getByText("SKU: unavailable for supplier blocked", { exact: true })
	).toHaveCount(0);
	await record("sibling supplier correction after submit");
	await clear();
	await page.getByLabel("SEO title", { exact: true }).fill("invalid");
	await page.getByLabel("Variant URL", { exact: true }).fill("invalid");
	await page.getByLabel("Block URL", { exact: true }).fill("invalid");
	await settle();
	await record("group array nested-array block nested-array after submit");
	await page.getByLabel("SEO title", { exact: true }).fill("valid");
	await page.getByLabel("Variant URL", { exact: true }).fill("valid");
	await page.getByLabel("Block URL", { exact: true }).fill("valid");
	await settle();
	await clear();
	await page.getByLabel("SKU", { exact: true }).fill("");
	await settle();
	await record("empty value after submit");
	await clear();
	await page.getByLabel("SKU", { exact: true }).fill("throw");
	await settle();
	await record("callback exception after submit");
	await page.getByLabel("SKU", { exact: true }).fill("valid");
	await settle();
	await clear();
	await page.getByLabel("SKU", { exact: true }).fill("slow-invalid");
	await expect
		.poll(async () =>
			(await (await page.request.get(`${origin}/evidence`)).json()).some(
				(e: Record<string, unknown>) => e.phase === "start" && e.value === "slow-invalid"
			)
		)
		.toBe(true);
	await record("pending slow validation");
	await page.getByLabel("SKU", { exact: true }).fill("fast-good");
	const samples: unknown[] = [];
	for (let i = 0; i < 35; i++) {
		samples.push({
			at: Date.now(),
			value: await page.getByLabel("SKU", { exact: true }).inputValue(),
			messages: await page.locator(".field-error").allTextContents(),
		});
		await settle(70);
	}
	observations.push({ name: "slow-old then fast-new UI samples", samples });
	await record("late response result");
	await page.screenshot({ path: ".evidence/final.png", fullPage: true });
} finally {
	await Bun.write(
		".evidence/browser-observations.json",
		JSON.stringify({ observations, requests, consoleErrors }, null, 2)
	);
	await browser.close();
}
