import type { FieldAuthoringHost } from "@riducms/plugin";
import { createContext } from "svelte";

const [getRichTextAuthoringHost, setRichTextAuthoringHost] = createContext<
	FieldAuthoringHost | undefined
>();

export { getRichTextAuthoringHost, setRichTextAuthoringHost };

import type { FieldDocumentForm } from "@riducms/plugin";
import type { SchemaField } from "@riducms/protocol";

const [getRichTextField, setRichTextField] = createContext<{
	readonly field: SchemaField;
	readonly form: FieldDocumentForm;
}>();
export { getRichTextField, setRichTextField };
