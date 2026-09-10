import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

import { ensureBinary } from "../src/launcher.js";
import { releaseFixture, releaseServer } from "../tests/release-fixture.js";

const flags = new Set(process.argv.slice(2));
const supported = new Set(["--unknown-size", "--timeout", "--non-tty"]);
if ([...flags].some((flag) => !supported.has(flag))) {
	throw new Error("Usage: bun run preview:download [--unknown-size] [--timeout] [--non-tty]");
}
if (process.platform === "win32") {
	throw new Error(
		"This local tarball preview runs on macOS or Linux; the launcher also supports Windows release ZIPs."
	);
}

const { version } = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
const fixture = releaseFixture(version, 8 * 1024 * 1024);
const cache = mkdtempSync(join(tmpdir(), "ridu-download-preview-"));
const controller = new AbortController();
const server = await releaseServer((_request, response) => {
	void (async () => {
		await delay(1500, undefined, { signal: controller.signal });
		if (_request.url.endsWith("SHA256SUMS")) {
			response.end(fixture.checksums);
			return;
		}
		response.writeHead(
			200,
			flags.has("--unknown-size") ? {} : { "Content-Length": fixture.archive.length }
		);
		const chunkSize = Math.ceil(fixture.archive.length / 32);
		for (let offset = 0; offset < fixture.archive.length; offset += chunkSize) {
			if (response.destroyed) return;
			response.write(fixture.archive.subarray(offset, offset + chunkSize));
			if (flags.has("--timeout")) return;
			await delay(250, undefined, { signal: controller.signal });
		}
		response.end();
	})().catch((error) => {
		if (!controller.signal.aborted) response.destroy(error);
	});
});

// Only this preview process uses the disposable cache and loopback release server.
delete process.env.RIDU_BINARY;
process.env.RIDU_CLI_CACHE_DIR = cache;
process.env.RIDU_CLI_RELEASE_BASE_URL = server.url;
process.env.RIDU_CLI_DOWNLOAD_TIMEOUT_MS = flags.has("--timeout") ? "3000" : "30000";
if (flags.has("--non-tty")) process.env.RIDU_ACCESSIBLE = "1";

process.stderr.write("\nLocal download preview — no internet access or project creation.\n\n");
const interrupt = () => controller.abort();
process.once("SIGINT", interrupt);
process.once("SIGTERM", interrupt);
let closing;
const closeServer = () => (closing ??= server.close());
controller.signal.addEventListener(
	"abort",
	() => {
		void closeServer();
	},
	{ once: true }
);
try {
	await ensureBinary(version);
	process.stderr.write(
		"\nPreview complete. A real create-ridu run would now open the project wizard.\n\n"
	);
} catch (error) {
	console.error(`create-ridu: ${error.message}`);
	process.exitCode = 1;
} finally {
	controller.abort();
	await closeServer();
	process.off("SIGINT", interrupt);
	process.off("SIGTERM", interrupt);
	// The fixture is never executed or left in the normal CLI cache.
	rmSync(cache, { recursive: true, force: true });
}
