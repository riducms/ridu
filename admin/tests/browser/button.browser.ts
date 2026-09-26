import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { expect, it } from "vitest";
import { render } from "vitest-browser-svelte";

const Buttons = svelte`
		<script>
			import { Button, TooltipProvider } from "@riducms/ui";
			let expanded = $state(false);
			let replaceLabel = $state("Replace pending upload");
		</script>
		<TooltipProvider>
			<Button variant="outline" aria-invalid="true">Invalid relationship</Button>
			<Button variant="ghost" aria-expanded={expanded} onclick={() => expanded = !expanded}>Filters</Button>
			<Button href="/disabled-destination" disabled>Unavailable link</Button>
			<Button aria-label="Default icon size">
				<svg data-testid="default-button-icon" aria-hidden="true" viewBox="0 0 24 24">
					<path d="M5 12h14" />
				</svg>
			</Button>
			<Button aria-label="Semantic icon size">
				<svg class="feature-action__icon" data-testid="semantic-button-icon" aria-hidden="true" viewBox="0 0 24 24">
					<path d="M12 5v14" />
				</svg>
			</Button>
			<Button tooltip="Replace upload" aria-label={replaceLabel} data-testid="tooltip-button">Replace</Button>
			<button type="button" onclick={() => replaceLabel = "Replace ridu-cover.png"}>Resolve upload</button>
			<span data-testid="colors" style="display:block;width:40px;height:40px;border:1px solid var(--destructive);background:var(--control-surface-hover);color:var(--foreground)"></span>
		</TooltipProvider>

	<style>
		@layer ridu.components {
			.feature-action__icon {
				width: 20px;
				height: 18px;
			}
		}
	</style>
`;

it("shared buttons retain state feedback, disabled hit targets, and semantic icon sizing", async () => {
	const screen = await render(Buttons);
	try {
		const colors = screen.getByTestId("colors");
		const reference = getComputedStyle(colors.element());
		const invalid = screen.getByRole("button", { name: "Invalid relationship" });
		expect(getComputedStyle(invalid.element()).borderTopColor).toBe(reference.borderTopColor);
		await invalid.hover();
		await expect
			.poll(() => getComputedStyle(invalid.element()).borderTopColor)
			.toBe(reference.borderTopColor);

		const filters = screen.getByRole("button", { name: "Filters" });
		await filters.click();
		await colors.hover();
		await expect.element(filters).toHaveAttribute("aria-expanded", "true");
		await expect
			.poll(() => getComputedStyle(filters.element()).backgroundColor)
			.toBe(reference.backgroundColor);
		expect(getComputedStyle(filters.element()).color).toBe(reference.color);

		const disabled = screen.getByRole("link", { name: "Unavailable link" });
		await expect.element(disabled).not.toHaveAttribute("href");
		await expect.element(disabled).toHaveAttribute("tabindex", "-1");
		const element = disabled.element();
		const rect = element.getBoundingClientRect();
		const hit = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2);
		expect(element.contains(hit)).toBe(false);

		const defaultIcon = screen.getByTestId("default-button-icon");
		expect(getComputedStyle(defaultIcon.element()).width).toBe("14px");
		expect(getComputedStyle(defaultIcon.element()).height).toBe("14px");
		const semanticIcon = screen.getByTestId("semantic-button-icon");
		expect(getComputedStyle(semanticIcon.element()).width).toBe("20px");
		expect(getComputedStyle(semanticIcon.element()).height).toBe("18px");

		const replace = screen.getByTestId("tooltip-button");
		await expect.element(replace).toHaveAttribute("aria-label", "Replace pending upload");
		await screen.getByRole("button", { name: "Resolve upload" }).click();
		await expect.element(replace).toHaveAttribute("aria-label", "Replace ridu-cover.png");
	} finally {
		await screen.unmount();
	}
});
