import { formatUrl } from "@lexical/link";
import { renderableRichTextURL } from "@plugin-richtext/link-url-policy";

const relativeURL = /^(?:\/|\.|#)/;

/** Normalize an author-entered URL while rejecting schemes browsers could execute. */
export function normalizeLinkURL(value: string): string | undefined {
	const trimmed = value.trim();
	if (trimmed === "" || /[\u0000-\u001f\u007f\s]/.test(trimmed)) return undefined;

	const normalized = formatUrl(trimmed);
	const safeURL = renderableRichTextURL(normalized);
	if (safeURL === undefined) return undefined;

	try {
		const parsed = new URL(safeURL, "https://ridu.invalid");
		if (
			!relativeURL.test(safeURL) &&
			(parsed.protocol === "http:" || parsed.protocol === "https:") &&
			parsed.hostname === ""
		)
			return undefined;
		if ((parsed.protocol === "mailto:" || parsed.protocol === "tel:") && parsed.pathname === "")
			return undefined;
	} catch {
		return undefined;
	}

	return safeURL;
}
