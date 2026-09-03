import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";

import { describe, expect, test } from "bun:test";

import {
	archiveName,
	expectedChecksum,
	extractTarGzipBinary,
	releaseTarget,
	verifyChecksum,
} from "./launcher.js";

describe("native release selection", () => {
	test("maps npm platforms to coordinated release assets", () => {
		expect(archiveName("1.2.3", releaseTarget("darwin", "arm64"))).toBe(
			"ridu_1.2.3_darwin_arm64.tar.gz"
		);
		expect(archiveName("1.2.3", releaseTarget("win32", "x64"))).toBe(
			"ridu_1.2.3_windows_amd64.zip"
		);
		expect(() => releaseTarget("freebsd", "x64")).toThrow("install it with Go");
	});

	test("requires the exact release checksum", () => {
		const bytes = Buffer.from("ridu");
		const checksum = createHash("sha256").update(bytes).digest("hex");
		expect(
			expectedChecksum(
				`${checksum}  ridu_1.2.3_linux_amd64.tar.gz\n`,
				"ridu_1.2.3_linux_amd64.tar.gz"
			)
		).toBe(checksum);
		expect(() => verifyChecksum(Buffer.from("other"), checksum, "archive")).toThrow(
			"checksum mismatch"
		);
	});
});

test("extracts the native binary from a release-shaped tarball", () => {
	const content = Buffer.from("native-ridu");
	const header = Buffer.alloc(512);
	header.write("ridu", 0, "utf8");
	header.write(content.length.toString(8).padStart(11, "0") + "\0", 124, "ascii");
	header[156] = "0".charCodeAt(0);
	const padding = Buffer.alloc(Math.ceil(content.length / 512) * 512 - content.length);
	const archive = gzipSync(Buffer.concat([header, content, padding, Buffer.alloc(1024)]));
	expect(extractTarGzipBinary(archive).toString("utf8")).toBe("native-ridu");
});
