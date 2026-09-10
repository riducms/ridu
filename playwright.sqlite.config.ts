import { defineConfig } from "@playwright/test";

const resetToken = process.env.RIDU_BROWSER_RESET_TOKEN ?? "ridu-playwright-sqlite-v1";
if (!/^[A-Za-z0-9_-]{16,128}$/.test(resetToken)) {
	throw new Error(
		"RIDU_BROWSER_RESET_TOKEN must contain 16-128 letters, digits, underscores, or hyphens"
	);
}
process.env.RIDU_BROWSER_RESET_TOKEN = resetToken;
process.env.RIDU_SQLITE_FIXTURE = "true";

export default defineConfig({
	outputDir: ".ridu/playwright/sqlite/results",
	reporter: [["list"]],
	testDir: "./tests/e2e/admin",
	testMatch: ["sqlite-smoke.spec.ts", "blocks.spec.ts", "primitive-lists.spec.ts"],
	fullyParallel: false,
	workers: 1,
	retries: 0,
	use: {
		trace: "retain-on-failure",
	},
	globalSetup: "./tests/e2e/admin/setup.ts",
});
