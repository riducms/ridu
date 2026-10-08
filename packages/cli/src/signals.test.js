import { afterEach, describe, expect, test } from "bun:test";

import { forwardSignals } from "./signals.js";

function fakeChild() {
	const kills = [];
	return { kills, kill: (signal) => kills.push(signal ?? "default") };
}

let stopForwarding = () => {};
afterEach(() => stopForwarding());

describe("signal forwarding", () => {
	test("leaves the first Windows Ctrl+C to ridu and ends it on the second", () => {
		const child = fakeChild();
		stopForwarding = forwardSignals(child, "win32");
		process.emit("SIGINT");
		expect(child.kills).toEqual([]);
		process.emit("SIGINT");
		expect(child.kills).toEqual(["default"]);
	});

	test("forwards Ctrl+C once on POSIX hosts", () => {
		const child = fakeChild();
		const listeners = process.listenerCount("SIGINT");
		stopForwarding = forwardSignals(child, "linux");
		expect(process.listenerCount("SIGINT")).toBe(listeners + 1);
		process.emit("SIGINT");
		expect(child.kills).toEqual(["SIGINT"]);
		expect(process.listenerCount("SIGINT")).toBe(listeners);
	});

	test("forwards termination and removes every listener when stopped", () => {
		const child = fakeChild();
		const before = [process.listenerCount("SIGINT"), process.listenerCount("SIGTERM")];
		stopForwarding = forwardSignals(child, "win32");
		process.emit("SIGTERM");
		expect(child.kills).toEqual(["SIGTERM"]);
		stopForwarding();
		expect([process.listenerCount("SIGINT"), process.listenerCount("SIGTERM")]).toEqual(before);
	});
});
