import type { AdminI18n } from "@riducms/plugin";
import type { Klass, LexicalEditor, LexicalNode } from "lexical";
import type { Snippet } from "svelte";

import type { RichTextMenuOption } from "#lib/menu/rich-text-options.js";

/** What an extension's plugin can see about the editor while it renders. */
export interface RichTextEditorExtensionState {
	/** Editing is blocked, for example while the document saves or when it is read-only. */
	readonly locked: boolean;
}

/**
 * One feature the core editor doesn't own, such as uploads, relationships or blocks in the
 * admin. The editor registers the nodes when it mounts, adds the options to the slash menu and
 * the block handle's menu, and renders the plugin inside the editor after its own plugins.
 */
export interface RichTextEditorExtension {
	/** Lexical nodes this extension's content uses. */
	readonly nodes?: readonly Klass<LexicalNode>[];
	/** Insert options for the slash menu and the block handle's menu. */
	readonly options?: (editor: LexicalEditor, i18n: AdminI18n) => readonly RichTextMenuOption[];
	/** Commands, pickers and other behaviour, rendered inside the editor. */
	readonly plugin?: Snippet<[RichTextEditorExtensionState]>;
}
