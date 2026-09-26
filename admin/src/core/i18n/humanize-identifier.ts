export function humanizeIdentifier(value: string, language: string) {
	return value
		.replaceAll(/[_-]+/g, " ")
		.replace(/\b\w/g, (letter) => letter.toLocaleUpperCase(language));
}
