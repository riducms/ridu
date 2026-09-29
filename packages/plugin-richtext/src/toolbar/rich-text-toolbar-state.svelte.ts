import type { ElementFormatType, LexicalEditor } from "lexical";

import {
	alignText,
	changeBlockType,
	changeIndent,
	emptyToolbarSelection,
	formatText,
	readToolbarSelection,
	sameToolbarSelection,
	toggleLink,
	type RichTextBlockType,
	type RichTextTextFormat,
	type RichTextToolbarSelection,
} from "@plugin-richtext/toolbar/rich-text-toolbar-selection";

/**
 * Selection state and commands shared by an editor's fixed and floating toolbars, so both read
 * one subscription. The owner calls `connect` from an effect and returns its cleanup.
 */
export class RichTextToolbarState {
	readonly #editor: LexicalEditor;
	// Plain copy for comparison, so connecting from an effect does not track the rendered state.
	#current = emptyToolbarSelection;
	#selection = $state.raw<RichTextToolbarSelection>(emptyToolbarSelection);

	constructor(editor: LexicalEditor) {
		this.#editor = editor;
	}

	get selection() {
		return this.#selection;
	}

	connect = () => {
		this.#refresh(readToolbarSelection(this.#editor));
		return this.#editor.registerUpdateListener(({ editorState }) =>
			this.#refresh(readToolbarSelection(this.#editor, editorState))
		);
	};

	formatText = (format: RichTextTextFormat) =>
		this.#withSelection(() => formatText(this.#editor, format));

	changeBlockType = (type: RichTextBlockType) =>
		this.#withSelection(() => changeBlockType(this.#editor, type));

	alignText = (format: ElementFormatType) =>
		this.#withSelection(() => alignText(this.#editor, format));

	indent = () => this.#withSelection(() => changeIndent(this.#editor, "indent"));

	outdent = () => this.#withSelection(() => changeIndent(this.#editor, "outdent"));

	toggleLink = () => toggleLink(this.#editor, this.#current.link);

	focusEditor = () => this.#editor.focus();

	#refresh(next: RichTextToolbarSelection) {
		if (sameToolbarSelection(this.#current, next)) return;
		this.#current = next;
		this.#selection = next;
	}

	// A fixed toolbar can be used before the editor has a selection; place the caret at the end.
	#withSelection(command: () => void) {
		if (this.#current.kind !== "none") command();
		else this.#editor.focus(command, { defaultSelection: "rootEnd" });
	}
}
