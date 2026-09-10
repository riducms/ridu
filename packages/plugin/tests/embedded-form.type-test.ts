import type { FieldAuthoringHost, EmbeddedSchemaFormScope } from "../src";
import type { Snippet } from "svelte";

function renderPayload(host: FieldAuthoringHost, identity: string) {
	const scope: EmbeddedSchemaFormScope = { treeKey: "widgets", identity };
	const renderer: Snippet<[EmbeddedSchemaFormScope]> | undefined = host.schemaForm;
	return { renderer, scope };
}
void renderPayload;

const valid: EmbeddedSchemaFormScope = { treeKey: "cards", identity: "stable", readOnly: true };
// @ts-expect-error A plugin cannot replace the host-resolved schema with arbitrary fields.
const invalid: EmbeddedSchemaFormScope = { treeKey: "cards", identity: "stable", fields: [] };
void valid;
void invalid;
