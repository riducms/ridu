import { expect, test as base } from "@playwright/test";
import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";

export { expect };
export type { Page } from "@playwright/test";

export const test = base.extend<
	{ resetAdminFixture: void },
	{ adminServer: { url: string; previewURL: string } }
>({
	adminServer: [
		async ({}, use) => {
			const binary = process.env.RIDU_BROWSER_BINARY;
			if (!binary) throw new Error("admin browser global setup did not build the server");
			const child = spawn(binary, [], {
				env: {
					...process.env,
					RIDU_BROWSER_ADDRESS: "127.0.0.1:0",
					RIDU_BROWSER_PREVIEW_ADDRESS: "127.0.0.1:0",
					RIDU_BROWSER_BOOTSTRAP: "",
					RIDU_BROWSER_ADMIN_DIR: ".ridu/admin-fixture-build",
					RIDU_POSTGRES_FIXTURE_SCHEMA: `ridu_admin_fixture_${randomUUID().replaceAll("-", "")}`,
				},
				stdio: ["ignore", "pipe", "pipe"],
			});
			let output = "";
			const exited = new Promise<void>((resolve) => child.once("close", () => resolve()));
			try {
				const server = await new Promise<{ url: string; previewURL: string }>((resolve, reject) => {
					const timer = setTimeout(
						() => reject(new Error(`admin fixture startup timed out:\n${output}`)),
						30_000
					);
					const read = (data: Buffer) => {
						output += data.toString();
						const url = /admin fixture listening at (http:\/\/127\.0\.0\.1:\d+)\/admin/.exec(
							output
						)?.[1];
						const previewURL = /preview fixture listening at (http:\/\/127\.0\.0\.1:\d+)/.exec(
							output
						)?.[1];
						if (url && previewURL) {
							clearTimeout(timer);
							resolve({ url, previewURL });
						}
					};
					child.stdout.on("data", read);
					child.stderr.on("data", read);
					child.once("error", (error) => {
						clearTimeout(timer);
						reject(error);
					});
					child.once("close", (code) => {
						clearTimeout(timer);
						reject(new Error(`admin fixture exited (${code}):\n${output}`));
					});
				});
				await use(server);
			} finally {
				child.kill("SIGTERM");
				const killTimer = setTimeout(() => child.kill("SIGKILL"), 10_000);
				await exited;
				clearTimeout(killTimer);
			}
		},
		{ scope: "worker", timeout: 45_000 },
	],
	baseURL: async ({ adminServer }, use) => {
		await use(adminServer.url);
	},
	resetAdminFixture: [
		async ({ request }, use) => {
			const resetToken = process.env.RIDU_BROWSER_RESET_TOKEN;
			if (!resetToken) {
				throw new Error("RIDU_BROWSER_RESET_TOKEN is required for the admin fixture reset");
			}
			const response = await request.post("/__ridu-test/reset", {
				headers: { "X-Ridu-Test-Reset-Token": resetToken },
			});
			if (!response.ok()) {
				throw new Error(`admin fixture reset failed with ${response.status()}`);
			}
			await use();
		},
		{ auto: true },
	],
});
