import { definePreset } from "unocss";

type DashSide = "top" | "bottom" | "left" | "right";

const DASH_COLOR = "var(--dash-color,var(--border))";

function resolveDashSides(rawSides: string) {
	const sides = new Set<DashSide>();

	for (const char of rawSides) {
		if (char === "t") sides.add("top");
		else if (char === "b") sides.add("bottom");
		else if (char === "l") sides.add("left");
		else if (char === "r") sides.add("right");
		else if (char === "x") {
			sides.add("left");
			sides.add("right");
		} else if (char === "y") {
			sides.add("top");
			sides.add("bottom");
		} else {
			return null;
		}
	}

	return ["top", "bottom", "left", "right"].filter((side) =>
		sides.has(side as DashSide)
	) as DashSide[];
}

/**
 * Dash preset
 *
 * Utility format:
 * - `dash-{sides}-{dash}-{gap}-{thickness}`
 *
 * Supported side tokens:
 * - `t`, `b`, `l`, `r`
 * - `x` (left + right)
 * - `y` (top + bottom)
 * - combinations like `tb`, `lr`, `tblr`
 *
 * Examples:
 * - Bottom divider: `w-full h-px dash-b-6-8-1`
 * - Vertical frame sides: `dash-x-5-8-1`
 * - Full dashed frame: `dash-tblr-10-10-1`
 * - Solid line (gap = 0): `dash-b-10-0-1`
 *
 * Dash color:
 * - Default color is `var(--border)`.
 * - Override per element with `--dash-color`:
 *   - `[--dash-color:var(--ring)] w-full h-px dash-b-6-8-1`
 *   - `[--dash-color:#3b82f6] dash-x-5-8-1`
 */
export default definePreset(() => ({
	name: "dash-preset",
	shortcuts: [
		[
			/^dash-([tblrxy]+)-(\d+)-(\d+)-(\d+)$/,
			([, rawSides = "", dashRaw = "", gapRaw = "", thicknessRaw = ""]) => {
				const sides = resolveDashSides(rawSides);
				if (!sides?.length) return;

				const dash = Number.parseInt(dashRaw, 10);
				const gap = Number.parseInt(gapRaw, 10);
				const thickness = Number.parseInt(thicknessRaw, 10);
				const cycle = dash + gap;

				const images: string[] = [];
				const sizes: string[] = [];
				const positions: string[] = [];

				for (const side of sides) {
					const isHorizontal = side === "top" || side === "bottom";
					const direction = isHorizontal ? "to right" : "to bottom";
					const gradient =
						gap === 0
							? `linear-gradient(${direction},${DASH_COLOR},${DASH_COLOR})`
							: `repeating-linear-gradient(${direction},${DASH_COLOR} 0px,${DASH_COLOR} ${dash}px,transparent ${dash}px,transparent ${cycle}px)`;

					images.push(gradient);
					sizes.push(isHorizontal ? `100% ${thickness}px` : `${thickness}px 100%`);
					positions.push(
						side === "top"
							? "left top"
							: side === "bottom"
								? "left bottom"
								: side === "left"
									? "left top"
									: "right top"
					);
				}

				return [
					{
						"background-image": images.join(","),
						"background-size": sizes.join(","),
						"background-position": positions.join(","),
						"background-repeat": "no-repeat",
					},
				];
			},
		],
	],
}));
