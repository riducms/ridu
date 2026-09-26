import { Compartment, EditorState, type Extension } from "@codemirror/state";
import {
	EditorView,
	keymap,
	lineNumbers,
	highlightActiveLineGutter,
	drawSelection,
} from "@codemirror/view";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import {
	bracketMatching,
	HighlightStyle,
	indentOnInput,
	syntaxHighlighting,
} from "@codemirror/language";
import { closeBrackets, closeBracketsKeymap } from "@codemirror/autocomplete";
import { tags } from "@lezer/highlight";

const highlighting = HighlightStyle.define([
	{ tag: [tags.keyword, tags.bool, tags.null], color: "var(--code-keyword)" },
	{ tag: [tags.string, tags.special(tags.string)], color: "var(--code-string)" },
	{ tag: [tags.number, tags.literal], color: "var(--code-number)" },
	{ tag: [tags.propertyName, tags.attributeName], color: "var(--code-property)" },
	{ tag: tags.comment, color: "var(--code-comment)", fontStyle: "italic" },
	{ tag: [tags.typeName, tags.className], color: "var(--code-type)" },
]);

// CodeMirror injects unlayered base CSS. Its theme API overrides those defaults while
// consuming Ridu tokens, so gutters and selection follow the surrounding admin theme.
const editorTheme = EditorView.theme({
	"&": { minHeight: "68px", fontSize: "13px" },
	"&.cm-focused": { outline: "none" },
	".cm-scroller": {
		fontFamily: "var(--font-mono)",
		lineHeight: "20px",
		overflow: "auto",
		maxHeight: "500px",
	},
	".cm-content": { padding: "12px 0", caretColor: "var(--foreground)" },
	".cm-line": { paddingInline: "10px 16px" },
	".cm-gutters": {
		backgroundColor: "transparent",
		color: "var(--foreground-faint)",
		border: "0",
		minWidth: "44px",
	},
	".cm-lineNumbers .cm-gutterElement": { padding: "0 10px" },
	".cm-activeLineGutter": { backgroundColor: "transparent", color: "var(--foreground)" },
	".cm-selectionBackground": { backgroundColor: "var(--control-surface-hover)" },
	"&.cm-focused .cm-selectionBackground": {
		backgroundColor: "color-mix(in srgb, var(--foreground) 20%, transparent)",
	},
	".cm-cursor": { borderLeftColor: "var(--foreground)" },
	".cm-matchingBracket": { outline: "1px solid var(--foreground-faint)" },
});

export interface CodeEditorConfiguration {
	id: string;
	label: string;
	readOnly: boolean;
	invalid: boolean;
	describedBy?: string;
	errorMessage?: string;
}

/** CodeMirror owns its DOM and selection; Svelte owns value, access and lifetime. */
export class CodeEditor {
	readonly view: EditorView;
	readonly #configuration = new Compartment();
	readonly #language = new Compartment();
	readonly #history = new Compartment();
	#languageRequest = 0;
	#destroyed = false;

	constructor(
		parent: HTMLElement,
		value: string,
		configuration: CodeEditorConfiguration,
		onChange: (value: string) => void
	) {
		this.view = new EditorView({
			parent,
			state: EditorState.create({
				doc: value,
				extensions: [
					editorTheme,
					lineNumbers(),
					highlightActiveLineGutter(),
					this.#history.of(history()),
					drawSelection(),
					indentOnInput(),
					bracketMatching(),
					closeBrackets(),
					keymap.of([...closeBracketsKeymap, ...defaultKeymap, ...historyKeymap]),
					syntaxHighlighting(highlighting),
					this.#configuration.of(this.#attributes(configuration)),
					this.#language.of([]),
					EditorView.updateListener.of((update) => {
						if (update.docChanged) onChange(update.state.doc.toString());
					}),
				],
			}),
		});
	}

	configure(configuration: CodeEditorConfiguration) {
		this.view.dispatch({
			effects: this.#configuration.reconfigure(this.#attributes(configuration)),
		});
	}

	async setLanguage(language: string) {
		const request = ++this.#languageRequest;
		const { codeLanguage } = await import("@admin/components/ui/code-editor/code-languages");
		if (this.#destroyed || request !== this.#languageRequest) return;
		const extension = codeLanguage(language);
		this.view.dispatch({ effects: this.#language.reconfigure(extension) });
	}

	setValue(value: string) {
		if (value === this.view.state.doc.toString()) return;
		// An external replacement starts a new editing lifetime, including undo history.
		this.view.dispatch({
			changes: { from: 0, to: this.view.state.doc.length, insert: value },
			effects: this.#history.reconfigure([]),
		});
		this.view.dispatch({ effects: this.#history.reconfigure(history()) });
	}

	destroy() {
		this.#destroyed = true;
		this.view.destroy();
	}

	#attributes(configuration: CodeEditorConfiguration): Extension {
		return [
			EditorState.readOnly.of(configuration.readOnly),
			EditorView.editable.of(!configuration.readOnly),
			EditorView.contentAttributes.of({
				id: configuration.id,
				"aria-label": configuration.label,
				"aria-readonly": String(configuration.readOnly),
				"aria-invalid": String(configuration.invalid),
				...(configuration.describedBy ? { "aria-describedby": configuration.describedBy } : {}),
				...(configuration.errorMessage ? { "aria-errormessage": configuration.errorMessage } : {}),
				spellcheck: "false",
				tabindex: "0",
			}),
		];
	}
}
