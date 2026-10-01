const explicitScheme = /^[a-z][a-z\d+.-]*:/iu;
const allowedScheme = /^(?:https?|mailto|tel):/iu;

/** Return the trimmed URL when every Ridu rich-text renderer can expose it safely. */
export function renderableRichTextURL(value: string): string | undefined {
	const url = value.trim();
	if (
		/[\u0000-\u001f\u007f]/u.test(value) ||
		url.startsWith("//") ||
		(explicitScheme.test(url) && !allowedScheme.test(url))
	)
		return undefined;
	return url;
}
