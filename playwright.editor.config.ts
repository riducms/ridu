import { defineConfig } from "@playwright/test";

const app = "bun run --cwd tests/contracts/editor_app";

// The SvelteKit fixture runs in development and as a production build, both rendering on the
// server. It uses the packages' svelte-package output, so build:runtime-packages runs first. Its
// post pages save to the Go server in tests/contracts/editor_server. Each project has its own,
// because the in-memory store keeps only the last of two concurrent writes.
const servers = {
	dev: { app: 4180, ridu: 4182 },
	build: { app: 4181, ridu: 4183 },
};

export default defineConfig({
	outputDir: ".ridu/playwright/editor/results",
	reporter: [["list"]],
	testDir: "./tests/e2e/editor",
	retries: 0,
	use: { trace: "retain-on-failure" },
	projects: Object.entries(servers).map(([name, ports]) => ({
		name,
		use: { baseURL: `http://127.0.0.1:${ports.app}` },
		metadata: { riduURL: `http://127.0.0.1:${ports.ridu}` },
	})),
	webServer: [
		...Object.values(servers).map((ports) => ({
			command: "go run ./tests/contracts/editor_server",
			env: { RIDU_EDITOR_SERVER_ADDRESS: `127.0.0.1:${ports.ridu}` },
			url: `http://127.0.0.1:${ports.ridu}/api/collections/posts`,
			reuseExistingServer: false,
			timeout: 120_000,
		})),
		{
			command: `${app} dev --host 127.0.0.1 --port ${servers.dev.app} --strictPort`,
			env: { RIDU_URL: `http://127.0.0.1:${servers.dev.ridu}` },
			url: `http://127.0.0.1:${servers.dev.app}`,
			reuseExistingServer: false,
			timeout: 120_000,
		},
		{
			command: `${app} build && ${app} preview --host 127.0.0.1 --port ${servers.build.app} --strictPort`,
			env: { RIDU_URL: `http://127.0.0.1:${servers.build.ridu}` },
			url: `http://127.0.0.1:${servers.build.app}`,
			reuseExistingServer: false,
			timeout: 180_000,
		},
	],
});
