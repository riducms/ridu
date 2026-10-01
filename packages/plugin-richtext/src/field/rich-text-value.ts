import { documentRecoveryIssue, type RichTextDocument } from "@riducms/sdk/richtext";

/** Decode a stored field value; embedded payload semantics remain schema-owned. */
export function decodeRichTextDocument(value: unknown): RichTextDocument<unknown> {
	const issue = documentRecoveryIssue(value);
	if (issue !== undefined) throw new Error(`Invalid rich-text document at ${issue}.`);
	return value as RichTextDocument<unknown>;
}
