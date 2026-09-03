import type { FieldAuthoringHost } from "@riducms/plugin";
import { createContext } from "svelte";

const [getRichTextAuthoringHost, setRichTextAuthoringHost] = createContext<
	FieldAuthoringHost | undefined
>();

export { getRichTextAuthoringHost, setRichTextAuthoringHost };
