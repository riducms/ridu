import { autocompletion, closeBrackets, closeBracketsKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { json } from "@codemirror/lang-json";
import {
	bracketMatching,
	HighlightStyle,
	indentOnInput,
	syntaxHighlighting,
} from "@codemirror/language";
import { lintGutter } from "@codemirror/lint";
import { Compartment, EditorState, type Extension } from "@codemirror/state";
import {
	drawSelection,
	EditorView,
	highlightActiveLineGutter,
	keymap,
	lineNumbers,
	placeholder,
} from "@codemirror/view";
import { tags } from "@lezer/highlight";
import { graphql, updateSchema } from "cm6-graphql";
import type { GraphQLSchema } from "graphql";

// Matches the admin's code editor so the playground reads as part of the same interface.
const highlighting = HighlightStyle.define([
	{ tag: [tags.keyword, tags.bool, tags.null], color: "var(--code-keyword)" },
	{ tag: [tags.string, tags.special(tags.string)], color: "var(--code-string)" },
	{ tag: [tags.number, tags.literal], color: "var(--code-number)" },
	{ tag: [tags.propertyName, tags.attributeName], color: "var(--code-property)" },
	{ tag: tags.comment, color: "var(--code-comment)", fontStyle: "italic" },
	{ tag: [tags.typeName, tags.className], color: "var(--code-type)" },
	{ tag: [tags.variableName, tags.definition(tags.variableName)], color: "var(--syntax-variable)" },
]);

// CodeMirror injects unlayered base CSS; its theme API overrides those defaults with Ridu tokens.
const theme = EditorView.theme({
	"&": { height: "100%", fontSize: "13px", backgroundColor: "transparent" },
	"&.cm-focused": { outline: "none" },
	".cm-scroller": { fontFamily: "var(--font-mono)", lineHeight: "20px", overflow: "auto" },
	".cm-content": { padding: "12px 0", caretColor: "var(--foreground)" },
	".cm-line": { paddingInline: "10px 16px" },
	// The gutter stays in place while long lines scroll sideways, so it must be opaque.
	".cm-gutters": {
		backgroundColor: "var(--background-surface, var(--card))",
		color: "var(--foreground-faint, var(--muted-foreground))",
		border: "0",
	},
	".cm-lineNumbers .cm-gutterElement": { padding: "0 8px 0 12px" },
	".cm-activeLineGutter": { backgroundColor: "transparent", color: "var(--foreground)" },
	".cm-selectionBackground": { backgroundColor: "var(--control-surface-hover)" },
	"&.cm-focused .cm-selectionBackground": {
		backgroundColor: "color-mix(in srgb, var(--foreground) 20%, transparent)",
	},
	".cm-cursor": { borderLeftColor: "var(--foreground)" },
	".cm-matchingBracket": { outline: "1px solid var(--muted-foreground)" },
	".cm-placeholder": { color: "var(--muted-foreground)" },
	".cm-tooltip": {
		backgroundColor: "var(--popover)",
		color: "var(--popover-foreground)",
		border: "1px solid var(--border)",
		borderRadius: "var(--radius)",
		boxShadow: "var(--shadow-popover)",
	},
	".cm-tooltip-autocomplete > ul > li[aria-selected]": {
		backgroundColor: "var(--control-surface-hover)",
		color: "var(--foreground)",
	},
	".cm-diagnostic-error": { borderLeftColor: "var(--destructive)" },
});

interface EditorOptions {
	doc: string;
	label: string;
	onChange?: (value: string) => void;
	/** Runs the current operation; bound to Ctrl/⌘+Enter in every playground editor. */
	onRun?: () => void;
	placeholder?: string;
}

interface EditorPresentation {
	label: string;
	placeholder?: string;
}

const editorConfigurations = new WeakMap<
	EditorView,
	{ compartment: Compartment; readOnly: boolean }
>();

function presentationExtensions(options: EditorPresentation, readOnly: boolean): Extension {
	return [
		EditorState.readOnly.of(readOnly),
		EditorView.editable.of(!readOnly),
		EditorView.contentAttributes.of({
			"aria-label": options.label,
			"aria-readonly": String(readOnly),
			spellcheck: "false",
			...(readOnly ? { tabindex: "0" } : {}),
		}),
		...(options.placeholder ? [placeholder(options.placeholder)] : []),
	];
}

function baseExtensions(
	options: EditorOptions,
	readOnly: boolean,
	configuration: Compartment
): Extension[] {
	const run = options.onRun;
	return [
		theme,
		syntaxHighlighting(highlighting),
		lineNumbers(),
		highlightActiveLineGutter(),
		drawSelection(),
		bracketMatching(),
		configuration.of(presentationExtensions(options, readOnly)),
		keymap.of([
			...(run
				? [
						{
							key: "Mod-Enter",
							run: () => {
								run();
								return true;
							},
						},
					]
				: []),
		]),
		...(readOnly
			? []
			: [
					history(),
					indentOnInput(),
					closeBrackets(),
					keymap.of([...closeBracketsKeymap, ...defaultKeymap, ...historyKeymap]),
				]),
		EditorView.updateListener.of((update) => {
			if (update.docChanged) options.onChange?.(update.state.doc.toString());
		}),
	];
}

export interface QueryEditorOptions extends EditorOptions {
	schema: GraphQLSchema | undefined;
	/** Opens the docs explorer at a type, from Ctrl/⌘+click or a completion's details. */
	onShowInDocs: (type: string) => void;
}

/** The GraphQL editor: schema-aware completion, validation, and jump-to-docs. */
export function createQueryEditor(parent: HTMLElement, options: QueryEditorOptions) {
	const configuration = new Compartment();
	const view = new EditorView({
		parent,
		state: EditorState.create({
			doc: options.doc,
			extensions: [
				...baseExtensions(options, false, configuration),
				graphql(options.schema, {
					onShowInDocs: (field, type, parentType) => {
						const target = parentType ?? type ?? field;
						if (target) options.onShowInDocs(target);
					},
				}),
				autocompletion({ activateOnTyping: true }),
				lintGutter(),
			],
		}),
	});
	editorConfigurations.set(view, { compartment: configuration, readOnly: false });
	return view;
}

export function setQuerySchema(view: EditorView, schema: GraphQLSchema | undefined) {
	updateSchema(view, schema);
}

/** A JSON editor for variables, or a read-only viewer for responses. */
export function createJSONEditor(
	parent: HTMLElement,
	options: EditorOptions & { readOnly: boolean }
) {
	const configuration = new Compartment();
	const view = new EditorView({
		parent,
		state: EditorState.create({
			doc: options.doc,
			extensions: [...baseExtensions(options, options.readOnly, configuration), json()],
		}),
	});
	editorConfigurations.set(view, { compartment: configuration, readOnly: options.readOnly });
	return view;
}

/** Updates translated editor chrome without recreating the editor or losing its history. */
export function configureEditor(view: EditorView, options: EditorPresentation) {
	const configuration = editorConfigurations.get(view);
	if (configuration === undefined) return;
	view.dispatch({
		effects: configuration.compartment.reconfigure(
			presentationExtensions(options, configuration.readOnly)
		),
	});
}

/** Replaces the whole document when an externally owned value changes. */
export function replaceDocument(view: EditorView, text: string) {
	if (view.state.doc.toString() === text) return;
	view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } });
}
