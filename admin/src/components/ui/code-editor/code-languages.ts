import type { Extension } from "@codemirror/state";
import { json } from "@codemirror/lang-json";
import { javascript } from "@codemirror/lang-javascript";
import { css } from "@codemirror/lang-css";
import { html } from "@codemirror/lang-html";

export function codeLanguage(language: string): Extension {
	switch (language.toLowerCase()) {
		case "json":
			return json();
		case "javascript":
		case "js":
		case "jsx":
			return javascript({ jsx: true });
		case "typescript":
		case "ts":
		case "tsx":
			return javascript({
				typescript: true,
				jsx: true,
			});
		case "css":
			return css();
		case "html":
			return html();
		default:
			return [];
	}
}
