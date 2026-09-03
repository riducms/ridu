import { describe, expect, test } from 'bun:test';
import { buildLLMSFull, buildLLMSIndex, frameworkVersion } from './llms';

describe('LLM documentation feeds', () => {
	test('publishes a concise canonical documentation index', async () => {
		const index = await buildLLMSIndex();

		expect(index).toContain('# Ridu');
		expect(index).toContain(`Release documentation version: ${frameworkVersion}`);
		expect(index).toContain('https://riducms.com/docs/fields/');
		expect(index).toContain('https://riducms.com/reference/field/');
		expect(index).toContain('https://riducms.com/search/');
		expect(index).toContain(`https://riducms.com/v/${frameworkVersion}/llms-full.txt`);
	});

	test('publishes complete public guides with absolute links', async () => {
		const full = await buildLLMSFull();

		expect(full).toContain('# Fields');
		expect(full).toContain('# Move from Payload');
		expect(full).toContain('# API Reference: field');
		expect(full).toContain('https://riducms.com/reference/field/select/');
		expect(full).toContain('https://riducms.com/docs/access-control/');
		expect(full).toContain(
			'https://raw.githubusercontent.com/riducms/ridu/main/docs/assets/ridu-admin-dashboard.png'
		);
		expect(full).toContain(
			'https://raw.githubusercontent.com/riducms/ridu/main/docs/assets/fields/select.png'
		);
		expect(full).not.toContain('../../../../docs/assets/');
		expect(full).not.toContain('docs/roadmap/payload-parity.md');
		expect(full).not.toContain('\nproduct: ');
		expect(full).not.toContain('\nnavigation:');
	});
});
