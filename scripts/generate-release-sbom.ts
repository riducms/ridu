const [outputPath, binaryPath, dependencyInventoryPath] = process.argv.slice(2);
if (outputPath === undefined || binaryPath === undefined || dependencyInventoryPath === undefined) {
	throw new Error(
		"usage: bun generate-release-sbom.ts <output.spdx.json> <ridu-binary> <dependency-inventory.json>"
	);
}

const repositoryRoot = new URL("../", import.meta.url).pathname;
const versionCheck = Bun.spawnSync(
	["bun", `${repositoryRoot}scripts/check-release-version.ts`, "--print"],
	{ cwd: repositoryRoot, stdout: "pipe", stderr: "pipe" }
);
if (versionCheck.exitCode !== 0) throw new Error(versionCheck.stderr.toString());
const version = versionCheck.stdout.toString().trim();

function gitOutput(...args: string[]) {
	const result = Bun.spawnSync(["git", "-C", repositoryRoot, ...args], {
		stdout: "pipe",
		stderr: "pipe",
	});
	return result.exitCode === 0 ? result.stdout.toString().trim() : undefined;
}

const revision = process.env.GITHUB_SHA ?? gitOutput("rev-parse", "HEAD") ?? "local";
const sourceDateEpoch = Number(
	process.env.SOURCE_DATE_EPOCH ?? gitOutput("show", "-s", "--format=%ct", "HEAD")
);
if (!Number.isSafeInteger(sourceDateEpoch) || sourceDateEpoch <= 0) {
	throw new Error("SOURCE_DATE_EPOCH or the current Git commit timestamp must be available");
}
const created = new Date(sourceDateEpoch * 1_000).toISOString().replace(/\.\d{3}Z$/, "Z");

function purlPath(name: string) {
	return name
		.split("/")
		.map((segment) => encodeURIComponent(segment))
		.join("/");
}

const goVersion = Bun.spawnSync(["go", "version", "-m", binaryPath], {
	cwd: repositoryRoot,
	stdout: "pipe",
	stderr: "pipe",
});
if (goVersion.exitCode !== 0) throw new Error(goVersion.stderr.toString());

const goModules = goVersion.stdout
	.toString()
	.split("\n")
	.map((line) => line.trim().split("\t"))
	.filter((fields) => fields[0] === "dep" && fields.length >= 3)
	.map((fields) => ({ Path: fields[1]!, Version: fields[2]! }));
if (goModules.length === 0) throw new Error(`${binaryPath} exposes no linked Go module inventory`);

interface BundledDependency {
	kind: "package" | "generated-icon-collection";
	name: string;
	version: string;
	license?: string;
	sourceURL?: string;
}
const javascriptDependencies = (await Bun.file(
	dependencyInventoryPath
).json()) as BundledDependency[];
if (javascriptDependencies.length === 0) {
	throw new Error(`${dependencyInventoryPath} exposes no bundled admin dependencies`);
}

const packages = [
	{
		SPDXID: "SPDXRef-Package-Ridu",
		name: "github.com/riducms/ridu",
		versionInfo: version,
		downloadLocation: `https://github.com/riducms/ridu/releases/tag/v${version}`,
		filesAnalyzed: false,
		licenseConcluded: "MIT",
		licenseDeclared: "MIT",
		copyrightText: "Copyright (c) 2026 Haniel Ubogu",
	},
	...goModules.map((module, index) => ({
		SPDXID: `SPDXRef-Package-Go-${index + 1}`,
		name: module.Path,
		versionInfo: module.Version,
		downloadLocation: "NOASSERTION",
		filesAnalyzed: false,
		licenseConcluded: "NOASSERTION",
		licenseDeclared: "NOASSERTION",
		copyrightText: "NOASSERTION",
		externalRefs: [
			{
				referenceCategory: "PACKAGE-MANAGER",
				referenceType: "purl",
				referenceLocator: `pkg:golang/${purlPath(module.Path)}@${encodeURIComponent(module.Version ?? "")}`,
			},
		],
	})),
	...javascriptDependencies.map((dependency, index) => ({
		SPDXID: `SPDXRef-Package-JS-${index + 1}`,
		name: dependency.name,
		versionInfo: dependency.version,
		downloadLocation: dependency.sourceURL ?? "NOASSERTION",
		filesAnalyzed: false,
		licenseConcluded: "NOASSERTION",
		licenseDeclared: dependency.license ?? "NOASSERTION",
		copyrightText: "NOASSERTION",
		...(dependency.kind === "package"
			? {
					externalRefs: [
						{
							referenceCategory: "PACKAGE-MANAGER",
							referenceType: "purl",
							referenceLocator: `pkg:npm/${purlPath(dependency.name)}@${encodeURIComponent(dependency.version)}`,
						},
					],
				}
			: {}),
	})),
];

const sbom = {
	spdxVersion: "SPDX-2.3",
	dataLicense: "CC0-1.0",
	SPDXID: "SPDXRef-DOCUMENT",
	name: `ridu-${version}`,
	documentNamespace: `https://riducms.com/sbom/${version}/${revision}`,
	creationInfo: {
		created,
		creators: ["Organization: Ridu", "Tool: scripts/generate-release-sbom.ts"],
	},
	packages,
	relationships: [
		{
			spdxElementId: "SPDXRef-DOCUMENT",
			relationshipType: "DESCRIBES",
			relatedSpdxElement: "SPDXRef-Package-Ridu",
		},
		...packages.slice(1).map((dependency) => ({
			spdxElementId: "SPDXRef-Package-Ridu",
			relationshipType: "DEPENDS_ON",
			relatedSpdxElement: dependency.SPDXID,
		})),
	],
};

const identifiers = new Set<string>();
for (const packageRecord of packages) {
	if (identifiers.has(packageRecord.SPDXID))
		throw new Error(`duplicate SPDX identifier ${packageRecord.SPDXID}`);
	identifiers.add(packageRecord.SPDXID);
	for (const reference of "externalRefs" in packageRecord ? packageRecord.externalRefs : []) {
		if (!reference.referenceLocator.startsWith("pkg:") || /%2f/i.test(reference.referenceLocator)) {
			throw new Error(`non-canonical package URL ${reference.referenceLocator}`);
		}
	}
}

await Bun.write(outputPath, `${JSON.stringify(sbom, null, 2)}\n`);
console.log(`wrote SPDX 2.3 SBOM with ${packages.length} packages to ${outputPath}`);
