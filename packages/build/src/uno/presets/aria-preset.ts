import { definePreset } from "unocss";
import { h, variantGetParameter } from "@unocss/preset-wind4/utils";
import type { Theme } from "@unocss/preset-wind4";

/**
 * Fills two gaps in preset-wind4's aria support (compared to Tailwind v4):
 *
 * 1. `aria-invalid:` — wind4's theme.aria map is missing `invalid`,
 *    so we extend it. Resolves to `[aria-invalid="true"]`.
 *
 * 2. `not-aria-*:` — wind4 only supports `not-` with pseudo-classes.
 *    This adds the negated aria variant, supporting both arbitrary
 *    values (`not-aria-[haspopup]` -> `:not([aria-haspopup])`) and
 *    theme keys (`not-aria-checked` -> `:not([aria-checked="true"])`).
 *
 * Both are multiPass, so they chain with other variants,
 * e.g. `active:not-aria-[haspopup]:translate-y-px`.
 */
export default definePreset(() => ({
	name: "aria-preset",

	theme: {
		aria: {
			invalid: 'invalid="true"',
		},
	},

	variants: [
		{
			name: "not-aria",
			match(matcher, ctx) {
				const variant = variantGetParameter("not-aria-", matcher, ctx.generator.config.separators);
				if (variant) {
					const [match, rest] = variant;
					if (match === undefined || rest === undefined) return;
					const aria = h.bracket?.(match) ?? (ctx.theme as Theme).aria?.[match] ?? "";
					if (aria) {
						return {
							matcher: rest,
							selector: (s) => `${s}:not([aria-${aria}])`,
						};
					}
				}
			},
			multiPass: true,
		},
	],
}));
