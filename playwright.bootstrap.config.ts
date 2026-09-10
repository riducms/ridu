import { defineConfig } from "@playwright/test";

const fixtureCommand =
	process.env.RIDU_ADMIN_FIXTURE_PREBUILT === "true"
		? "go build -o .ridu/admin-server-bootstrap-fixture ./tests/contracts/admin_server && exec env RIDU_BROWSER_BOOTSTRAP=true RIDU_BROWSER_ADDRESS=127.0.0.1:18083 RIDU_BROWSER_PREVIEW_ADDRESS=127.0.0.1:18084 RIDU_BROWSER_ADMIN_DIR=.ridu/admin-fixture-build ./.ridu/admin-server-bootstrap-fixture"
		: "bun run build:admin-fixture && go build -o .ridu/admin-server-bootstrap-fixture ./tests/contracts/admin_server && exec env RIDU_BROWSER_BOOTSTRAP=true RIDU_BROWSER_ADDRESS=127.0.0.1:18083 RIDU_BROWSER_PREVIEW_ADDRESS=127.0.0.1:18084 RIDU_BROWSER_ADMIN_DIR=.ridu/admin-fixture-build ./.ridu/admin-server-bootstrap-fixture";

export default defineConfig({
	outputDir: ".ridu/playwright/bootstrap/results",
	reporter: [["list"]],
	testDir: "./tests/e2e",
	testMatch: "**/admin/bootstrap.spec.ts",
	fullyParallel: false,
	workers: 1,
	retries: 0,
	use: {
		baseURL: "http://127.0.0.1:18083",
		trace: "retain-on-failure",
	},
	webServer: {
		command: fixtureCommand,
		url: "http://127.0.0.1:18083/api/schema",
		reuseExistingServer: false,
		timeout: 60_000,
		gracefulShutdown: { signal: "SIGTERM", timeout: 10_000 },
	},
});
