import { fileURLToPath } from "node:url";

import {
	defineConfig,
	presetIcons,
	presetTypography,
	presetWind4,
	transformerDirectives,
	transformerVariantGroup,
} from "unocss";
import presetAnimations from "unocss-preset-animations";

import ariaPreset from "./presets/aria-preset.js";
import riduUtilitiesPreset from "./presets/custom-preset.js";
import dashPreset from "./presets/dash-preset.js";
import shadcnPreset from "./presets/shadcn-preset.js";

export interface AdminUnoConfigOptions {
	filesystem?: readonly string[];
}

/** Return Ridu's shared UnoCSS utility shortcuts and variants. */
export function presetRiduUtilities() {
	return riduUtilitiesPreset;
}

const presetDependencies = [
	"./presets/custom-preset.js",
	"./presets/shadcn-preset.js",
	"./presets/dash-preset.js",
	"./presets/aria-preset.js",
].map((path) => fileURLToPath(new URL(path, import.meta.url)));

export function createAdminUnoConfig(options: AdminUnoConfigOptions = {}) {
	const wind = presetWind4();
	// Reserve the public cascade even when a utility-only library stylesheet loads first.
	// Keep this order aligned with @riducms/ui/layers.css until UnoCSS is retired.
	const cssLayers = [
		"ridu",
		"ridu-plugins",
		"app",
		"ridu.reset",
		"ridu.theme",
		"ridu.base",
		"ridu.components",
		"ridu.utilities",
	];
	return defineConfig({
		layers: Object.fromEntries(cssLayers.map((layer, index) => [layer, -10000 + index])),
		outputToCssLayers: {
			allLayers: true,
			cssLayerName: (layer) =>
				cssLayers.includes(layer)
					? layer
					: ["base", "theme", "properties"].includes(layer)
						? `ridu.reset.${layer}`
						: `ridu.utilities.${layer}`,
		},
		content: {
			filesystem: [
				"./node_modules/bits-ui/dist/**/*.{html,js,svelte,ts}",
				...(options.filesystem ?? []),
			],
			pipeline: {
				include: [/\.(vue|svelte|[jt]sx|mdx?|astro|elm|php|phtml|html|ts)($|\?)/],
			},
		},
		shortcuts: [{}],
		configDeps: presetDependencies,
		presets: [
			presetRiduUtilities(),
			dashPreset(),
			ariaPreset,
			{
				name: "ridu-deterministic-wind-theme",
				preflights: [
					{
						layer: "theme",
						getCSS() {
							// Wind records discovered theme variables in a Set. Normalize that
							// discovery order before its preflight serializes the theme layer.
							const dependencies = wind.meta?.themeDeps;
							if (!(dependencies instanceof Set)) return "";
							const ordered = [...dependencies].sort();
							dependencies.clear();
							for (const dependency of ordered) dependencies.add(dependency);
							return "";
						},
					},
				],
			},
			wind,
			shadcnPreset,
			presetIcons({ scale: 1.2 }),
			presetTypography(),
			presetAnimations(),
		],
		transformers: [transformerDirectives(), transformerVariantGroup()],
	});
}

export default createAdminUnoConfig();
