/**
 * Removes template-literal indentation without changing the example's
 * relative nesting. Tabs are expanded after the common indentation is
 * removed so Shiki, clipboard output, and plain-text tests see the same code.
 */
export function normalizeSource(value: string, tabWidth = 2): string {
	if (!Number.isInteger(tabWidth) || tabWidth < 1) {
		throw new RangeError('tabWidth must be a positive integer');
	}

	const lines = value.replace(/\r\n?/g, '\n').split('\n');
	while (lines.length > 0 && lines[0].trim() === '') lines.shift();
	while (lines.length > 0 && lines.at(-1)?.trim() === '') lines.pop();

	const indentWidth = (line: string): number => {
		let width = 0;
		for (const character of line.match(/^[\t ]*/)?.[0] ?? '') {
			width += character === '\t' ? tabWidth - (width % tabWidth) : 1;
		}
		return width;
	};

	const indents = lines.filter((line) => line.trim() !== '').map(indentWidth);
	const commonIndent = indents.length > 0 ? Math.min(...indents) : 0;

	return lines
		.map((line) => {
			let column = 0;
			let index = 0;
			let partialTab = '';

			while (index < line.length && column < commonIndent) {
				if (line[index] === ' ') {
					column += 1;
				} else if (line[index] === '\t') {
					const tabColumns = tabWidth - (column % tabWidth);
					const nextColumn = column + tabColumns;
					if (nextColumn > commonIndent) {
						partialTab = ' '.repeat(nextColumn - commonIndent);
					}
					column = nextColumn;
				} else {
					break;
				}
				index += 1;
			}

			const remainder = `${partialTab}${line.slice(index)}`;
			const leading = remainder.match(/^[\t ]*/)?.[0] ?? '';
			let expanded = '';
			let expandedWidth = 0;

			for (const character of leading) {
				const spaces = character === '\t' ? tabWidth - (expandedWidth % tabWidth) : 1;
				expanded += ' '.repeat(spaces);
				expandedWidth += spaces;
			}

			return `${expanded}${remainder.slice(leading.length)}`.trimEnd();
		})
		.join('\n');
}
