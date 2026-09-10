import { describe, expect, it } from 'bun:test';
import { Glob } from 'bun';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { parseCodeLineAnnotations, validateCodeLineAnnotations } from './code-line-annotations';

describe('parseCodeLineAnnotations', () => {
	it('maps added and highlighted ranges to one-based source lines', () => {
		const annotations = parseCodeLineAnnotations(
			'title="content/richtext.go" add={1,8-11} highlight={3-5}',
			12
		);

		expect(annotations[0]).toEqual({
			lineNumber: 1,
			classes: ['is-added'],
			marker: '+'
		});
		expect(annotations.slice(2, 5)).toEqual([
			{ lineNumber: 3, classes: ['is-highlighted'], marker: null },
			{ lineNumber: 4, classes: ['is-highlighted'], marker: null },
			{ lineNumber: 5, classes: ['is-highlighted'], marker: null }
		]);
		expect(annotations.slice(7, 11).every((line) => line.marker === '+')).toBe(true);
		expect(annotations[11]).toEqual({ lineNumber: 12, classes: [], marker: null });
	});

	it('combines overlapping and repeated annotations deterministically', () => {
		const annotations = parseCodeLineAnnotations('focus={2-4} add={3,4} add={3}, highlight={4}', 5);

		expect(annotations).toEqual([
			{ lineNumber: 1, classes: [], marker: null },
			{ lineNumber: 2, classes: ['is-highlighted'], marker: null },
			{ lineNumber: 3, classes: ['is-added', 'is-highlighted'], marker: '+' },
			{ lineNumber: 4, classes: ['is-added', 'is-highlighted'], marker: '+' },
			{ lineNumber: 5, classes: [], marker: null }
		]);
	});

	it('marks removed lines without changing their source text', () => {
		const annotations = parseCodeLineAnnotations('remove={2-3} add={5}', 5);

		expect(annotations).toEqual([
			{ lineNumber: 1, classes: [], marker: null },
			{ lineNumber: 2, classes: ['is-removed'], marker: '−' },
			{ lineNumber: 3, classes: ['is-removed'], marker: '−' },
			{ lineNumber: 4, classes: [], marker: null },
			{ lineNumber: 5, classes: ['is-added'], marker: '+' }
		]);
	});

	it('validates malformed, out-of-range, and contradictory selectors', () => {
		expect(validateCodeLineAnnotations('add={2-4} remove={3} focus=nope', 3)).toEqual([
			'focus must use a braced line selector',
			'add range 2-4 exceeds the 3-line code block',
			'line 3 cannot be both added and removed'
		]);
		expect(validateCodeLineAnnotations('title="add={99}" add={2}', 2)).toEqual([]);
	});

	it('ignores quoted lookalikes, malformed selectors, and out-of-bounds lines', () => {
		const annotations = parseCodeLineAnnotations(
			`title="add={1}" label='highlight={2}' add={0,2-3,4-2,nope,8-10} highlight={3-99}`,
			5
		);

		expect(annotations).toEqual([
			{ lineNumber: 1, classes: [], marker: null },
			{ lineNumber: 2, classes: ['is-added'], marker: '+' },
			{ lineNumber: 3, classes: ['is-added', 'is-highlighted'], marker: '+' },
			{ lineNumber: 4, classes: ['is-highlighted'], marker: null },
			{ lineNumber: 5, classes: ['is-highlighted'], marker: null }
		]);
	});

	it('returns a stable unannotated record for every source line', () => {
		expect(parseCodeLineAnnotations(undefined, 2)).toEqual([
			{ lineNumber: 1, classes: [], marker: null },
			{ lineNumber: 2, classes: [], marker: null }
		]);
		expect(parseCodeLineAnnotations('add={1}', 0)).toEqual([]);
		expect(parseCodeLineAnnotations('add={1}', Number.NaN)).toEqual([]);
	});

	it('keeps every authored annotation inside useful unchanged context', async () => {
		const contentRoot = path.join(import.meta.dir, '../content');
		const glob = new Glob('**/*.{md,mdx}');
		const failures: string[] = [];

		for await (const relativePath of glob.scan({ cwd: contentRoot })) {
			const source = readFileSync(path.join(contentRoot, relativePath), 'utf8');
			const fences = source.matchAll(
				/^[ \t]*```[^\n]*\b(?:add|remove|highlight|focus)=\{[^\n]+\}[^\n]*\n([\s\S]*?)^[ \t]*```/gm
			);
			for (const fence of fences) {
				const code = fence[1]?.replace(/\n$/, '') ?? '';
				const lines = code === '' ? [] : code.split('\n');
				const meta = fence[0]?.slice(3, fence[0].indexOf('\n')) ?? '';
				const annotated = parseCodeLineAnnotations(meta, lines.length).filter(
					(line) => line.classes.length > 0
				).length;
				const context = lines.length - annotated;
				if (context < 2) {
					const line = source.slice(0, fence.index).split('\n').length;
					failures.push(
						`${relativePath}:${line} has ${annotated} annotated lines and ${context} context lines`
					);
				}
			}
		}

		expect(failures).toEqual([]);
	});

	it('keeps the Quickstart model change on the new field', () => {
		const quickstart = readFileSync(
			path.join(import.meta.dir, '../content/docs/quickstart.md'),
			'utf8'
		);
		const fence = quickstart.match(/```go([^\n]*add=\{[^}]+\}[^\n]*)\n([\s\S]*?)```/);
		expect(fence, 'quickstart.md is missing its annotated model change').not.toBeNull();

		const lines = (fence?.[2] ?? '').trimEnd().split('\n');
		const addedLines = parseCodeLineAnnotations(fence?.[1], lines.length)
			.filter((annotation) => annotation.classes.includes('is-added'))
			.map((annotation) => lines[annotation.lineNumber - 1]?.trim());

		expect(addedLines).toEqual([
			'field.Textarea("summary").',
			'MaxLength(240).',
			'Admin(field.Admin{',
			'Description: "A short introduction used by post cards.",',
			'}),'
		]);
	});

	it('keeps the CORS annotations attached to the relevant server configuration', () => {
		const cors = readFileSync(path.join(import.meta.dir, '../content/docs/cors.md'), 'utf8');
		const fences = [...cors.matchAll(/```go([^\n]*add=\{[^}]+\}[^\n]*)\n([\s\S]*?)```/g)];
		expect(fences).toHaveLength(2);

		const addedLines = (fence: RegExpMatchArray) => {
			const lines = fence[2].trimEnd().split('\n');
			return parseCodeLineAnnotations(fence[1], lines.length)
				.filter((annotation) => annotation.classes.includes('is-added'))
				.map((annotation) => lines[annotation.lineNumber - 1]?.trim());
		};

		expect(addedLines(fences[0])).toEqual([
			'ridu.WithHandlerOptions(ridu.HandlerOptions{',
			'AllowedOrigins: []string{',
			'"https://app.example.com",',
			'},',
			'}),'
		]);
		expect(addedLines(fences[1])).toEqual([
			'AllowedRequestHeaders: []string{',
			'"X-Workspace-ID",',
			'},'
		]);
	});
});
