import { readdir } from "node:fs/promises";
import { resolve } from "node:path";

const assetsRoot = resolve(import.meta.dir, "../.ridu/admin-fixture-build/assets");
const files = await readdir(assetsRoot);

async function gzipSize(path: string) {
	return Bun.gzipSync(await Bun.file(resolve(assetsRoot, path)).bytes()).byteLength;
}

const entry = files.find((path) => /^index-.+\.js$/.test(path));
if (entry === undefined) throw new Error("admin fixture build has no index JavaScript entry");
const asyncFiles = files.filter((path) => path.endsWith(".js") && path !== entry);
const cssFiles = files.filter((path) => path.endsWith(".css"));
const measurements = {
	entryJS: await gzipSize(entry),
	largestAsyncJS: Math.max(0, ...(await Promise.all(asyncFiles.map(gzipSize)))),
	totalCSS: (await Promise.all(cssFiles.map(gzipSize))).reduce((total, size) => total + size, 0),
};
const budgets = { entryJS: 215 * 1024, largestAsyncJS: 125 * 1024, totalCSS: 23 * 1024 };

for (const [name, size] of Object.entries(measurements)) {
	console.log(
		`adminFixture.${name}: ${size.toLocaleString()} gzip bytes (budget ${budgets[name as keyof typeof budgets].toLocaleString()})`
	);
	if (size > budgets[name as keyof typeof budgets]) {
		console.error(`Admin fixture ${name} exceeds its bundle budget.`);
		process.exitCode = 1;
	}
}
