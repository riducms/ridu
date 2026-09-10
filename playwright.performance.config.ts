import { defineConfig } from "@playwright/test";
import application from "./playwright.config";

// Run separately from correctness/Go gates: timing assertions need an idle host.
export default defineConfig({
	...application,
	testDir: "./tests/performance",
	testIgnore: [],
	fullyParallel: false,
	workers: 1,
	outputDir: ".ridu/playwright/performance/results",
});
