import { defineConfig } from "@playwright/test";

const baseURL = process.env.RIDU_MONGODB_GENERATED_ADMIN_URL;
if (!baseURL) {
	throw new Error("RIDU_MONGODB_GENERATED_ADMIN_URL is required");
}

export default defineConfig({
	outputDir: ".ridu/playwright/mongodb-generated/results",
	reporter: [["list"]],
	testDir: "./tests/e2e",
	testMatch: "**/admin/generated-mongodb.spec.ts",
	fullyParallel: false,
	workers: 1,
	retries: 0,
	use: {
		baseURL,
		trace: "retain-on-failure",
	},
});
