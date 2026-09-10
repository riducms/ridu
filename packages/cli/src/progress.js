const frames = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];

function bytes(value) {
	if (value < 1024) return `${value} B`;
	if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`;
	return `${(value / (1024 * 1024)).toFixed(1)} MiB`;
}

// All launcher diagnostics go to stderr; stdout belongs to the native command.
export function downloadProgress(
	label,
	{ output = process.stderr, env = process.env, now = () => performance.now() } = {}
) {
	const interactive = Boolean(
		output.isTTY &&
		!env.CI &&
		env.TERM !== "dumb" &&
		env.RIDU_ACCESSIBLE !== "1" &&
		!("NO_COLOR" in env)
	);
	const started = now();
	let message = "Connecting to release server";
	let frame = 0;
	let finished = false;
	const elapsed = () => `${Math.floor((now() - started) / 1000)}s`;
	const line = (symbol) => {
		const text = `  ${symbol} ${message} · ${elapsed()}`;
		// Avoid wrapping an animated line and leaving stale terminal rows behind.
		const width = Math.max(1, (output.columns || 80) - 1);
		output.write(interactive ? `\r\x1b[2K${text.slice(0, width)}` : `${text}\n`);
	};
	output.write(`Downloading ${label}\n`);
	line(interactive ? frames[frame++] : "-");
	const timer = setInterval(
		() => line(interactive ? frames[frame++ % frames.length] : "-"),
		interactive ? 100 : 5000
	);
	timer.unref?.();
	return {
		transfer(received, total) {
			if (finished) return;
			message =
				total === undefined
					? `Downloading · ${bytes(received)}`
					: `Downloading · ${bytes(received)} / ${bytes(total)} (${Math.floor((received / total) * 100)}%)`;
		},
		stage(value) {
			if (finished) return;
			message = value;
			line(interactive ? frames[frame++ % frames.length] : "-");
		},
		finish(value, failed = false) {
			if (finished) return;
			finished = true;
			clearInterval(timer);
			message = value;
			line(interactive ? (failed ? "✗" : "✓") : failed ? "!" : "-");
			if (interactive) output.write("\n");
		},
	};
}
