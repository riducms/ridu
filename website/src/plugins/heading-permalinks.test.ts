import { describe, expect, it } from 'bun:test';
import { markdownToHtml } from 'satteri';
import { riduHeadingPermalinks } from './heading-permalinks';

describe('riduHeadingPermalinks', () => {
	it('links h2 through h4 while leaving the page title and deeper headings alone', () => {
		const { html } = markdownToHtml(
			'# Page title\n\n## Section\n\n### Detail\n\n#### Example\n\n##### Aside',
			{ hastPlugins: [riduHeadingPermalinks] }
		);

		expect(html).toContain('<h1>Page title</h1>');
		expect(html).toContain('<h2 id="section">Section<a href="#section"');
		expect(html).toContain('<h3 id="detail">Detail<a href="#detail"');
		expect(html).toContain('<h4 id="example">Example<a href="#example"');
		expect(html).not.toContain('section">#</a>');
		expect(html).toContain('<h5>Aside</h5>');
		expect(html.match(/class="heading-permalink"/g)).toHaveLength(3);
	});

	it('preserves an explicit ID and derives the accessible label from rendered text', () => {
		const { html } = markdownToHtml('## Configure **features** {#rich-text-features}', {
			features: { headingAttributes: true },
			hastPlugins: [riduHeadingPermalinks]
		});

		expect(html).toContain('<h2 id="rich-text-features">');
		expect(html).toContain('href="#rich-text-features"');
		expect(html).toContain('aria-label="Link to “Configure features” section"');
		expect(html).toContain('title="Link to this section"');
	});

	it('uses the same stable duplicate suffixes as Astro heading IDs', () => {
		const { html } = markdownToHtml('## Setup\n\n## Setup\n\n## Setup-1', {
			hastPlugins: [riduHeadingPermalinks]
		});

		expect(html).toContain('<h2 id="setup">');
		expect(html).toContain('<h2 id="setup-1">');
		expect(html).toContain('<h2 id="setup-1-1">');
	});
});
