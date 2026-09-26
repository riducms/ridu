interface FilterableRichTextOption {
	label: string;
	description: string;
	keywords: readonly string[];
}

export function filterRichTextOptions<Option extends FilterableRichTextOption>(
	options: readonly Option[],
	query: string,
	language?: string
): Option[] {
	const normalized = query.trim().toLocaleLowerCase(language);
	if (normalized === "") return [...options];
	const compact = normalized.replace(/[\s\-_]/g, "");
	const matches = (value: string) => {
		const lower = value.toLocaleLowerCase(language);
		return (
			lower.includes(normalized) ||
			(compact !== "" && lower.replace(/[\s\-_]/g, "").includes(compact))
		);
	};
	const exactLabel = (option: Option) =>
		option.label.toLocaleLowerCase(language).replace(/[\s\-_]/g, "") === compact;
	return options
		.filter(
			(option) =>
				matches(option.label) || matches(option.description) || option.keywords.some(matches)
		)
		.sort((a, b) => Number(exactLabel(b)) - Number(exactLabel(a)));
}
