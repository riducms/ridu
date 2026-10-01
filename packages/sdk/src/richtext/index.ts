/** Framework-free rich-text documents and synchronous HTML rendering. */
export type {
	RichTextDocument,
	RichTextDocumentInput,
	RichTextElementFormat,
	RichTextRootNode,
	RichTextBlockNode,
	RichTextTextNode,
	RichTextElementNode,
	RichTextReferenceNode,
	RichTextNode,
	RichTextBlockRenderers,
} from "./document.js";
export {
	escapeRichText,
	safeRichTextURL,
	renderRichTextText,
	richTextElementTag,
	renderRichTextHTML,
	type RichTextRenderOptions,
} from "./render.js";
export { documentRecoveryIssue } from "./document-validation.js";
