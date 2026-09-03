const repositoryRoot = new URL("../", import.meta.url).pathname;

const packageDefinitions = [
	{ path: "admin/package.json", name: "@riducms/admin" },
	{ path: "packages/cli/package.json", name: "@riducms/cli" },
	{ path: "packages/create-ridu/package.json", name: "create-ridu" },
	{ path: "packages/build/package.json", name: "@riducms/build" },
	{ path: "packages/plugin/package.json", name: "@riducms/plugin" },
	{ path: "packages/ui/package.json", name: "@riducms/ui" },
	{ path: "packages/protocol/package.json", name: "@riducms/protocol" },
	{ path: "packages/sdk/package.json", name: "@riducms/sdk" },
	{ path: "packages/translations/package.json", name: "@riducms/translations" },
	{ path: "packages/plugin-richtext/package.json", name: "@riducms/plugin-richtext" },
	{ path: "packages/plugin-seo/package.json", name: "@riducms/plugin-seo" },
	{ path: "packages/plugin-form-builder/package.json", name: "@riducms/plugin-form-builder" },
] as const;

const semanticVersionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
const coreSource = await Bun.file(`${repositoryRoot}core/version.go`).text();
const coreVersion = coreSource.match(/const FrameworkVersion = "([^"]+)"/)?.[1];
if (coreVersion === undefined || !semanticVersionPattern.test(coreVersion)) {
	throw new Error("core/version.go must expose one stable semantic FrameworkVersion");
}

const printVersion = process.argv.includes("--print");
const tagArgument = process.argv.slice(2).find((argument) => argument !== "--print");
const environmentTag =
	process.env.GITHUB_REF_TYPE === "tag" ? process.env.GITHUB_REF_NAME : undefined;
if (environmentTag !== undefined && tagArgument !== undefined && tagArgument !== environmentTag) {
	throw new Error(
		`explicit release tag ${tagArgument} does not match GitHub tag ${environmentTag}`
	);
}
const requestedTag = environmentTag ?? tagArgument;
let version = coreVersion;
if (requestedTag !== undefined) {
	if (!requestedTag.startsWith("v") || !semanticVersionPattern.test(requestedTag.slice(1))) {
		throw new Error(`release tag ${requestedTag} must have the form vMAJOR.MINOR.PATCH`);
	}
	version = requestedTag.slice(1);
}

if (coreVersion !== version) {
	throw new Error(`core/version.go has ${coreVersion}; release requires ${version}`);
}

for (const definition of packageDefinitions) {
	const manifest = await Bun.file(`${repositoryRoot}${definition.path}`).json();
	if (manifest.name !== definition.name) {
		throw new Error(`${definition.path} is named ${manifest.name}; expected ${definition.name}`);
	}
	if (manifest.version !== version) {
		throw new Error(`${definition.path} has version ${manifest.version}; expected ${version}`);
	}
	if (manifest.private !== undefined) {
		throw new Error(`${definition.path} must not declare private`);
	}
	if (manifest.license !== "MIT") {
		throw new Error(`${definition.path} must declare the MIT license`);
	}
	if (manifest.publishConfig?.access !== "public" || manifest.publishConfig?.provenance !== true) {
		throw new Error(`${definition.path} must publish publicly with provenance`);
	}
}

for (const pluginPath of [
	"plugins/graphql/graphql.go",
	"plugins/richtext/richtext.go",
	"plugins/seo/seo.go",
	"plugins/formbuilder/formbuilder.go",
]) {
	const source = await Bun.file(`${repositoryRoot}${pluginPath}`).text();
	if (!/Version:\s+ridu\.FrameworkVersion/.test(source)) {
		throw new Error(`${pluginPath} does not inherit the framework release version`);
	}
	if (!/Minimum:\s+ridu\.FrameworkVersion/.test(source)) {
		throw new Error(`${pluginPath} does not begin compatibility at the framework release version`);
	}
	if (/MaximumExclusive:/.test(source)) {
		throw new Error(`${pluginPath} must leave compatibility open for coordinated future releases`);
	}
}

if (printVersion) {
	console.log(version);
} else {
	console.log(
		`coordinated Ridu release version verified (${version}, ${packageDefinitions.length} npm packages)`
	);
}
