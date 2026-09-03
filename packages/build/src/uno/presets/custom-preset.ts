import { definePreset } from "unocss";
import { handler as h, variantGetParameter } from "@unocss/preset-wind4/utils";
import type { Theme } from "@unocss/preset-wind4";

export default definePreset(() => ({
	name: "custom-preset",

	rules: [
		["abs", { position: "absolute" }],
		["flex|col", { display: "flex", "flex-direction": "column" }],
	],

	shortcuts: [
		// [/^flex\|col$/, () => "flex flex-col", { layer: "default" }],
		[
			// flex-s stands for flex-shortcut
			// to avoid mixups with default flex utilities like flex-wrap
			/^(inline-)?flex-s-(start|center|between|evenly|around|end)(-(start|center|baseline|end))?(\|(col))?$/,
			([, i, justify, align, , col]) =>
				`${i || ""}flex justify-${justify} items${align || "-center"} ${col ? "flex-col" : ""}`,
			{ layer: "default" },
		],
		// use when width and height values are the same
		[/^s-(.*)$/, ([, v]) => `h-${v} w-${v}`, { layer: "utilities" }],
		// use when min width and height values are the same
		[/^min-s-(.*)$/, ([, v]) => `min-h-${v} min-w-${v}`, { layer: "utilities" }],

		[
			/^scrollbar-f-(thin)-(.*)$/,
			([, size, colors]) => `[scrollbar-width:${size}] [scrollbar-color:${colors}]`,
			{ layer: "utilities" },
		],
		[
			/^teeny-scrollbar-(w|h)-(\d+)$/,
			([, ax, dg]) => `
      scrollbar:${ax}-${dg}
      scrollbar-track:(rd-xl bg-transparent)
      scrollbar-thumb:(rd-xl bg-grey-4)
      `,
		],
	],

	variants: [
		{
			// adds support for "@min-[width]:class" and "@min-h-[width]:class"
			// or
			// "@min-width:class" and "@min-h-width:class"
			name: "arbitrary-media-query",
			match(matcher, { theme }) {
				// prefix with @ to specify that it's a media query
				const minVariant = variantGetParameter("@min-", matcher, [":", "-"]);
				const maxVariant = variantGetParameter("@max-", matcher, [":", "-"]);
				const minHeightVariant = variantGetParameter("@min-h-", matcher, [":", "-"]);
				const maxHeightVariant = variantGetParameter("@max-h-", matcher, [":", "-"]);

				// the order that we check the variants is important
				// because we want to match the most specific one
				const matched =
					(minHeightVariant && {
						type: "min-h",
						variant: minHeightVariant,
					}) ||
					(maxHeightVariant && {
						type: "max-h",
						variant: maxHeightVariant,
					}) ||
					(minVariant && {
						type: "min",
						variant: minVariant,
					}) ||
					(maxVariant && {
						type: "max",
						variant: maxVariant,
					});

				if (matched?.variant) {
					const [match, rest] = matched.variant;
					if (match === undefined || rest === undefined) return;
					// this is for extracting the value from the match and
					// makes sure it either has no brackets or has brackets
					const extractedValue =
						h.bracket?.(match) || (!match.startsWith("[") && !match.endsWith("]") && match) || "";
					const endsWithUnit = /^\d+(em|px|rem)$/.test(extractedValue);
					const isOnlyNum = /^\d+$/.test(extractedValue);
					const breakpoints =
						(theme as Theme & { breakpoints?: Record<string, string> }).breakpoints ?? {};

					if (endsWithUnit || isOnlyNum || breakpoints[extractedValue]) {
						return {
							matcher: rest,
							layer: "utilities",
							handle: (input, next) =>
								next({
									...input,
									parent: `${input.parent ? `${input.parent} $$ ` : ""}@media (${
										matched.type == "min"
											? "min-width"
											: matched.type == "max"
												? "max-width"
												: matched.type == "min-h"
													? "min-height"
													: "max-height"
									}:${
										endsWithUnit
											? extractedValue
											: isOnlyNum
												? extractedValue + "px"
												: breakpoints[extractedValue]
									})`,
								}),
						};
					}
				}
			},
		},
		{
			name: "firefox-only",
			match(matcher) {
				const ffVariant = variantGetParameter("@ff", matcher, [":"]);
				if (ffVariant) {
					const [, rest] = ffVariant;
					return {
						matcher: rest,
						handle: (input, next) =>
							next({
								...input,
								parent: `${input.parent ? `${input.parent} $$ ` : ""}@-moz-document url-prefix()`,
							}),
					};
				}
			},
		},
		(matcher) => {
			const [m1, m2, m3] = ["scrollbar:", "scrollbar-track:", "scrollbar-thumb:"];
			let matchedStr = "";

			if (matcher.startsWith(m1)) {
				matchedStr = m1;
			} else if (matcher.startsWith(m2)) {
				matchedStr = m2;
			} else if (matcher.startsWith(m3)) {
				matchedStr = m3;
			} else {
				return matcher;
			}

			return {
				matcher: matcher.slice(matchedStr.length),
				selector: (s) =>
					`${s}::-webkit-scrollbar${matchedStr == m2 ? "-track" : matchedStr == m3 ? "-thumb" : ""}`,
				layer: "default",
			};
		},
	],
}));
