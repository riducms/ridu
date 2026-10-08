import { formatUrl } from "@lexical/link";
import { safeRichTextURL } from "@riducms/sdk/richtext";

const relativeURL = /^(?:\/|\.|#)/;

/** Normalize an author-entered URL while rejecting schemes browsers could execute. */
export function normalizeLinkURL(value: string): string | undefined {
	const trimmed = value.trim();
	if (trimmed === "" || /[\u0000-\u001f\u007f\s]/.test(trimmed)) return undefined;

	const normalized = formatUrl(trimmed);
	try {
		const safeURL = safeRichTextURL(normalized);
		const parsed = new URL(safeURL, "https://ridu.invalid");
		if (
			!relativeURL.test(safeURL) &&
			(parsed.protocol === "http:" || parsed.protocol === "https:") &&
			parsed.hostname === ""
		)
			return undefined;
		if ((parsed.protocol === "mailto:" || parsed.protocol === "tel:") && parsed.pathname === "")
			return undefined;
		return safeURL;
	} catch {
		return undefined;
	}
}
