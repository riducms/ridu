import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { generateReferenceCatalog, stableJSON } from '../src/reference/generator/generate';

const repositoryRoot = path.resolve(import.meta.dir, '../..');
const check = process.argv.includes('--check');
const seedRoutes = process.argv.includes('--seed-routes');
const generated = generateReferenceCatalog({
	repositoryRoot,
	write: !check,
	seedRoutes
});

if (check) {
	const catalogPath = path.join(repositoryRoot, 'website/src/reference/generated/catalog.json');
	const routeLockPath = path.join(
		repositoryRoot,
		'website/src/reference/authoring/route-lock.json'
	);
	if (!existsSync(catalogPath) || readFileSync(catalogPath, 'utf8') !== stableJSON(generated)) {
		throw new Error('generated API reference drifted; run `bun run reference:generate`');
	}
	if (!existsSync(routeLockPath)) throw new Error('generated API reference route lock is missing');
}

console.log(
	`${check ? 'Verified' : 'Generated'} ${generated.modules.length} reference modules and ${generated.modules.reduce((total, module) => total + module.symbols.length, 0)} declarations.`
);
