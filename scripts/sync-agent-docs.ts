import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, posix, resolve } from "node:path";
import { fileURLToPath } from "node:url";

type ReferenceDefinition = {
	source: `docs/${string}.md` | `guides/${string}.md`;
	target: `ridu-project/reference/${string}.md` | `payload-to-ridu/references/${string}.md`;
};

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const websiteContentRoot = resolve(repositoryRoot, "website/src/content");
const skillRoot = resolve(repositoryRoot, "internal/agentdocs/skills");
const check = process.argv.includes("--check");

const references: ReferenceDefinition[] = [
	{ source: "docs/quickstart.md", target: "ridu-project/reference/quickstart.md" },
	{ source: "docs/installation.md", target: "ridu-project/reference/installation.md" },
	{ source: "docs/core-concepts.md", target: "ridu-project/reference/core-concepts.md" },
	{ source: "docs/go-for-typescript.md", target: "ridu-project/reference/go-for-typescript.md" },
	{ source: "docs/go-packages.md", target: "ridu-project/reference/go-packages.md" },
	...["operation", "store", "query", "schema"].map((name) => ({
		source: `docs/go-packages/${name}.md` as const,
		target: `ridu-project/reference/go-packages/${name}.md` as const,
	})),
	{ source: "docs/configuration.md", target: "ridu-project/reference/configuration.md" },
	{ source: "docs/collections.md", target: "ridu-project/reference/collections.md" },
	{ source: "docs/fields.md", target: "ridu-project/reference/fields.md" },
	{ source: "docs/fields/lists.md", target: "ridu-project/reference/fields/lists.md" },
	{ source: "docs/fields/defaults.md", target: "ridu-project/reference/fields/defaults.md" },
	{
		source: "docs/fields/live-validation.md",
		target: "ridu-project/reference/fields/live-validation.md",
	},
	{
		source: "docs/fields/validation.md",
		target: "ridu-project/reference/fields/validation.md",
	},
	{
		source: "docs/fields/callback-values.md",
		target: "ridu-project/reference/fields/callback-values.md",
	},
	{ source: "docs/access-control.md", target: "ridu-project/reference/access-control.md" },
	{ source: "docs/hooks.md", target: "ridu-project/reference/hooks.md" },
	...["collections", "globals", "fields", "context", "transactions-and-errors"].map((name) => ({
		source: `docs/hooks/${name}.md` as const,
		target: `ridu-project/reference/hooks/${name}.md` as const,
	})),
	{ source: "docs/querying.md", target: "ridu-project/reference/querying.md" },
	{ source: "docs/local-api.md", target: "ridu-project/reference/local-api.md" },
	{
		source: "docs/generated-contracts.md",
		target: "ridu-project/reference/generated-contracts.md",
	},
	{ source: "docs/migrations.md", target: "ridu-project/reference/migrations.md" },
	{ source: "docs/postgres.md", target: "ridu-project/reference/postgres.md" },
	{ source: "docs/sqlite.md", target: "ridu-project/reference/sqlite.md" },
	{ source: "docs/mongodb.md", target: "ridu-project/reference/mongodb.md" },
	{ source: "docs/data-access.md", target: "ridu-project/reference/data-access.md" },
	{ source: "docs/rest-api.md", target: "ridu-project/reference/rest-api.md" },
	{ source: "docs/typescript-sdk.md", target: "ridu-project/reference/typescript-sdk.md" },
	{ source: "docs/plugins.md", target: "ridu-project/reference/plugins.md" },
	{ source: "docs/rich-text.md", target: "ridu-project/reference/rich-text.md" },
	{ source: "docs/seo.md", target: "ridu-project/reference/seo.md" },
	{ source: "docs/form-builder.md", target: "ridu-project/reference/form-builder.md" },
	{ source: "docs/graphql.md", target: "ridu-project/reference/graphql.md" },
	{ source: "docs/mcp.md", target: "ridu-project/reference/mcp.md" },
	{ source: "docs/admin.md", target: "ridu-project/reference/admin.md" },
	{ source: "docs/custom-components.md", target: "ridu-project/reference/custom-components.md" },
	...[
		"field-components",
		"row-labels",
		"list-cells",
		"dashboard",
		"custom-pages",
		"document-views",
		"document-actions",
		"custom-views",
		"branding-and-navigation",
		"providers",
	].map((name) => ({
		source: `docs/custom-components/${name}.md` as const,
		target: `ridu-project/reference/custom-components/${name}.md` as const,
	})),
	{ source: "guides/custom-fields.md", target: "ridu-project/reference/custom-fields.md" },
	{
		source: "docs/browsing-content.md",
		target: "ridu-project/reference/browsing-content.md",
	},
	{
		source: "docs/saved-views-and-hierarchy.md",
		target: "ridu-project/reference/saved-views-and-hierarchy.md",
	},
	{
		source: "docs/editing-documents.md",
		target: "ridu-project/reference/editing-documents.md",
	},
	{
		source: "docs/bulk-and-trash.md",
		target: "ridu-project/reference/bulk-and-trash.md",
	},
	{
		source: "docs/document-locks.md",
		target: "ridu-project/reference/document-locks.md",
	},
	{
		source: "docs/drafts-and-versions.md",
		target: "ridu-project/reference/drafts-and-versions.md",
	},
	{ source: "docs/localization.md", target: "ridu-project/reference/localization.md" },
	{ source: "docs/uploads.md", target: "ridu-project/reference/uploads.md" },
	{ source: "guides/live-preview.md", target: "ridu-project/reference/live-preview.md" },
	{ source: "docs/testing.md", target: "ridu-project/reference/testing.md" },
	{ source: "docs/production.md", target: "ridu-project/reference/production.md" },
	{
		source: "docs/troubleshooting.md",
		target: "ridu-project/reference/troubleshooting.md",
	},
	{ source: "docs/status.md", target: "ridu-project/reference/capabilities.md" },
	{ source: "guides/from-payload.md", target: "payload-to-ridu/references/migration-guide.md" },
];

const targetsByRoute = new Map(
	references.map((reference) => {
		const route = `/${reference.source.replace(/\.md$/, "/")}`;
		return [route, reference.target] as const;
	})
);

function frontmatterValue(frontmatter: string, key: string): string | undefined {
	const match = frontmatter.match(
		new RegExp(`^${key}:\\s*(?:'([^']*)'|"([^"]*)"|([^\\n]+))$`, "m")
	);
	return match?.[1] ?? match?.[2] ?? match?.[3]?.trim();
}

function parseDocument(source: string): { body: string; title: string } {
	const match = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/);
	if (!match) throw new Error("public documentation is missing YAML frontmatter");
	const frontmatter = match[1] ?? "";
	const body = match[2] ?? "";
	const title = frontmatterValue(frontmatter, "title");
	if (!title) throw new Error("public documentation is missing a title");
	return { title, body: body.trim() };
}

function remoteTarget(target: string): string {
	return `https://riducms.com${target}`;
}

function remoteSourceImage(source: ReferenceDefinition["source"], target: string): string {
	const sourceDirectory = posix.dirname(posix.join("website/src/content", source));
	const repositoryPath = posix.normalize(posix.join(sourceDirectory, target));
	if (repositoryPath === ".." || repositoryPath.startsWith("../")) {
		throw new Error(`image target ${JSON.stringify(target)} escapes the public source tree`);
	}
	return `https://raw.githubusercontent.com/riducms/ridu/main/${repositoryPath}`;
}

function rewriteLink(target: string, currentTarget: string): string {
	if (!target.startsWith("/")) return target;
	const fragmentIndex = target.indexOf("#");
	const withoutFragment = fragmentIndex === -1 ? target : target.slice(0, fragmentIndex);
	const fragment = fragmentIndex === -1 ? undefined : target.slice(fragmentIndex + 1);
	const normalizedRoute = withoutFragment.endsWith("/") ? withoutFragment : `${withoutFragment}/`;
	const localTarget = targetsByRoute.get(normalizedRoute);
	if (!localTarget) return remoteTarget(target);
	let relative = posix.relative(posix.dirname(currentTarget), localTarget);
	if (!relative.startsWith(".")) relative = `./${relative}`;
	return `${relative}${fragment === undefined ? "" : `#${fragment}`}`;
}

function renderReference(definition: ReferenceDefinition, source: string): string {
	const document = parseDocument(source);
	const imagesRewritten = document.body.replace(
		/!\[([^\]]*)\]\(([^)\s]+)\)/g,
		(_match, label: string, target: string) => {
			if (target.startsWith("https://")) return `![${label}](${target})`;
			if (target.startsWith("/")) return `![${label}](${remoteTarget(target)})`;
			return `![${label}](${remoteSourceImage(definition.source, target)})`;
		}
	);
	const body = imagesRewritten.replace(/\]\((\/[^)\s]+)\)/g, (_match, target: string) => {
		return `](${rewriteLink(target, definition.target)})`;
	});
	return `<!-- Generated from website/src/content/${definition.source} by scripts/sync-agent-docs.ts. -->\n\n# ${document.title}\n\n${body}\n`;
}

const drift: string[] = [];
for (const definition of references) {
	const source = await readFile(resolve(websiteContentRoot, definition.source), "utf8");
	const rendered = renderReference(definition, source);
	const target = resolve(skillRoot, definition.target);
	let current: string | undefined;
	try {
		current = await readFile(target, "utf8");
	} catch (error) {
		if (!(error instanceof Error && "code" in error && error.code === "ENOENT")) throw error;
	}
	if (current === rendered) continue;
	if (check) {
		drift.push(definition.target);
		continue;
	}
	await mkdir(dirname(target), { recursive: true });
	await writeFile(target, rendered);
}

if (drift.length > 0) {
	throw new Error(`agent documentation is out of date: ${drift.join(", ")}`);
}

if (!check) console.log(`synchronized ${references.length} agent reference documents`);
