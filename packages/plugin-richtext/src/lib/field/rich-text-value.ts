import { documentRecoveryIssue, type RichTextDocument } from "@riducms/sdk/richtext";

/** Decode a stored field value; embedded payload semantics remain schema-owned. */
export function decodeRichTextDocument(value: unknown): RichTextDocument<unknown> {
	const issue = documentRecoveryIssue(value);
	if (issue !== undefined) throw new Error(`Invalid rich-text document at ${issue}.`);
	return value as RichTextDocument<unknown>;
}

/** Compare JSON field data without treating server object-key ordering as an edit. */
export function equalRichTextValues(left: unknown, right: unknown): boolean {
	if (Object.is(left, right)) return true;
	if (left === null || right === null || typeof left !== "object" || typeof right !== "object")
		return false;
	if (Array.isArray(left))
		return (
			Array.isArray(right) &&
			left.length === right.length &&
			left.every((value, index) => equalRichTextValues(value, right[index]))
		);
	if (Array.isArray(right)) return false;

	const before = left as Record<string, unknown>;
	const after = right as Record<string, unknown>;
	// Undefined optional properties are absent from the serialized wire value.
	const keys = Object.keys(before).filter((key) => before[key] !== undefined);
	return (
		keys.length === Object.keys(after).filter((key) => after[key] !== undefined).length &&
		keys.every((key) => Object.hasOwn(after, key) && equalRichTextValues(before[key], after[key]))
	);
}
