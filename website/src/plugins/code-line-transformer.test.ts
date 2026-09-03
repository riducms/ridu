import { describe, expect, it } from 'bun:test';
import { codeToHtml } from 'shiki';
import { riduCodeLineTransformer } from './code-line-transformer';

describe('riduCodeLineTransformer', () => {
	it('adds static line classes and markers while leaving source text alone', async () => {
		const html = await codeToHtml('first\nsecond\nthird\nfourth', {
			lang: 'text',
			theme: 'github-dark',
			meta: { __raw: 'add={2} focus={3} remove={4}' },
			transformers: [riduCodeLineTransformer]
		});

		expect(html).toContain('class="shiki github-dark has-line-annotations"');
		expect(html).toContain(
			'class="line is-added" data-line-number="2" data-line-marker="+"><span>second</span>'
		);
		expect(html).toContain('class="line is-highlighted" data-line-number="3"><span>third</span>');
		expect(html).toContain(
			'class="line is-removed" data-line-number="4" data-line-marker="−"><span>fourth</span>'
		);
		expect(html).not.toContain('add={2}');
		expect(html).not.toContain('focus={3}');
		expect(html).not.toContain('remove={4}');
	});

	it('rejects contradictory annotations before rendering', async () => {
		expect(
			codeToHtml('first\nsecond', {
				lang: 'text',
				theme: 'github-dark',
				meta: { __raw: 'add={2} remove={2}' },
				transformers: [riduCodeLineTransformer]
			})
		).rejects.toThrow('line 2 cannot be both added and removed');
	});
});
