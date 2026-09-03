import { defineConfig } from "@playwright/test";

const baseURL = requiredEnvironment("RIDU_MONGODB_PRODUCTION_ADMIN_URL");
const outputDir = requiredEnvironment("RIDU_MONGODB_PRODUCTION_PLAYWRIGHT_OUTPUT_DIR");

export default defineConfig({
	testDir: "./tests/e2e",
	testMatch: "**/admin/generated-mongodb-production.spec.ts",
	fullyParallel: false,
	workers: 1,
	retries: 0,
	timeout: 90_000,
	expect: { timeout: 15_000 },
	outputDir,
	reporter: [["line"]],
	use: {
		baseURL,
		trace: "off",
		screenshot: "off",
		video: "off",
	},
});

function requiredEnvironment(name: string) {
	const value = process.env[name]?.trim();
	if (!value) throw new Error(`${name} is required`);
	return value;
}
