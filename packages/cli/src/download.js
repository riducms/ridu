export function downloadTimeout(value = process.env.RIDU_CLI_DOWNLOAD_TIMEOUT_MS ?? "30000") {
	const timeout = Number(value);
	if (
		!/^\d+$/.test(value) ||
		!Number.isSafeInteger(timeout) ||
		timeout < 1 ||
		timeout > 2147483647
	) {
		throw new Error(
			"RIDU_CLI_DOWNLOAD_TIMEOUT_MS must be a positive integer in milliseconds (at most 2147483647)"
		);
	}
	return timeout;
}

export async function download(url, { signal, timeoutMs, onProgress = () => {} }) {
	const controller = new AbortController();
	const abort = () => controller.abort(signal.reason);
	if (signal.aborted) abort();
	else signal.addEventListener("abort", abort, { once: true });
	let timer;
	const resetTimeout = () => {
		clearTimeout(timer);
		timer = setTimeout(
			() =>
				controller.abort(
					new Error(
						`No response or download data for ${timeoutMs / 1000}s. Retry, or increase RIDU_CLI_DOWNLOAD_TIMEOUT_MS for a slow connection`
					)
				),
			timeoutMs
		);
	};
	resetTimeout();
	try {
		const response = await fetch(url, {
			headers: { "user-agent": "@riducms/cli" },
			redirect: "follow",
			signal: controller.signal,
		});
		if (!response.ok) {
			throw new Error(`HTTP ${response.status} ${response.statusText}`.trim());
		}
		if (!response.body) throw new Error("The release server returned an empty response body");
		const length = response.headers.get("content-length");
		const encoding = response.headers.get("content-encoding");
		// fetch decodes Content-Encoding, so its Content-Length can describe different bytes.
		let total =
			(!encoding || encoding === "identity") &&
			/^\d+$/.test(length ?? "") &&
			Number.isSafeInteger(Number(length)) &&
			Number(length) > 0
				? Number(length)
				: undefined;
		let received = 0;
		const chunks = [];
		onProgress(received, total);
		resetTimeout();
		for await (const chunk of response.body) {
			resetTimeout();
			chunks.push(chunk);
			received += chunk.byteLength;
			if (total !== undefined && received > total) total = undefined;
			onProgress(received, total);
		}
		return Buffer.concat(chunks, received);
	} catch (error) {
		const reason = controller.signal.aborted ? controller.signal.reason : error;
		const detail = reason instanceof Error ? reason.message : String(reason);
		const code = reason?.cause?.code ?? reason?.code;
		throw new Error(
			`Could not download ${url}: ${detail}${code ? ` (${code})` : ""}.\n` +
				"Check your connection and access to the release URL, then retry. " +
				"You can also set RIDU_BINARY to an already-installed Ridu binary.",
			{ cause: error }
		);
	} finally {
		clearTimeout(timer);
		signal.removeEventListener("abort", abort);
		controller.abort();
	}
}
