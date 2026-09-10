/** Portable envelope exported by the paired Outline fixture. */
export interface OutlineNode<Payload> {
	kind: string;
	items?: OutlineNode<Payload>[] | null;
	content?: Payload;
}
export interface Outline<Payload> {
	outline: OutlineNode<Payload>[];
}
export type OutlineInput<Payload> = Outline<Payload>;
export type OutlineValue = Outline<unknown>;
/** Embedded payload validation belongs to the schema/operation engine. */
export function decodeOutline(value: unknown): OutlineValue {
	if (
		typeof value !== "object" ||
		value === null ||
		!("outline" in value) ||
		!Array.isArray(value.outline)
	)
		throw new Error("Outline requires an outline array.");
	const visit = (node: unknown, depth: number): void => {
		if (
			depth > 64 ||
			typeof node !== "object" ||
			node === null ||
			!("kind" in node) ||
			typeof node.kind !== "string"
		)
			throw new Error("Invalid Outline node.");
		if ("items" in node && node.items != null) {
			if (!Array.isArray(node.items)) throw new Error("Outline children must be an array.");
			node.items.forEach((child: unknown) => visit(child, depth + 1));
		}
	};
	value.outline.forEach((node: unknown) => visit(node, 0));
	return value as OutlineValue;
}
