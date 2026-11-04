/**
 * Convert a source or manual value into Ridu's URL-segment form, exactly as the server's
 * `field.NormalizeSlug` stores it: ASCII letters are lowercased, digits and underscores are
 * kept, runs of whitespace and hyphens become one hyphen, and every other character is
 * removed. Use it to preview a slug before the server derives it.
 */
export function normalizeSlug(value: string): string {
	let normalized = "";
	let pendingSeparator = false;
	for (const character of value) {
		const code = character.charCodeAt(0);
		if (code >= 65 && code <= 90) {
			if (pendingSeparator && normalized.length > 0) normalized += "-";
			normalized += String.fromCharCode(code + 32);
			pendingSeparator = false;
		} else if ((code >= 97 && code <= 122) || (code >= 48 && code <= 57) || character === "_") {
			if (pendingSeparator && normalized.length > 0) normalized += "-";
			normalized += character;
			pendingSeparator = false;
		} else if (character === "-" || isASCIISpace(character)) {
			pendingSeparator = normalized.length > 0;
		}
	}
	return normalized;
}

function isASCIISpace(character: string) {
	return (
		character === " " ||
		character === "\t" ||
		character === "\n" ||
		character === "\r" ||
		character === "\f" ||
		character === "\v"
	);
}
