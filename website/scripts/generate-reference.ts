import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { generateReferenceCatalog, stableJSON } from '../src/reference/generator/generate';

const repositoryRoot = path.resolve(import.meta.dir, '../..');
const check = process.argv.includes('--check');
const catalog = generateReferenceCatalog({ repositoryRoot, write: !check });

if (check) {
	const catalogPath = path.join(repositoryRoot, 'website/src/reference/generated/catalog.json');
	if (!existsSync(catalogPath) || readFileSync(catalogPath, 'utf8') !== stableJSON(catalog)) {
		throw new Error('generated API reference drifted; run `bun run reference:generate`');
	}
}

console.log(
	`${check ? 'Verified' : 'Generated'} ${catalog.modules.length} reference modules and ${catalog.modules.reduce((total, module) => total + module.symbols.length, 0)} declarations.`
);
