import { describe, expect, it } from "bun:test";
import { cv } from "../../packages/ui/src/lib/variants";

describe("semantic component variants", () => {
	it("keeps defaults when optional component props are forwarded as undefined", () => {
		const button = cv({
			base: "ridu-button",
			variants: {
				tone: { primary: "ridu-button--primary", quiet: "ridu-button--quiet" },
				size: { small: "ridu-button--small", large: "ridu-button--large" },
			},
			defaultVariants: { tone: "primary", size: "small" },
		});

		expect(button()).toBe("ridu-button ridu-button--primary ridu-button--small");
		expect(button({ tone: undefined, size: "large", class: "app-submit" })).toBe(
			"ridu-button ridu-button--primary ridu-button--large app-submit"
		);
		expect(button({ tone: "quiet" })).toBe("ridu-button ridu-button--quiet ridu-button--small");
		expect(button()).toBe("ridu-button ridu-button--primary ridu-button--small");
	});

	it("omits unselected or empty variants and leaves class precedence to CSS", () => {
		const surface = cv({
			base: "ridu-surface px-2",
			variants: { tone: { neutral: "", warning: "ridu-surface--warning" } },
		});

		expect(surface()).toBe("ridu-surface px-2");
		expect(surface({ tone: "neutral", class: "px-4" })).toBe("ridu-surface px-2 px-4");
		expect(surface({ tone: "warning" })).toBe("ridu-surface px-2 ridu-surface--warning");
	});
});
