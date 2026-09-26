import type { Locator } from "@playwright/test";
import { expect, test } from "./fixture";
import { chooseRiduSelect, loginAsEditor, observePageErrors } from "./helpers";

// Pause actual CSS animation so intermediate geometry is deterministic on busy CI hosts.
async function captureMotion(panel: Locator) {
	await panel.evaluate((element) => {
		element.addEventListener(
			"animationstart",
			() => {
				for (const animation of element.getAnimations()) {
					animation.pause();
					animation.currentTime = 150;
				}
			},
			{ once: true }
		);
	});
}

async function expectIntermediateHeight(panel: Locator) {
	await expect
		.poll(() =>
			panel.evaluate((element) =>
				element.getAnimations().some((animation) => animation.playState === "paused")
			)
		)
		.toBe(true);
	const dimensions = await panel.evaluate((element) => ({
		height: element.getBoundingClientRect().height,
		fullHeight: Number.parseFloat(
			getComputedStyle(element).getPropertyValue("--bits-collapsible-content-height")
		),
		duration: element.getAnimations()[0]?.effect?.getTiming().duration,
		easing: getComputedStyle(element).animationTimingFunction,
		overflow: getComputedStyle(element).overflow,
	}));
	expect(dimensions.height).toBeGreaterThan(0);
	expect(dimensions.height).toBeLessThan(dimensions.fullHeight);
	expect(dimensions.duration).toBe(300);
	expect(dimensions.easing).toBe("ease");
	expect(dimensions.overflow).toBe("hidden");
}

async function finishMotion(panel: Locator) {
	await panel.evaluate((element) => {
		for (const animation of element.getAnimations()) animation.finish();
	});
}

test("list panels animate both directions and preserve drafts across interrupted switching", async ({
	page,
}) => {
	const errors = observePageErrors(page);
	await page.emulateMedia({ reducedMotion: "no-preference" });
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	const columns = page.locator("#collection-columns");
	const filters = page.locator("#collection-filters");
	const columnsButton = page.getByRole("button", { name: "Columns", exact: true });
	const filtersButton = page.getByRole("button", { name: "Filters", exact: true });

	for (const [panel, button] of [
		[columns, columnsButton],
		[filters, filtersButton],
	] as const) {
		await expect(panel).toBeHidden();
		await captureMotion(panel);
		await button.press("Enter");
		await expectIntermediateHeight(panel);
		await expect(button).toBeFocused();
		await finishMotion(panel);
		await expect(panel).toHaveCSS("overflow", "visible");
		await captureMotion(panel);
		await button.press("Space");
		await expectIntermediateHeight(panel);
		await expect(panel).toHaveAttribute("inert", "");
		await expect(panel).toHaveAttribute("aria-hidden", "true");
		await expect(button).toBeFocused();
		await finishMotion(panel);
		await expect(panel).toBeHidden();
	}

	await filtersButton.click();
	await page.getByRole("button", { name: "Add filter", exact: true }).click();
	const field = page.getByLabel("Filter field", { exact: true });
	await chooseRiduSelect(page, field, "Summary");
	await expect(page.getByLabel("Filter value", { exact: true })).toHaveValue("");

	await captureMotion(filters);
	await columnsButton.click();
	await expectIntermediateHeight(filters);
	await expect(filters).toHaveAttribute("inert", "");
	// Reverse the pending exit before it can hide the re-opened content.
	await filtersButton.click();
	await expect(filters).not.toHaveAttribute("inert");
	await expect(filters).toHaveCSS("overflow", "visible");
	await expect(columns).toBeHidden();
	await expect(field).toContainText("Summary");
	await expect(page.getByLabel("Filter value", { exact: true })).toHaveValue("");
	await expect(filtersButton).toBeFocused();

	await page.setViewportSize({ width: 390, height: 844 });
	await expect(filters).toHaveCSS("overflow", "visible");
	const expandedHeight = await filters.evaluate(
		(element) => element.getBoundingClientRect().height
	);
	await captureMotion(filters);
	await filtersButton.click();
	await expectIntermediateHeight(filters);
	const measuredHeight = await filters.evaluate((element) =>
		Number.parseFloat(
			getComputedStyle(element).getPropertyValue("--bits-collapsible-content-height")
		)
	);
	expect(measuredHeight).toBeCloseTo(expandedHeight, 0);
	await finishMotion(filters);
	await expect(filters).toBeHidden();
	expect(errors).toEqual({ consoleErrors: [], pageErrors: [] });
});

test("list panels respect reduced motion and still retain filter drafts", async ({ page }) => {
	await page.emulateMedia({ reducedMotion: "reduce" });
	await loginAsEditor(page);
	await page.goto("/admin/collections/posts");
	const filtersButton = page.getByRole("button", { name: "Filters", exact: true });
	const columnsButton = page.getByRole("button", { name: "Columns", exact: true });
	const filters = page.locator("#collection-filters");
	const columns = page.locator("#collection-columns");
	await filtersButton.click();
	await expect(filters).toBeVisible();
	await expect(filters).toHaveCSS("animation-name", "none");
	await page.getByRole("button", { name: "Add filter", exact: true }).click();
	await columnsButton.click();
	await expect(filters).toBeHidden();
	await expect(columns).toBeVisible();
	await expect(columns).toHaveCSS("animation-name", "none");
	await filtersButton.click();
	await expect(columns).toBeHidden();
	await expect(page.getByLabel("Filter value", { exact: true })).toHaveValue("");
	await filtersButton.click();
	await expect(filters).toBeHidden();
});
