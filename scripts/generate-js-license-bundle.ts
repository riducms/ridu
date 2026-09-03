export {};

const [inventoryPath, noticesPath, outputPath] = process.argv.slice(2);
if (inventoryPath === undefined || noticesPath === undefined || outputPath === undefined) {
	throw new Error(
		"usage: bun generate-js-license-bundle.ts <dependency-inventory.json> <notices.md> <output.txt>"
	);
}

interface BundledDependency {
	kind: "package" | "generated-icon-collection";
	name: string;
	version: string;
	license?: string;
	noticeSection?: string;
	root: string;
	sourcePackage?: string;
}

interface ReviewedNotice {
	license: string;
	section: string;
}

const reviewedNotices = new Map<string, ReviewedNotice>([
	["@fontsource-variable/geist@5.3.0", { license: "OFL-1.1", section: "Geist" }],
	[
		"@fontsource-variable/spline-sans-mono@5.3.0",
		{ license: "OFL-1.1", section: "Spline Sans Mono" },
	],
	["@dnd-kit/abstract@0.2.4", { license: "MIT", section: "dnd-kit" }],
	["@dnd-kit/collision@0.2.4", { license: "MIT", section: "dnd-kit" }],
	["@dnd-kit/dom@0.2.4", { license: "MIT", section: "dnd-kit" }],
	["@dnd-kit/geometry@0.2.4", { license: "MIT", section: "dnd-kit" }],
	["@dnd-kit/state@0.2.4", { license: "MIT", section: "dnd-kit" }],
	["@dnd-kit-svelte/svelte@0.1.6", { license: "MIT", section: "dnd-kit-svelte" }],
	["Lucide icons@2.2.509", { license: "ISC AND MIT", section: "Lucide icons" }],
]);
const reviewedIdentityByName = new Map(
	[...reviewedNotices.keys()].map((identity) => [
		identity.slice(0, identity.lastIndexOf("@")),
		identity,
	])
);

const dependencies = (await Bun.file(inventoryPath).json()) as BundledDependency[];
const notices = await Bun.file(noticesPath).text();
const renderedIdentities = new Set(
	dependencies.map((dependency) => `${dependency.name}@${dependency.version}`)
);
let bundle =
	"Third-party JavaScript package licences\n\n" +
	"Generated from package modules included in emitted production Ridu admin chunks. " +
	"Dependencies named in THIRD_PARTY_NOTICES.md are validated against that reviewed file " +
	"and are not duplicated here.\n";

for (const dependency of dependencies) {
	const identity = `${dependency.name}@${dependency.version}`;
	const reviewedIdentity = reviewedIdentityByName.get(dependency.name);
	if (reviewedIdentity !== undefined && reviewedIdentity !== identity) {
		throw new Error(`${identity} requires a reviewed notice; expected ${reviewedIdentity}`);
	}
	await validateSourcePackage(dependency);
	const reviewedNotice = reviewedNotices.get(identity);
	if (reviewedNotice !== undefined) {
		if (dependency.license !== reviewedNotice.license) {
			throw new Error(
				`${identity} declares ${dependency.license ?? "no licence"}; expected ${reviewedNotice.license}`
			);
		}
		if (
			dependency.noticeSection !== undefined &&
			dependency.noticeSection !== reviewedNotice.section
		) {
			throw new Error(
				`${identity} expects notice section ${dependency.noticeSection}, not ${reviewedNotice.section}`
			);
		}
		if (!notices.includes(`## ${reviewedNotice.section}`)) {
			throw new Error(
				`${identity} requires a reviewed ## ${reviewedNotice.section} section in ${noticesPath}`
			);
		}
		if (!notices.includes(`\`${dependency.name} ${dependency.version}\``)) {
			throw new Error(`${identity} is not identified by exact name and version in ${noticesPath}`);
		}
		continue;
	}

	if (dependency.kind !== "package") {
		throw new Error(`${identity} has no reviewed generated-asset notice`);
	}
	const entries = [
		...new Bun.Glob("**/{LICENSE,LICENCE,COPYING,NOTICE,license,licence,copying,notice}*").scanSync(
			dependency.root
		),
	].sort();
	if (entries.length === 0) {
		throw new Error(`${identity} has no distributable licence file or reviewed notice`);
	}
	bundle += `\n\n================================================================================\n${dependency.name} ${dependency.version}\n`;
	for (const entry of entries) {
		const contents = await Bun.file(`${dependency.root}/${entry}`).text();
		bundle += `\n--- ${entry} ---\n${contents}`;
		if (!contents.endsWith("\n")) bundle += "\n";
	}
}

for (const identity of reviewedNotices.keys()) {
	if (!renderedIdentities.has(identity)) {
		throw new Error(`${identity} has a reviewed dependency notice but is absent from the bundle`);
	}
}

await Bun.write(outputPath, bundle);
console.log(
	`validated notices and wrote licences for ${dependencies.length} bundled admin dependencies to ${outputPath}`
);

async function validateSourcePackage(dependency: BundledDependency) {
	const manifestPath = `${dependency.root}/package.json`;
	if (!(await Bun.file(manifestPath).exists())) {
		throw new Error(
			`${dependency.name}@${dependency.version} has no source package at ${dependency.root}`
		);
	}
	const manifest = (await Bun.file(manifestPath).json()) as { name?: string; version?: string };
	const expectedName = dependency.sourcePackage ?? dependency.name;
	if (manifest.name !== expectedName || manifest.version !== dependency.version) {
		throw new Error(
			`${dependency.name}@${dependency.version} source identity is ${manifest.name ?? "missing"}@${manifest.version ?? "missing"}`
		);
	}
}
