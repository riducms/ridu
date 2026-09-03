import { execFile } from "node:child_process";
import { cp, mkdtemp, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import { fileURLToPath, pathToFileURL } from "node:url";

const execFileAsync = promisify(execFile);
const dirname = path.dirname(fileURLToPath(import.meta.url));
const fixtureRoot = path.resolve(dirname, "../..");
const committedMigrationDir = path.resolve(dirname, "migrations");
const configPath = path.resolve(dirname, "payload.config.ts");
const payloadCLI = path.resolve(fixtureRoot, "node_modules/.bin/payload");

type ID = number | string;

type Author = { id: ID; name: string };
type Category = { id: ID; name: string };
type Post = {
	author: Author | ID;
	category: Category | ID;
	id: ID;
	summary?: null | string;
	title: string;
};
type Page<T> = { docs: T[]; totalDocs: number };
type SQLiteCountResult = { rows: Array<{ table_count?: bigint | number | string }> };

// The fixture's main config owns its generated global Payload types. Keep this
// alternate config isolated with the smallest Local API surface its contract uses.
type BaselinePayload = {
	create(args: { collection: "authors"; data: { name: string } }): Promise<Author>;
	create(args: { collection: "categories"; data: { name: string } }): Promise<Category>;
	create(args: {
		collection: "posts";
		data: {
			_status: "draft";
			author: ID;
			category: ID;
			summary: string;
			title: string;
		};
		draft: true;
	}): Promise<Post>;
	db: {
		client: { execute(statement: string): Promise<SQLiteCountResult> };
		migrate(): Promise<void>;
		migrateDown(): Promise<void>;
	};
	delete(args: { collection: "posts"; id: ID }): Promise<Post>;
	destroy(): Promise<void>;
	find(args: {
		collection: "payload-migrations";
		limit: number;
		overrideAccess: true;
	}): Promise<Page<{ id: ID; name?: null | string }>>;
	find(args: {
		collection: "posts";
		depth: 0;
		draft: true;
		where: { title: { equals: string } };
	}): Promise<Page<Post>>;
	findByID(args: { collection: "posts"; depth: 0; draft: true; id: ID }): Promise<Post>;
	findByID(args: { collection: "posts"; depth: 1; draft: true; id: ID }): Promise<Post>;
	findByID(args: { collection: "posts"; draft: true; id: ID }): Promise<Post>;
	findVersions(args: {
		collection: "posts";
		limit: number;
		where: { parent: { equals: ID } };
	}): Promise<Page<{ version: Post }>>;
	update(args: {
		collection: "posts";
		data: { summary: string };
		draft: true;
		id: ID;
	}): Promise<Post>;
};

function invariant(condition: unknown, message: string): asserts condition {
	if (!condition) {
		throw new Error(message);
	}
}

function relationshipID(value: unknown): string | number | undefined {
	if (typeof value === "string" || typeof value === "number") {
		return value;
	}
	if (value && typeof value === "object" && "id" in value) {
		const id = value.id;
		return typeof id === "string" || typeof id === "number" ? id : undefined;
	}
	return undefined;
}

function relationshipName(value: unknown): string | undefined {
	if (value && typeof value === "object" && "name" in value && typeof value.name === "string") {
		return value.name;
	}
	return undefined;
}

async function assertMigrationArtifactsHaveNoDrift(workspace: string): Promise<void> {
	const scratchMigrationDir = path.join(workspace, "migration-drift");
	await cp(committedMigrationDir, scratchMigrationDir, { recursive: true });
	const before = (await readdir(scratchMigrationDir)).sort();

	await execFileAsync(payloadCLI, ["migrate:create", "sqlite_baseline_drift", "--skip-empty"], {
		cwd: fixtureRoot,
		env: {
			...process.env,
			PAYLOAD_CONFIG_PATH: configPath,
			PAYLOAD_SQLITE_BASELINE_MIGRATION_DIR: scratchMigrationDir,
			PAYLOAD_SQLITE_BASELINE_URL: `file:${path.join(workspace, "drift.db")}`,
		},
	});

	const after = (await readdir(scratchMigrationDir)).sort();
	invariant(
		JSON.stringify(after) === JSON.stringify(before),
		`Payload SQLite migration artifacts drifted: expected ${before.join(", ")}; got ${after.join(", ")}`
	);
}

async function run(): Promise<void> {
	const workspace = await mkdtemp(path.join(tmpdir(), "ridu-payload-sqlite-baseline-"));
	const databasePath = path.join(workspace, "payload-baseline.db");

	try {
		await assertMigrationArtifactsHaveNoDrift(workspace);

		Object.assign(process.env, {
			NODE_ENV: "production",
			PAYLOAD_MIGRATING: "true",
			PAYLOAD_SQLITE_BASELINE_URL: `file:${databasePath}`,
		});
		delete process.env.PAYLOAD_SQLITE_BASELINE_MIGRATION_DIR;

		const [{ default: config }, { default: payload }] = await Promise.all([
			import(pathToFileURL(configPath).href),
			import("payload"),
		]);

		await payload.init({ config, disableOnInit: true });
		const baseline = payload as unknown as BaselinePayload;

		try {
			await baseline.db.migrate();
			await baseline.db.migrate();

			const migrations = await baseline.find({
				collection: "payload-migrations",
				limit: 10,
				overrideAccess: true,
			});
			invariant(migrations.totalDocs === 1, "the committed SQLite migration must be recorded once");

			const author = await baseline.create({
				collection: "authors",
				data: { name: "Ada Lovelace" },
			});
			const category = await baseline.create({
				collection: "categories",
				data: { name: "Release notes" },
			});
			const post = await baseline.create({
				collection: "posts",
				data: {
					_status: "draft",
					author: author.id,
					category: category.id,
					summary: "Initial SQLite baseline",
					title: "Pinned Payload SQLite",
				},
				draft: true,
			});
			await baseline.create({
				collection: "posts",
				data: {
					_status: "draft",
					author: author.id,
					category: category.id,
					summary: "Nonmatching SQLite baseline",
					title: "Unrelated Payload SQLite",
				},
				draft: true,
			});

			const read = await baseline.findByID({
				collection: "posts",
				id: post.id,
				depth: 0,
				draft: true,
			});
			invariant(read.title === "Pinned Payload SQLite", "created post must be readable by ID");
			invariant(relationshipID(read.author) === author.id, "author ID must survive a depth-0 read");
			invariant(
				relationshipID(read.category) === category.id,
				"category ID must survive a depth-0 read"
			);

			const listed = await baseline.find({
				collection: "posts",
				depth: 0,
				draft: true,
				where: { title: { equals: "Pinned Payload SQLite" } },
			});
			invariant(listed.totalDocs === 1, "filtered list query must exclude the nonmatching post");
			invariant(listed.docs.length === 1, "filtered list query must return exactly one post");
			invariant(
				listed.docs[0]?.id === post.id,
				"filtered list query must return the created post ID"
			);
			invariant(
				listed.docs[0]?.title === "Pinned Payload SQLite",
				"filtered list query must return the created post title"
			);

			await baseline.update({
				collection: "posts",
				id: post.id,
				data: { summary: "Updated SQLite baseline" },
				draft: true,
			});

			const populated = await baseline.findByID({
				collection: "posts",
				id: post.id,
				depth: 1,
				draft: true,
			});
			invariant(populated.summary === "Updated SQLite baseline", "updated post must be readable");
			invariant(
				relationshipID(populated.author) === author.id,
				"populated author must preserve its ID"
			);
			invariant(
				relationshipName(populated.author) === "Ada Lovelace",
				"author relationship must populate"
			);
			invariant(
				relationshipID(populated.category) === category.id,
				"populated category must preserve its ID"
			);
			invariant(
				relationshipName(populated.category) === "Release notes",
				"category relationship must populate"
			);

			const versions = await baseline.findVersions({
				collection: "posts",
				limit: 10,
				where: { parent: { equals: post.id } },
			});
			invariant(versions.totalDocs === 2, "create and update must retain two post versions");
			invariant(
				versions.docs.some((version) => version.version.summary === "Initial SQLite baseline"),
				"the original post version must be retained"
			);
			invariant(
				versions.docs.some((version) => version.version.summary === "Updated SQLite baseline"),
				"the updated post version must be retained"
			);

			await baseline.delete({ collection: "posts", id: post.id });
			let deletedReadFailed = false;
			try {
				await baseline.findByID({ collection: "posts", id: post.id, draft: true });
			} catch {
				deletedReadFailed = true;
			}
			invariant(deletedReadFailed, "deleted post must no longer be readable");

			await baseline.db.migrateDown();
			const downState = await baseline.db.client.execute(
				"SELECT count(*) AS table_count FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
			);
			invariant(
				Number(downState.rows[0]?.table_count) === 0,
				"rolling down must remove the migration ledger and application schema before reapply"
			);
			await baseline.db.migrate();
			const replayedMigrations = await baseline.find({
				collection: "payload-migrations",
				limit: 10,
				overrideAccess: true,
			});
			invariant(
				replayedMigrations.totalDocs === 1,
				"the SQLite migration must apply cleanly after rolling down"
			);
		} finally {
			await baseline.destroy();
		}

		console.log(
			"Payload SQLite side-by-side baseline passed: committed up/down/replay, CRUD, versions, and author/category relationships."
		);
	} finally {
		await rm(workspace, { force: true, recursive: true });
	}
}

await run();
