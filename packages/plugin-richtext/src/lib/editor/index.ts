import type Editor from "#lib/editor/rich-text-app-editor.svelte";
import { installPrismGlobal } from "#lib/prism.js";

installPrismGlobal();
// Typed explicitly: the declaration emitted for a dynamic import of a component drops its props.
const RichTextEditor: typeof Editor = (await import("#lib/editor/rich-text-app-editor.svelte"))
	.default;

export { RichTextEditor };
export type { RichTextMessageKey, RichTextMessages } from "#lib/editor/rich-text-editor-i18n.js";
export type { RichTextEditorFeature, RichTextToolbar } from "#lib/field/rich-text-config.js";
