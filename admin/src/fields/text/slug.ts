export function normalizeSlug(value: string) {
	let normalized = "";
	let pendingSeparator = false;
	for (const character of value.trim()) {
		const code = character.charCodeAt(0);
		if (code >= 65 && code <= 90) {
			if (pendingSeparator && normalized.length > 0) normalized += "-";
			normalized += String.fromCharCode(code + 32);
			pendingSeparator = false;
			continue;
		}
		if ((code >= 97 && code <= 122) || (code >= 48 && code <= 57) || character === "_") {
			if (pendingSeparator && normalized.length > 0) normalized += "-";
			normalized += character;
			pendingSeparator = false;
			continue;
		}
		if (character === "-" || isASCIISpace(character)) {
			pendingSeparator = normalized.length > 0;
		}
	}
	return normalized;
}

export function slugFollowsSource(slug: unknown, source: unknown) {
	return String(slug ?? "") === normalizeSlug(String(source ?? ""));
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
