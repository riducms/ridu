import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { resolve } from "node:path";

const repositoryRoot = resolve(import.meta.dir, "..");
await mkdir(resolve(repositoryRoot, ".ridu/check"), { recursive: true });
const runDirectory = await mkdtemp(resolve(repositoryRoot, ".ridu/check/live-sdk-"));
const binary = resolve(runDirectory, "server");
const serverEnvironment = { ...process.env };
if (process.env.RIDU_LIVE_STORE === "sqlite") {
	serverEnvironment.RIDU_SQLITE_PATH = resolve(runDirectory, "live.sqlite");
} else {
	delete serverEnvironment.RIDU_SQLITE_PATH;
}

const build = Bun.spawn(["go", "build", "-o", binary, "./tests/contracts/live_server"], {
	cwd: repositoryRoot,
	stdout: "inherit",
	stderr: "inherit",
});
if ((await build.exited) !== 0) {
	await rm(runDirectory, { recursive: true, force: true });
	throw new Error("failed to build the live SDK server");
}

const server = Bun.spawn([binary], {
	cwd: repositoryRoot,
	env: serverEnvironment,
	stdout: "pipe",
	stderr: "inherit",
});

async function serverURL() {
	const reader = server.stdout.getReader();
	const decoder = new TextDecoder();
	let output = "";
	while (!output.includes("\n")) {
		const { done, value } = await reader.read();
		if (done) throw new Error("live SDK server exited before reporting its URL");
		output += decoder.decode(value, { stream: true });
	}
	return output.slice(0, output.indexOf("\n")).trim();
}

try {
	let timeout: ReturnType<typeof setTimeout> | undefined;
	const timedOut = new Promise<never>((_, reject) => {
		timeout = setTimeout(
			() => reject(new Error("timed out waiting for the live SDK server")),
			20_000
		);
	});
	const url = await Promise.race([serverURL(), timedOut]).finally(() => clearTimeout(timeout));
	if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(url)) throw new Error(`invalid live SDK URL: ${url}`);

	const test = Bun.spawn(["bun", "test", "tests/contracts/live-sdk.test.ts"], {
		cwd: repositoryRoot,
		env: { ...process.env, RIDU_LIVE_URL: url },
		stdout: "inherit",
		stderr: "inherit",
	});
	const exitCode = await test.exited;
	if (exitCode !== 0) process.exitCode = exitCode;
} finally {
	server.kill();
	await server.exited;
	await rm(runDirectory, { recursive: true, force: true });
}
