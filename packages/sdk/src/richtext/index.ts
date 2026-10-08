/** Framework-free rich-text documents, synchronous HTML rendering and plain-text conversion. */
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
export { convertLexicalToPlaintext, type RichTextPlaintextOptions } from "./plaintext.js";
export { documentRecoveryIssue } from "./document-validation.js";
