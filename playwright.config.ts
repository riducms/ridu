import { availableParallelism } from "node:os";
import { defineConfig } from "@playwright/test";

const resetToken = process.env.RIDU_BROWSER_RESET_TOKEN ?? "ridu-playwright-reset-v1";
if (!/^[A-Za-z0-9_-]{16,128}$/.test(resetToken)) {
	throw new Error(
		"RIDU_BROWSER_RESET_TOKEN must contain 16-128 letters, digits, underscores, or hyphens"
	);
}
process.env.RIDU_BROWSER_RESET_TOKEN = resetToken;

export default defineConfig({
	outputDir: ".ridu/playwright/admin/results",
	reporter: [["list"]],
	testDir: "./tests/e2e",
	testIgnore: [
		"**/admin/bootstrap.spec.ts",
		"**/admin/generated-mongodb.spec.ts",
		"**/admin/generated-mongodb-production.spec.ts",
		"**/admin/sqlite-smoke.spec.ts",
	],
	fullyParallel: true,
	workers: Math.min(4, availableParallelism()),
	retries: 0,
	use: {
		trace: "retain-on-failure",
	},
	globalSetup: "./tests/e2e/admin/setup.ts",
});
