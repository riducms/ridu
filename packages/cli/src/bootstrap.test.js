import { spawn } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "bun:test";

import { releaseFixture, releaseServer } from "../tests/release-fixture.js";
import { releaseTarget } from "./launcher.js";

const { version } = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
const initializer = fileURLToPath(new URL("../../create-ridu/src/cli.js", import.meta.url));
const target = releaseTarget();

function run(cache, url, extraEnv = {}) {
	const env = {
		...process.env,
		RIDU_CLI_CACHE_DIR: cache,
		RIDU_CLI_RELEASE_BASE_URL: url,
		RIDU_CLI_DOWNLOAD_TIMEOUT_MS: "2000",
		npm_config_user_agent: "bun/1.4.0",
		...extraEnv,
	};
	delete env.RIDU_BINARY;
	const child = spawn("node", [initializer, "my-app", "--template", "blank"], { env });
	let stdout = "";
	let stderr = "";
	const started = performance.now();
	const firstOutput = new Promise((resolve) =>
		child.stderr.once("data", () => resolve(performance.now() - started))
	);
	child.stdout.on("data", (chunk) => {
		stdout += chunk;
	});
	child.stderr.on("data", (chunk) => {
		stderr += chunk;
	});
	const done = new Promise((resolve, reject) => {
		child.once("error", reject);
		child.once("close", (code) => resolve({ code, stdout, stderr }));
	});
	return { child, done, firstOutput };
}

// The downloaded shell fixture exercises process forwarding on the POSIX release hosts.
test.skipIf(process.platform === "win32")(
	"cold create-ridu reports before any response; warm cache is silent and preserves command output",
	async () => {
		const fixture = releaseFixture(version);
		const cache = mkdtempSync(join(tmpdir(), "ridu-bootstrap-"));
		const pending = [];
		let requests = 0;
		let bothRequested;
		const requested = new Promise((resolve) => {
			bothRequested = resolve;
		});
		const server = await releaseServer((request, response) => {
			requests++;
			pending.push(() =>
				response.end(request.url.endsWith("SHA256SUMS") ? fixture.checksums : fixture.archive)
			);
			if (pending.length === 2) bothRequested();
		});
		let invocation;
		try {
			invocation = run(cache, server.url);
			await requested;
			// Neither headers nor body have been released; progress must precede network completion.
			expect(await invocation.firstOutput).toBeLessThan(1000);
			for (const respond of pending) respond();
			const cold = await invocation.done;
			expect(cold.code).toBe(7);
			expect(cold.stdout).toBe("new\n--package-manager\nbun\nmy-app\n--template\nblank\n");
			expect(cold.stderr).toContain(`Downloading Ridu v${version}`);
			expect(cold.stderr).toContain("Verifying download");
			expect(cold.stderr).toContain("Ridu CLI ready");
			expect(cold.stderr).not.toMatch(/[\r\x1b]/);
			const warm = await run(cache, server.url).done;
			expect(warm.code).toBe(cold.code);
			expect(warm.stdout).toBe(cold.stdout);
			expect(warm.stderr).toBe("");
			expect(requests).toBe(2);
		} finally {
			invocation?.child.kill();
			await server.close();
			rmSync(cache, { recursive: true, force: true });
		}
	}
);

test.skipIf(process.platform === "win32").each(["checksum", "http", "timeout"])(
	"a %s failure exits promptly without caching or executing partial data",
	async (failure) => {
		const fixture = releaseFixture(version);
		const cache = mkdtempSync(join(tmpdir(), "ridu-bootstrap-"));
		const server = await releaseServer((request, response) => {
			if (request.url.endsWith("SHA256SUMS")) {
				if (failure === "checksum") response.end(`${"0".repeat(64)}  ${fixture.filename}\n`);
				else if (failure === "http") response.writeHead(503).end();
				// The timeout case leaves checksums pending after the archive is complete.
			} else if (failure !== "http") {
				response.end(fixture.archive);
			}
			// The HTTP case leaves the archive pending to exercise sibling cancellation.
		});
		let invocation;
		try {
			invocation = run(cache, server.url, { RIDU_CLI_DOWNLOAD_TIMEOUT_MS: "200" });
			const result = await invocation.done;
			expect(result.code).toBe(1);
			expect(result.stdout).toBe("");
			expect(result.stderr).toContain("Ridu CLI setup failed");
			expect(result.stderr).not.toContain("Ridu CLI ready");
			expect(result.stderr).toContain(
				failure === "checksum"
					? "checksum mismatch"
					: failure === "http"
						? "HTTP 503"
						: "RIDU_CLI_DOWNLOAD_TIMEOUT_MS"
			);
			if (failure === "timeout") expect(result.stderr).toContain("waiting for checksums");
			const destination = join(cache, version, `${target.goos}-${target.goarch}`, "ridu");
			expect(existsSync(destination)).toBe(false);
			expect(existsSync(`${destination}.${invocation.child.pid}.tmp`)).toBe(false);
		} finally {
			invocation?.child.kill();
			await server.close();
			rmSync(cache, { recursive: true, force: true });
		}
	}
);
