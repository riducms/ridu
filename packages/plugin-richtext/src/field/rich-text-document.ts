export function initialEditorState(value: unknown) {
	if (!isRecord(value) || value.version !== 1 || !isRecord(value.root)) return null;
	return JSON.stringify({ root: withElementDefaults(value.root) });
}

function withElementDefaults(node: Record<string, unknown>): Record<string, unknown> {
	if (!Array.isArray(node.children)) return node;

	return {
		...node,
		children: node.children.map((child) => (isRecord(child) ? withElementDefaults(child) : child)),
		direction: node.direction ?? null,
		format: node.format ?? "",
		indent: node.indent ?? 0,
	};
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
