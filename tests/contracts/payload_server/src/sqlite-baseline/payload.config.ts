import { sqliteAdapter } from "@payloadcms/db-sqlite";
import { lexicalEditor } from "@payloadcms/richtext-lexical";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { buildConfig, type CollectionConfig } from "payload";

const filename = fileURLToPath(import.meta.url);
const dirname = path.dirname(filename);
const databaseURL = process.env.PAYLOAD_SQLITE_BASELINE_URL;

if (!databaseURL?.startsWith("file:")) {
	throw new Error("PAYLOAD_SQLITE_BASELINE_URL must be a local file: SQLite URL");
}

const migrationDir = process.env.PAYLOAD_SQLITE_BASELINE_MIGRATION_DIR
	? path.resolve(process.env.PAYLOAD_SQLITE_BASELINE_MIGRATION_DIR)
	: path.resolve(dirname, "migrations");

export default buildConfig({
	collections: [
		{
			slug: "authors",
			fields: [{ name: "name", type: "text", required: true }],
		},
		{
			slug: "categories",
			fields: [{ name: "name", type: "text", required: true }],
		},
		{
			slug: "posts",
			fields: [
				{ name: "title", type: "text", required: true },
				{ name: "summary", type: "textarea" },
				{ name: "author", type: "relationship", relationTo: "authors", required: true },
				{ name: "category", type: "relationship", relationTo: "categories", required: true },
			],
			versions: { drafts: true },
		},
	] as unknown as CollectionConfig[],
	db: sqliteAdapter({
		autoIncrement: true,
		client: { url: databaseURL },
		migrationDir,
		push: false,
	}),
	editor: lexicalEditor({}),
	secret: "ridu-payload-sqlite-baseline-test-secret",
	telemetry: false,
	typescript: { autoGenerate: false },
});
