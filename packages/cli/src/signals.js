/**
 * Pass Ctrl+C and termination on to the native ridu process, and return a function that stops.
 *
 * On Windows, ridu shares the console and receives Ctrl+C itself. Node and Bun turn
 * `child.kill()` into TerminateProcess there, which would end ridu before it stops the servers
 * it manages. The first Ctrl+C therefore only waits for ridu; a second one ends it at once.
 */
export function forwardSignals(child, platform = process.platform) {
	const listeners = [];
	function listen(signal, listener, repeat) {
		listeners.push([signal, listener]);
		if (repeat) process.on(signal, listener);
		else process.once(signal, listener);
	}
	if (platform === "win32") {
		let interrupted = false;
		listen(
			"SIGINT",
			() => {
				if (interrupted) child.kill();
				interrupted = true;
			},
			true
		);
	} else {
		listen("SIGINT", () => child.kill("SIGINT"), false);
	}
	listen("SIGTERM", () => child.kill("SIGTERM"), false);
	return () => {
		for (const [signal, listener] of listeners) process.off(signal, listener);
	};
}
