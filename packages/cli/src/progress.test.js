import { afterEach, expect, jest, test } from "bun:test";

import { downloadProgress } from "./progress.js";

afterEach(() => jest.useRealTimers());

function capture(isTTY = false, columns = 80) {
	const chunks = [];
	return {
		output: { isTTY, columns, write: (text) => chunks.push(text) },
		text: () => chunks.join(""),
	};
}

test("non-TTY starts immediately, reports elapsed time during silence, and stops after completion", () => {
	jest.useFakeTimers();
	let now = 0;
	const log = capture();
	const progress = downloadProgress("Ridu v1.2.3", { output: log.output, env: {}, now: () => now });
	expect(log.text()).toContain("Connecting to release server · 0s\n");
	now = 5000;
	jest.advanceTimersByTime(5000);
	expect(log.text()).toContain("Connecting to release server · 5s\n");
	progress.transfer(1024, 2048);
	now = 10000;
	jest.advanceTimersByTime(5000);
	expect(log.text()).toContain("1.0 KiB / 2.0 KiB (50%) · 10s\n");
	progress.stage("Verifying download");
	progress.finish("Ridu CLI ready");
	const done = log.text();
	jest.advanceTimersByTime(10000);
	progress.stage("late response");
	expect(log.text()).toBe(done);
	expect(done).not.toMatch(/[\r\x1b]/);
});

test("TTY animates one bounded line and shows unknown sizes without percentages", () => {
	jest.useFakeTimers();
	const log = capture(true, 48);
	const progress = downloadProgress("Ridu", { output: log.output, env: {} });
	progress.transfer(2 * 1024 * 1024);
	jest.advanceTimersByTime(200);
	expect(log.text()).toContain("Downloading · 2.0 MiB");
	expect(log.text()).not.toContain("%");
	progress.finish("Ridu CLI setup failed", true);
	expect(log.text()).toContain("✗ Ridu CLI setup failed");
	expect(log.text()).toEndWith("\n");
	for (const line of log.text().split("\r\x1b[2K").slice(1)) {
		expect(line.trimEnd().length).toBeLessThan(48);
	}
	const done = log.text();
	jest.advanceTimersByTime(5000);
	expect(log.text()).toBe(done);
});

test.each([{ CI: "true" }, { TERM: "dumb" }, { RIDU_ACCESSIBLE: "1" }, { NO_COLOR: "" }])(
	"plain output preferences work even in a TTY: %j",
	(env) => {
		const log = capture(true);
		const progress = downloadProgress("Ridu", { output: log.output, env });
		progress.finish("Ridu CLI ready");
		expect(log.text()).not.toMatch(/[\r\x1b⠋✓]/);
	}
);
