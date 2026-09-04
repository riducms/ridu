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
	return defineConfig({
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
			presetWind4(),
			shadcnPreset,
			presetIcons({ scale: 1.2 }),
			presetTypography(),
			presetAnimations(),
		],
		transformers: [transformerDirectives(), transformerVariantGroup()],
	});
}

export default createAdminUnoConfig();
