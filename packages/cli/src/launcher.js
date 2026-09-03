import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { basename, dirname, join } from "node:path";
import { gunzipSync } from "node:zlib";

const repository = "https://github.com/riducms/ridu";

export function releaseTarget(platform = process.platform, architecture = process.arch) {
	const operatingSystems = { darwin: "darwin", linux: "linux", win32: "windows" };
	const architectures = { arm64: "arm64", x64: "amd64" };
	const goos = operatingSystems[platform];
	const goarch = architectures[architecture];
	if (goos === undefined || goarch === undefined) {
		throw new Error(
			`Ridu has no prebuilt CLI for ${platform}/${architecture}; install it with Go instead`
		);
	}
	return { goarch, goos, windows: goos === "windows" };
}

export function archiveName(version, target) {
	const extension = target.windows ? "zip" : "tar.gz";
	return `ridu_${version}_${target.goos}_${target.goarch}.${extension}`;
}

export function expectedChecksum(checksums, filename) {
	for (const line of checksums.split(/\r?\n/u)) {
		const match = line.match(/^([a-fA-F0-9]{64})\s+\*?(.+)$/u);
		if (match !== null && basename(match[2]) === filename) {
			return match[1].toLowerCase();
		}
	}
	throw new Error(`Ridu release checksums do not contain ${filename}`);
}

export function verifyChecksum(bytes, expected, filename) {
	const actual = createHash("sha256").update(bytes).digest("hex");
	if (actual !== expected) {
		throw new Error(
			`Ridu CLI checksum mismatch for ${filename}: expected ${expected}, got ${actual}`
		);
	}
}

export function extractTarGzipBinary(archive, binaryName = "ridu") {
	const tar = gunzipSync(archive);
	for (let offset = 0; offset + 512 <= tar.length;) {
		const header = tar.subarray(offset, offset + 512);
		if (header.every((byte) => byte === 0)) break;
		const name = header.subarray(0, 100).toString("utf8").replace(/\0.*$/u, "");
		const encodedSize = header.subarray(124, 136).toString("ascii").replace(/\0.*$/u, "").trim();
		const size = Number.parseInt(encodedSize || "0", 8);
		if (!Number.isSafeInteger(size) || size < 0) {
			throw new Error(`invalid tar member size for ${name}`);
		}
		const contentOffset = offset + 512;
		if (basename(name) === binaryName) {
			return tar.subarray(contentOffset, contentOffset + size);
		}
		offset = contentOffset + Math.ceil(size / 512) * 512;
	}
	throw new Error(`Ridu release archive does not contain ${binaryName}`);
}

function cacheRoot() {
	if (process.env.RIDU_CLI_CACHE_DIR) return process.env.RIDU_CLI_CACHE_DIR;
	if (process.platform === "win32" && process.env.LOCALAPPDATA) {
		return join(process.env.LOCALAPPDATA, "ridu", "cli");
	}
	if (process.env.XDG_CACHE_HOME) return join(process.env.XDG_CACHE_HOME, "ridu", "cli");
	return join(homedir(), ".cache", "ridu", "cli");
}

async function download(url) {
	const response = await fetch(url, {
		headers: { "user-agent": "@riducms/cli" },
		redirect: "follow",
	});
	if (!response.ok) {
		throw new Error(`download ${url}: ${response.status} ${response.statusText}`);
	}
	return Buffer.from(await response.arrayBuffer());
}

function installWindowsArchive(archive, destination) {
	const stage = join(tmpdir(), `ridu-cli-${process.pid}-${Date.now()}`);
	const archivePath = `${stage}.zip`;
	mkdirSync(stage, { recursive: true });
	try {
		writeFileSync(archivePath, archive, { mode: 0o600 });
		const result = spawnSync(
			"powershell.exe",
			[
				"-NoProfile",
				"-NonInteractive",
				"-Command",
				`Expand-Archive -LiteralPath '${archivePath.replaceAll("'", "''")}' -DestinationPath '${stage.replaceAll("'", "''")}' -Force`,
			],
			{ encoding: "utf8" }
		);
		if (result.status !== 0) {
			const detail = result.error?.message ?? String(result.stderr ?? "").trim();
			throw new Error(`extract Ridu CLI archive: ${detail || "PowerShell exited unsuccessfully"}`);
		}
		writeFileSync(destination, readFileSync(join(stage, "ridu.exe")), { mode: 0o755 });
	} finally {
		rmSync(stage, { force: true, recursive: true });
		rmSync(archivePath, { force: true });
	}
}

export async function ensureBinary(version) {
	if (process.env.RIDU_BINARY) return process.env.RIDU_BINARY;
	const target = releaseTarget();
	const binaryName = target.windows ? "ridu.exe" : "ridu";
	const destination = join(cacheRoot(), version, `${target.goos}-${target.goarch}`, binaryName);
	if (existsSync(destination)) return destination;

	mkdirSync(dirname(destination), { recursive: true });
	const filename = archiveName(version, target);
	const releaseBase =
		process.env.RIDU_CLI_RELEASE_BASE_URL ?? `${repository}/releases/download/v${version}`;
	const [archive, checksums] = await Promise.all([
		download(`${releaseBase}/${filename}`),
		download(`${releaseBase}/SHA256SUMS`),
	]);
	verifyChecksum(archive, expectedChecksum(checksums.toString("utf8"), filename), filename);

	const temporary = `${destination}.${process.pid}.tmp`;
	try {
		if (target.windows) {
			installWindowsArchive(archive, temporary);
		} else {
			writeFileSync(temporary, extractTarGzipBinary(archive), { mode: 0o755 });
		}
		try {
			renameSync(temporary, destination);
		} catch (error) {
			if (!existsSync(destination)) throw error;
		}
	} finally {
		rmSync(temporary, { force: true });
	}
	return destination;
}
