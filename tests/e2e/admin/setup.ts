import { execFileSync } from "node:child_process";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { resolve } from "node:path";

export default async function setup() {
	await mkdir(".ridu/check", { recursive: true });
	const directory = await mkdtemp(resolve(".ridu/check/admin-browser-"));
	const cleanup = () => rm(directory, { recursive: true, force: true });
	try {
		if (process.env.RIDU_ADMIN_FIXTURE_PREBUILT !== "true") {
			execFileSync("bun", ["run", "build:admin-fixture"], { stdio: "inherit" });
		}
		const binary = resolve(directory, "server");
		execFileSync("go", ["build", "-o", binary, "./tests/contracts/admin_server"], {
			stdio: "inherit",
		});
		process.env.RIDU_BROWSER_BINARY = binary;
		return cleanup;
	} catch (error) {
		await cleanup();
		throw error;
	}
}
