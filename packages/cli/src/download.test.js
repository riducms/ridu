import { gzipSync } from "node:zlib";

import { expect, test } from "bun:test";

import { releaseServer } from "../tests/release-fixture.js";
import { download, downloadTimeout } from "./download.js";

test.each([
	[{}, undefined],
	[{ "Content-Length": "4" }, 4],
	[{ "Content-Encoding": "gzip" }, undefined],
])(
	"reports real received bytes, with a total only for comparable Content-Length: %j",
	async (headers, total) => {
		const content = Buffer.from("ridu");
		const server = await releaseServer((_request, response) => {
			const body = headers["Content-Encoding"] ? gzipSync(content) : content;
			response.writeHead(
				200,
				headers["Content-Encoding"] ? { ...headers, "Content-Length": body.length } : headers
			);
			response.write(body);
			response.end();
		});
		const updates = [];
		try {
			const result = await download(server.url, {
				signal: new AbortController().signal,
				timeoutMs: 1000,
				onProgress: (received, size) => updates.push([received, size]),
			});
			expect(result).toEqual(content);
			expect(updates.at(-1)).toEqual([4, total]);
		} finally {
			await server.close();
		}
	}
);

test.each(["headers", "body"])(
	"times out a stalled %s with the URL and recovery instructions",
	async (stage) => {
		const server = await releaseServer((_request, response) => {
			if (stage === "body") {
				response.writeHead(200);
				response.write("partial");
			}
		});
		try {
			await expect(
				download(server.url, {
					signal: new AbortController().signal,
					timeoutMs: 50,
				})
			).rejects.toThrow(
				`Could not download ${server.url}: No response or download data for 0.05s. Retry, or increase RIDU_CLI_DOWNLOAD_TIMEOUT_MS`
			);
		} finally {
			await server.close();
		}
	}
);

test("HTTP failure retains the status, release URL and installed-binary workaround", async () => {
	const server = await releaseServer((_request, response) => response.writeHead(404).end());
	try {
		await expect(
			download(server.url, {
				signal: new AbortController().signal,
				timeoutMs: 1000,
			})
		).rejects.toThrow(
			`Could not download ${server.url}: HTTP 404 Not Found.\nCheck your connection and access to the release URL, then retry. You can also set RIDU_BINARY`
		);
	} finally {
		await server.close();
	}
});

test("network errors retain actionable context", async () => {
	const server = await releaseServer(() => {});
	await server.close();
	await expect(
		download(server.url, {
			signal: new AbortController().signal,
			timeoutMs: 1000,
		})
	).rejects.toThrow("Check your connection and access to the release URL, then retry.");
});

test("download timeout rejects invalid and overflowing timer values", () => {
	expect(downloadTimeout("30000")).toBe(30000);
	for (const value of ["", "0", "-1", "1.5", "Infinity", "2147483648", "slow"]) {
		expect(() => downloadTimeout(value)).toThrow("positive integer in milliseconds");
	}
});
