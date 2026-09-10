import { createHash, randomBytes } from "node:crypto";
import { createServer } from "node:http";
import { gzipSync } from "node:zlib";

import { archiveName, releaseTarget } from "../src/launcher.js";

// A local archive for download tests/previews. Never executes the real CLI or creates a project.
export function releaseFixture(version, padding = 0) {
	const filename = archiveName(version, releaseTarget());
	const binary = Buffer.from('#!/bin/sh\nprintf "%s\\n" "$@"\nexit 7\n');
	const content = padding ? Buffer.concat([binary, randomBytes(padding)]) : binary;
	const header = Buffer.alloc(512);
	header.write("ridu");
	header.write(content.length.toString(8).padStart(11, "0") + "\0", 124, "ascii");
	header[156] = "0".charCodeAt(0);
	const archive = gzipSync(
		Buffer.concat([
			header,
			content,
			Buffer.alloc(Math.ceil(content.length / 512) * 512 - content.length + 1024),
		])
	);
	const checksums = `${createHash("sha256").update(archive).digest("hex")}  ${filename}\n`;
	return { archive, checksums, filename };
}

export async function releaseServer(handler) {
	const server = createServer(handler);
	const sockets = new Set();
	server.on("connection", (socket) => {
		sockets.add(socket);
		socket.once("close", () => sockets.delete(socket));
	});
	await new Promise((resolve, reject) => {
		server.once("error", reject);
		server.listen(0, "127.0.0.1", resolve);
	});
	return {
		url: `http://127.0.0.1:${server.address().port}`,
		async close() {
			await new Promise((resolve) => {
				server.close(resolve);
				for (const socket of sockets) socket.destroy();
			});
		},
	};
}
