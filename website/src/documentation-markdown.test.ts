import { describe, expect, test } from 'bun:test';
import { documentationPageMarkdown } from './documentation-markdown';

describe('documentationPageMarkdown', () => {
	test('produces standalone Markdown with public links and source-backed images', () => {
		const markdown = documentationPageMarkdown({
			title: 'Select field',
			description: 'Configure a choice field.',
			href: '/docs/fields/select/',
			body: '[Fields](/docs/fields/)\n\n![Select](../../../../../docs/assets/fields/select.png)',
			sourcePath: 'website/src/content/docs/fields/select.md'
		});

		expect(markdown).toContain('# Select field');
		expect(markdown).toContain('Canonical URL: https://riducms.com/docs/fields/select/');
		expect(markdown).toContain('[Fields](https://riducms.com/docs/fields/)');
		expect(markdown).toContain(
			'![Select](https://raw.githubusercontent.com/riducms/ridu/main/docs/assets/fields/select.png)'
		);
	});
});
