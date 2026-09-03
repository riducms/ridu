export type CodeLineMarker = '+' | '−' | null;

export interface CodeLineAnnotation {
	/** One-based source line number. */
	lineNumber: number;
	/** Classes to append to Shiki's existing `line` span. */
	classes: string[];
	/** A visible gutter marker, when the annotation calls for one. */
	marker: CodeLineMarker;
}

type AnnotationKind = 'add' | 'remove' | 'highlight';
type AnnotationDirective = AnnotationKind | 'focus';

const annotationClasses: Record<AnnotationKind, string> = {
	add: 'is-added',
	remove: 'is-removed',
	highlight: 'is-highlighted'
};

interface ParsedAnnotationDirectives {
	diagnostics: string[];
	directives: Array<{ kind: AnnotationKind; value: string }>;
}

function skipQuotedValue(meta: string, start: number): number {
	const quote = meta[start];
	let index = start + 1;

	while (index < meta.length) {
		if (meta[index] === '\\') {
			index += 2;
			continue;
		}
		if (meta[index] === quote) return index + 1;
		index += 1;
	}

	return meta.length;
}

function readBracedValue(meta: string, start: number): { next: number; value: string } | null {
	let depth = 1;
	let index = start + 1;

	while (index < meta.length) {
		if (meta[index] === '"' || meta[index] === "'") {
			index = skipQuotedValue(meta, index);
			continue;
		}
		if (meta[index] === '{') depth += 1;
		if (meta[index] === '}') {
			depth -= 1;
			if (depth === 0) {
				return { next: index + 1, value: meta.slice(start + 1, index) };
			}
		}
		index += 1;
	}

	return null;
}

function readAnnotationDirectives(meta: string): ParsedAnnotationDirectives {
	const directives: Array<{ kind: AnnotationKind; value: string }> = [];
	const diagnostics: string[] = [];
	let index = 0;

	while (index < meta.length) {
		while (index < meta.length && /[\s,]/.test(meta[index])) {
			index += 1;
		}
		if (index >= meta.length) break;

		if (meta[index] === '"' || meta[index] === "'") {
			index = skipQuotedValue(meta, index);
			continue;
		}

		const nameStart = index;
		while (index < meta.length && /[A-Za-z0-9_-]/.test(meta[index])) index += 1;
		if (nameStart === index) {
			index += 1;
			continue;
		}

		const name = meta.slice(nameStart, index);
		while (index < meta.length && /\s/.test(meta[index])) index += 1;
		if (meta[index] !== '=') continue;

		index += 1;
		while (index < meta.length && /\s/.test(meta[index])) index += 1;

		if (meta[index] === '"' || meta[index] === "'") {
			index = skipQuotedValue(meta, index);
			continue;
		}

		const annotationName =
			name === 'add' || name === 'remove' || name === 'highlight' || name === 'focus';
		if (meta[index] !== '{') {
			if (annotationName) diagnostics.push(`${name} must use a braced line selector`);
			while (index < meta.length && !/[\s,]/.test(meta[index])) index += 1;
			continue;
		}

		const bracedValue = readBracedValue(meta, index);
		if (!bracedValue) {
			if (annotationName) diagnostics.push(`${name} has an unclosed line selector`);
			break;
		}
		index = bracedValue.next;

		if (annotationName) {
			const directive = name as AnnotationDirective;
			const kind: AnnotationKind = directive === 'focus' ? 'highlight' : directive;
			directives.push({ kind, value: bracedValue.value });
		}
	}

	return { diagnostics, directives };
}

function addSelectedLines(
	target: Set<number>,
	selector: string,
	lineCount: number,
	diagnostics?: string[],
	directive?: AnnotationKind
): void {
	for (const part of selector.split(',')) {
		const match = part.trim().match(/^(\d+)(?:\s*-\s*(\d+))?$/);
		if (!match) {
			diagnostics?.push(
				`${directive ?? 'annotation'} has an invalid selector ${JSON.stringify(part.trim())}`
			);
			continue;
		}

		const start = Number(match[1]);
		const end = Number(match[2] ?? match[1]);
		if (!Number.isSafeInteger(start) || !Number.isSafeInteger(end) || start < 1 || end < start) {
			diagnostics?.push(`${directive ?? 'annotation'} has an invalid range ${part.trim()}`);
			continue;
		}
		if (start > lineCount || end > lineCount) {
			diagnostics?.push(
				`${directive ?? 'annotation'} range ${part.trim()} exceeds the ${lineCount}-line code block`
			);
		}

		const boundedStart = Math.max(1, start);
		const boundedEnd = Math.min(lineCount, end);
		for (let line = boundedStart; line <= boundedEnd; line += 1) target.add(line);
	}
}

function selectedAnnotationLines(meta: string, lineCount: number) {
	const parsed = readAnnotationDirectives(meta);
	const selected: Record<AnnotationKind, Set<number>> = {
		add: new Set<number>(),
		remove: new Set<number>(),
		highlight: new Set<number>()
	};

	for (const directive of parsed.directives) {
		addSelectedLines(
			selected[directive.kind],
			directive.value,
			lineCount,
			parsed.diagnostics,
			directive.kind
		);
	}

	for (const line of selected.add) {
		if (selected.remove.has(line)) {
			parsed.diagnostics.push(`line ${line} cannot be both added and removed`);
		}
	}

	return { diagnostics: [...new Set(parsed.diagnostics)], selected };
}

/** Returns actionable errors for malformed, out-of-range, or contradictory annotations. */
export function validateCodeLineAnnotations(
	meta: string | null | undefined,
	lineCount: number
): string[] {
	const boundedLineCount = Number.isFinite(lineCount) ? Math.max(0, Math.floor(lineCount)) : 0;
	return selectedAnnotationLines(meta ?? '', boundedLineCount).diagnostics;
}

/**
 * Converts fenced-code metadata such as `add={1,8-11} remove={4} focus={3-5}` into
 * presentation data for every source line. Selectors are one-based and ranges
 * are inclusive. `highlight` remains an alias for `focus`. Invalid selectors
 * and lines outside the source are ignored.
 */
export function parseCodeLineAnnotations(
	meta: string | null | undefined,
	lineCount: number
): CodeLineAnnotation[] {
	const boundedLineCount = Number.isFinite(lineCount) ? Math.max(0, Math.floor(lineCount)) : 0;
	if (boundedLineCount === 0) return [];

	const { selected } = selectedAnnotationLines(meta ?? '', boundedLineCount);

	return Array.from({ length: boundedLineCount }, (_, index) => {
		const lineNumber = index + 1;
		const classes = (Object.keys(annotationClasses) as AnnotationKind[])
			.filter((kind) => selected[kind].has(lineNumber))
			.map((kind) => annotationClasses[kind]);

		return {
			lineNumber,
			classes,
			marker: selected.add.has(lineNumber) ? '+' : selected.remove.has(lineNumber) ? '−' : null
		};
	});
}
