import type { FieldAuthoringHost } from "@riducms/plugin";
import { createContext } from "svelte";

const [getRichTextAuthoringHost, setRichTextAuthoringHost] = createContext<
	FieldAuthoringHost | undefined
>();

export { getRichTextAuthoringHost, setRichTextAuthoringHost };

import type { SchemaField } from "@riducms/protocol";

const [getRichTextField, setRichTextField] = createContext<{
	readonly field: SchemaField;
	/** Acknowledge an ordinary inline field write before Lexical reconciles its decorators. */
	acceptEmbeddedChange(): void;
}>();
export { getRichTextField, setRichTextField };
