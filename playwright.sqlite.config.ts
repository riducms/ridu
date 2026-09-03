import { defineConfig } from "@playwright/test";

const resetToken = process.env.RIDU_BROWSER_RESET_TOKEN ?? "ridu-playwright-sqlite-v1";
if (!/^[A-Za-z0-9_-]{16,128}$/.test(resetToken)) {
	throw new Error(
		"RIDU_BROWSER_RESET_TOKEN must contain 16-128 letters, digits, underscores, or hyphens"
	);
}
process.env.RIDU_BROWSER_RESET_TOKEN = resetToken;

const fixtureCommand =
	process.env.RIDU_ADMIN_FIXTURE_PREBUILT === "true"
		? `go build -o .ridu/admin-server-fixture ./tests/contracts/admin_server && exec env RIDU_SQLITE_FIXTURE=true RIDU_BROWSER_RESET_TOKEN=${resetToken} RIDU_BROWSER_ADMIN_DIR=.ridu/admin-fixture-build ./.ridu/admin-server-fixture`
		: `bun run build:admin-fixture && go build -o .ridu/admin-server-fixture ./tests/contracts/admin_server && exec env RIDU_SQLITE_FIXTURE=true RIDU_BROWSER_RESET_TOKEN=${resetToken} RIDU_BROWSER_ADMIN_DIR=.ridu/admin-fixture-build ./.ridu/admin-server-fixture`;

export default defineConfig({
	testDir: "./tests/e2e/admin",
	testMatch: "sqlite-smoke.spec.ts",
	fullyParallel: false,
	workers: 1,
	retries: 0,
	use: {
		baseURL: "http://127.0.0.1:18081",
		trace: "retain-on-failure",
	},
	webServer: {
		command: fixtureCommand,
		url: "http://127.0.0.1:18081/api/schema",
		reuseExistingServer: false,
		timeout: 60_000,
		gracefulShutdown: { signal: "SIGTERM", timeout: 10_000 },
	},
});
