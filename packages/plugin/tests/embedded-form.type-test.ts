import type { FieldAuthoringHost, EmbeddedSchemaFormProps } from "../src/lib";
import type { Snippet } from "svelte";

function renderPayload(host: FieldAuthoringHost, identity: string) {
	const scope: EmbeddedSchemaFormProps = {
		treeKey: "widgets",
		identity,
		onChange(payload) {
			void payload;
		},
	};
	const renderer: Snippet<[EmbeddedSchemaFormProps]> | undefined = host.schemaForm;
	return { renderer, scope };
}
void renderPayload;

const valid: EmbeddedSchemaFormProps = { treeKey: "cards", identity: "stable", readOnly: true };
// @ts-expect-error A plugin cannot replace the host-resolved schema with arbitrary fields.
const invalid: EmbeddedSchemaFormProps = { treeKey: "cards", identity: "stable", fields: [] };
void valid;
void invalid;
