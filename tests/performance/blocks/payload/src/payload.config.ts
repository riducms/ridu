// Payload side of the block-heavy Ridu/Payload benchmark. The schema comes from the
// framework-neutral JSON spec written by ../../spec.ts (BLOCKS_SPEC), so one production build
// serves every scenario. The database adapter is imported dynamically, so a process loads only
// the selected adapter, like an application that installs one.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import v8 from "node:v8";
import vm from "node:vm";
import {
	buildConfig,
	type Block,
	type CollectionConfig,
	type Field,
	type GlobalConfig,
	type PayloadRequest,
} from "payload";

type FieldSpec =
	| { type: "text" | "textarea" | "number" | "checkbox" | "date"; name: string; required?: boolean }
	| { type: "select"; name: string; options: string[] }
	| { type: "relationship"; name: string; relationTo: string }
	| { type: "group" | "array"; name: string; fields: FieldSpec[] }
	| { type: "blocks"; name: string; blocks: string[] };
type ResourceSpec = { slug: string; drafts: boolean; fields: FieldSpec[] };
type Spec = {
	scenario: string;
	references: boolean;
	blocks: Array<{ slug: string; fields: FieldSpec[] }>;
	collections: ResourceSpec[];
	globals: ResourceSpec[];
};

const dirname = path.dirname(fileURLToPath(import.meta.url));
// `next build` evaluates the config without a benchmark spec; it needs only a valid shape.
const buildSpec: Spec = {
	scenario: "build",
	references: true,
	blocks: [],
	collections: [{ slug: "media", drafts: false, fields: [{ type: "text", name: "title" }] }],
	globals: [],
};
const spec: Spec = process.env.BLOCKS_SPEC
	? JSON.parse(fs.readFileSync(process.env.BLOCKS_SPEC, "utf8"))
	: buildSpec;

const databaseURL =
	process.env.PAYLOAD_DATABASE_URL ?? "postgres://127.0.0.1:5432/payload_blocks_build";
const db = databaseURL.startsWith("mongodb")
	? (await import("@payloadcms/db-mongodb")).mongooseAdapter({ url: databaseURL })
	: (await import("@payloadcms/db-postgres")).postgresAdapter({
			pool: { connectionString: databaseURL },
			push: false,
			...(process.env.BLOCKS_MIGRATION_DIR
				? { migrationDir: process.env.BLOCKS_MIGRATION_DIR }
				: {}),
		});

const authenticated = ({ req }: { req: PayloadRequest }) => Boolean(req.user);
const everyone = () => true;

const definitions = new Map(spec.blocks.map((block) => [block.slug, block]));
const inlineBlocks = new Map<string, Block>();
// PostgreSQL limits identifiers to 63 characters and Payload rejects longer block table and enum
// names (for example enum__sections_v_blocks_spacing_container_spacing_padding_bottom), so each
// block gets a short dbName, as a Payload application with this layout builder must declare.
const blockDBNames = new Map(
	spec.blocks.map((block, index) => [
		block.slug,
		`${block.slug
			.split("-")
			.map((part) => part[0])
			.join("")}${index}`,
	])
);
const interfaceName = (slug: string) =>
	slug
		.split("-")
		.map((part) => part[0].toUpperCase() + part.slice(1))
		.join("");

// Inline specs reuse one Block object per slug with an explicit interfaceName, the usual way to
// share an inline definition in Payload; reference specs select the registry by slug.
function inlineBlock(slug: string): Block {
	const cached = inlineBlocks.get(slug);
	if (cached) return cached;
	const definition = definitions.get(slug);
	if (!definition) throw new Error(`unknown block ${slug}`);
	const block: Block = {
		slug,
		dbName: blockDBNames.get(slug),
		interfaceName: interfaceName(slug),
		fields: definition.fields.map(toField),
	};
	inlineBlocks.set(slug, block);
	return block;
}

function toField(field: FieldSpec): Field {
	switch (field.type) {
		case "text":
			return { name: field.name, type: "text", required: field.required };
		case "textarea":
			return { name: field.name, type: "textarea", required: field.required };
		case "number":
			return { name: field.name, type: "number" };
		case "checkbox":
			return { name: field.name, type: "checkbox" };
		case "date":
			return { name: field.name, type: "date" };
		case "select":
			return { name: field.name, type: "select", options: field.options };
		case "relationship":
			return { name: field.name, type: "relationship", relationTo: field.relationTo };
		case "group":
			return { name: field.name, type: "group", fields: field.fields.map(toField) };
		case "array":
			return { name: field.name, type: "array", fields: field.fields.map(toField) };
		case "blocks":
			return spec.references
				? { name: field.name, type: "blocks", blocks: [], blockReferences: field.blocks }
				: { name: field.name, type: "blocks", blocks: field.blocks.map(inlineBlock) };
	}
}

const collections: CollectionConfig[] = [
	{
		slug: "users",
		auth: true,
		admin: { useAsTitle: "email" },
		access: { read: authenticated, update: authenticated, delete: authenticated },
		fields: [],
	},
	...spec.collections.map((resource): CollectionConfig => ({
		slug: resource.slug,
		admin: { useAsTitle: "title" },
		access: { create: authenticated, read: everyone, update: authenticated, delete: authenticated },
		fields: resource.fields.map(toField),
		...(resource.drafts ? { versions: { drafts: true, maxPerDoc: 50 } } : {}),
	})),
];

const globals: GlobalConfig[] = spec.globals.map((resource) => ({
	slug: resource.slug,
	access: { read: everyone, update: authenticated },
	fields: resource.fields.map(toField),
	...(resource.drafts ? { versions: { drafts: true, max: 50 } } : {}),
}));

let collectGarbage: (() => void) | undefined;

export default buildConfig({
	admin: {
		user: "users",
		importMap: { baseDir: path.resolve(dirname), autoGenerate: false },
	},
	blocks: spec.references
		? spec.blocks.map((block) => ({
				slug: block.slug,
				dbName: blockDBNames.get(block.slug),
				fields: block.fields.map(toField),
			}))
		: undefined,
	collections,
	db,
	endpoints: [
		{
			// Benchmark-only diagnostics, the counterpart of the Ridu fixture's /__bench/memory.
			path: "/__bench/memory",
			method: "get",
			handler: async () => {
				if (!collectGarbage) {
					v8.setFlagsFromString("--expose-gc");
					collectGarbage = vm.runInNewContext("gc") as () => void;
				}
				collectGarbage();
				const memory = process.memoryUsage();
				return Response.json({
					heapUsed: memory.heapUsed,
					heapTotal: memory.heapTotal,
					external: memory.external,
					arrayBuffers: memory.arrayBuffers,
					rss: memory.rss,
				});
			},
		},
	],
	globals,
	graphQL: { disable: process.env.BLOCKS_GRAPHQL === "false" },
	secret: process.env.PAYLOAD_SECRET ?? "ridu-blocks-benchmark-secret",
	telemetry: false,
	typescript: {
		autoGenerate: false,
		outputFile: process.env.BLOCKS_TYPES_OUTPUT ?? path.resolve(dirname, "payload-types.ts"),
	},
});
