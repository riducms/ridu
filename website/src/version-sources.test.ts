/// <reference types="bun-types" />

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const repositoryRoot = resolve(import.meta.dir, '../..');

test('keeps published documentation toolchain versions derived from workspace sources', () => {
	const goModule = readFileSync(resolve(repositoryRoot, 'go.mod'), 'utf8');
	const goVersion = goModule.match(/^go\s+(\S+)$/m)?.[1];
	const workspace = JSON.parse(readFileSync(resolve(repositoryRoot, 'package.json'), 'utf8')) as {
		packageManager: string;
	};
	const bunVersion = workspace.packageManager.match(/^bun@(\S+)$/)?.[1];
	if (!goVersion || !bunVersion) throw new Error('Workspace toolchain versions are missing');
	const goRelease = goVersion.split('.').slice(0, 2).join('.');

	const publishedDocs = [
		'README.md',
		'website/src/content/docs/installation.md',
		'website/src/content/docs/quickstart.md',
		'website/src/content/docs/releases.md'
	];
	for (const relativePath of publishedDocs) {
		const source = readFileSync(resolve(repositoryRoot, relativePath), 'utf8');
		expect(source, `${relativePath} Go version`).toContain(`Go ${goRelease} or newer`);
	}

	// Bun's pinned version applies to contributors building the source checkout.
	// Generated applications may instead select npm, pnpm, or Yarn.
	for (const relativePath of ['README.md', 'website/src/content/docs/releases.md']) {
		const source = readFileSync(resolve(repositoryRoot, relativePath), 'utf8');
		expect(source, `${relativePath} Bun version`).toMatch(
			new RegExp(`Bun[^\\n]{0,80}${bunVersion.replaceAll('.', '\\.')}`)
		);
	}
});
