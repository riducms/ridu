/** Validates the explicit settings selected for a static component before decoding. */
export function decodeComponentConfig(
	selection: { config?: unknown } | undefined,
	decoder: ((value: unknown) => unknown) | undefined
): unknown {
	const supplied = selection !== undefined && Object.hasOwn(selection, "config");
	if (!supplied) {
		if (decoder !== undefined)
			throw new Error("config is required by decodeConfig; supply a configuration object in Go");
		return undefined;
	}
	if (decoder === undefined)
		throw new Error(
			"config was supplied but there is no decodeConfig; remove the Go configuration or register a decoder"
		);
	if (typeof decoder !== "function") throw new Error("decodeConfig must be a synchronous function");
	const raw = selection.config;
	if (typeof raw !== "object" || raw === null || Array.isArray(raw))
		throw new Error("supplied config must be a finite JSON object");
	const seen = new Set<object>();
	const copy = (value: unknown, depth: number): unknown => {
		if (depth > 100) throw new Error("config exceeds the supported JSON depth");
		if (value === null || typeof value === "string" || typeof value === "boolean") return value;
		if (typeof value === "number" && Number.isFinite(value)) return value;
		if (
			typeof value !== "object" ||
			(!Array.isArray(value) &&
				Object.getPrototypeOf(value) !== Object.prototype &&
				Object.getPrototypeOf(value) !== null) ||
			seen.has(value)
		)
			throw new Error("config must contain finite, acyclic JSON data");
		seen.add(value);
		const result = Array.isArray(value)
			? Array.from(value, (child) => copy(child, depth + 1))
			: Object.fromEntries(
					Object.entries(value).map(([key, child]) => [key, copy(child, depth + 1)])
				);
		seen.delete(value);
		return result;
	};
	const decoded = decoder(copy(raw, 0));
	if (
		decoded !== null &&
		(typeof decoded === "object" || typeof decoded === "function") &&
		"then" in decoded &&
		typeof decoded.then === "function"
	)
		throw new Error("config decoders must be synchronous");
	return decoded;
}
