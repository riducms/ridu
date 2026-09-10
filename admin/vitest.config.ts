import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";
import { playwright } from "@vitest/browser-playwright";
import inlineSvelte from "@hvniel/vite-plugin-svelte-inline-component/plugin";
import application from "./vite.config.ts";

const output = fileURLToPath(new URL("../.ridu/vitest-admin/", import.meta.url));

export default defineConfig((environment) => {
	const production = application(environment);
	return {
		...production,
		root: fileURLToPath(new URL(".", import.meta.url)),
		base: "/",
		cacheDir: `${output}/cache`,
		build: { outDir: `${output}/build` },
		plugins: [...(production.plugins ?? []), inlineSvelte()],
		// Inline harness imports are discovered after Vite's initial dependency scan.
		optimizeDeps: {
			...production.optimizeDeps,
			include: [
				"@dnd-kit-svelte/svelte",
				"@dnd-kit-svelte/svelte/sortable",
				"@hvniel/svelte-router",
				"svelte-sonner",
				"@riducms/ui > tailwind-variants",
			],
		},
		test: {
			name: "admin-components",
			include: ["tests/browser/**/*.browser.ts"],
			setupFiles: ["./tests/browser/setup.ts"],
			attachmentsDir: `${output}/attachments`,
			maxWorkers: 2,
			browser: {
				enabled: true,
				headless: true,
				provider: playwright(),
				instances: [{ browser: "chromium" }],
				screenshotDirectory: `${output}/screenshots`,
			},
		},
	};
});
