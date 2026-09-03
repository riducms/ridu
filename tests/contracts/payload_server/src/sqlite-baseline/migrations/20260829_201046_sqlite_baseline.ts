import { MigrateUpArgs, MigrateDownArgs, sql } from "@payloadcms/db-sqlite";

export async function up({ db, payload, req }: MigrateUpArgs): Promise<void> {
	await db.run(sql`CREATE TABLE \`authors\` (
	\`id\` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	\`name\` text NOT NULL,
	\`updated_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
	\`created_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL
  );
  `);
	await db.run(sql`CREATE INDEX \`authors_updated_at_idx\` ON \`authors\` (\`updated_at\`);`);
	await db.run(sql`CREATE INDEX \`authors_created_at_idx\` ON \`authors\` (\`created_at\`);`);
	await db.run(sql`CREATE TABLE \`categories\` (
	\`id\` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	\`name\` text NOT NULL,
	\`updated_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
	\`created_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL
  );
  `);
	await db.run(sql`CREATE INDEX \`categories_updated_at_idx\` ON \`categories\` (\`updated_at\`);`);
	await db.run(sql`CREATE INDEX \`categories_created_at_idx\` ON \`categories\` (\`created_at\`);`);
	await db.run(sql`CREATE TABLE \`posts\` (
	\`id\` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	\`title\` text,
	\`summary\` text,
	\`author_id\` integer,
	\`category_id\` integer,
	\`updated_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
	\`created_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
	\`_status\` text DEFAULT 'draft',
	FOREIGN KEY (\`author_id\`) REFERENCES \`authors\`(\`id\`) ON UPDATE no action ON DELETE set null,
	FOREIGN KEY (\`category_id\`) REFERENCES \`categories\`(\`id\`) ON UPDATE no action ON DELETE set null
  );
  `);
	await db.run(sql`CREATE INDEX \`posts_author_idx\` ON \`posts\` (\`author_id\`);`);
	await db.run(sql`CREATE INDEX \`posts_category_idx\` ON \`posts\` (\`category_id\`);`);
	await db.run(sql`CREATE INDEX \`posts_updated_at_idx\` ON \`posts\` (\`updated_at\`);`);
	await db.run(sql`CREATE INDEX \`posts_created_at_idx\` ON \`posts\` (\`created_at\`);`);
	await db.run(sql`CREATE INDEX \`posts__status_idx\` ON \`posts\` (\`_status\`);`);
	await db.run(sql`CREATE TABLE \`_posts_v\` (
	\`id\` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	\`parent_id\` integer,
	\`version_title\` text,
	\`version_summary\` text,
	\`version_author_id\` integer,
	\`version_category_id\` integer,
	\`version_updated_at\` text,
	\`version_created_at\` text,
	\`version__status\` text DEFAULT 'draft',
	\`created_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
	\`updated_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
	\`latest\` integer,
	FOREIGN KEY (\`parent_id\`) REFERENCES \`posts\`(\`id\`) ON UPDATE no action ON DELETE set null,
	FOREIGN KEY (\`version_author_id\`) REFERENCES \`authors\`(\`id\`) ON UPDATE no action ON DELETE set null,
	FOREIGN KEY (\`version_category_id\`) REFERENCES \`categories\`(\`id\`) ON UPDATE no action ON DELETE set null
  );
  `);
	await db.run(sql`CREATE INDEX \`_posts_v_parent_idx\` ON \`_posts_v\` (\`parent_id\`);`);
	await db.run(
		sql`CREATE INDEX \`_posts_v_version_version_author_idx\` ON \`_posts_v\` (\`version_author_id\`);`
	);
	await db.run(
		sql`CREATE INDEX \`_posts_v_version_version_category_idx\` ON \`_posts_v\` (\`version_category_id\`);`
	);
	await db.run(
		sql`CREATE INDEX \`_posts_v_version_version_updated_at_idx\` ON \`_posts_v\` (\`version_updated_at\`);`
	);
	await db.run(
		sql`CREATE INDEX \`_posts_v_version_version_created_at_idx\` ON \`_posts_v\` (\`version_created_at\`);`
	);
	await db.run(
		sql`CREATE INDEX \`_posts_v_version_version__status_idx\` ON \`_posts_v\` (\`version__status\`);`
	);
	await db.run(sql`CREATE INDEX \`_posts_v_created_at_idx\` ON \`_posts_v\` (\`created_at\`);`);
	await db.run(sql`CREATE INDEX \`_posts_v_updated_at_idx\` ON \`_posts_v\` (\`updated_at\`);`);
	await db.run(sql`CREATE INDEX \`_posts_v_latest_idx\` ON \`_posts_v\` (\`latest\`);`);
	await db.run(sql`CREATE TABLE \`payload_kv\` (
	\`id\` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	\`key\` text NOT NULL,
	\`data\` text NOT NULL
  );
  `);
	await db.run(sql`CREATE UNIQUE INDEX \`payload_kv_key_idx\` ON \`payload_kv\` (\`key\`);`);
	await db.run(sql`CREATE TABLE \`users_sessions\` (
	\`_order\` integer NOT NULL,
	\`_parent_id\` integer NOT NULL,
	\`id\` text PRIMARY KEY NOT NULL,
	\`created_at\` text,
	\`expires_at\` text NOT NULL,
	FOREIGN KEY (\`_parent_id\`) REFERENCES \`users\`(\`id\`) ON UPDATE no action ON DELETE cascade
  );
  `);
	await db.run(sql`CREATE INDEX \`users_sessions_order_idx\` ON \`users_sessions\` (\`_order\`);`);
	await db.run(
		sql`CREATE INDEX \`users_sessions_parent_id_idx\` ON \`users_sessions\` (\`_parent_id\`);`
	);
	await db.run(sql`CREATE TABLE \`users\` (
	\`id\` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	\`updated_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
	\`created_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
	\`email\` text NOT NULL,
	\`reset_password_token\` text,
	\`reset_password_expiration\` text,
	\`salt\` text,
	\`hash\` text,
	\`login_attempts\` numeric DEFAULT 0,
	\`lock_until\` text
  );
  `);
	await db.run(sql`CREATE INDEX \`users_updated_at_idx\` ON \`users\` (\`updated_at\`);`);
	await db.run(sql`CREATE INDEX \`users_created_at_idx\` ON \`users\` (\`created_at\`);`);
	await db.run(sql`CREATE UNIQUE INDEX \`users_email_idx\` ON \`users\` (\`email\`);`);
	await db.run(sql`CREATE TABLE \`payload_locked_documents\` (
	\`id\` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	\`global_slug\` text,
	\`updated_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
	\`created_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL
  );
  `);
	await db.run(
		sql`CREATE INDEX \`payload_locked_documents_global_slug_idx\` ON \`payload_locked_documents\` (\`global_slug\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_locked_documents_updated_at_idx\` ON \`payload_locked_documents\` (\`updated_at\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_locked_documents_created_at_idx\` ON \`payload_locked_documents\` (\`created_at\`);`
	);
	await db.run(sql`CREATE TABLE \`payload_locked_documents_rels\` (
	\`id\` integer PRIMARY KEY NOT NULL,
	\`order\` integer,
	\`parent_id\` integer NOT NULL,
	\`path\` text NOT NULL,
	\`authors_id\` integer,
	\`categories_id\` integer,
	\`posts_id\` integer,
	\`users_id\` integer,
	FOREIGN KEY (\`parent_id\`) REFERENCES \`payload_locked_documents\`(\`id\`) ON UPDATE no action ON DELETE cascade,
	FOREIGN KEY (\`authors_id\`) REFERENCES \`authors\`(\`id\`) ON UPDATE no action ON DELETE cascade,
	FOREIGN KEY (\`categories_id\`) REFERENCES \`categories\`(\`id\`) ON UPDATE no action ON DELETE cascade,
	FOREIGN KEY (\`posts_id\`) REFERENCES \`posts\`(\`id\`) ON UPDATE no action ON DELETE cascade,
	FOREIGN KEY (\`users_id\`) REFERENCES \`users\`(\`id\`) ON UPDATE no action ON DELETE cascade
  );
  `);
	await db.run(
		sql`CREATE INDEX \`payload_locked_documents_rels_order_idx\` ON \`payload_locked_documents_rels\` (\`order\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_locked_documents_rels_parent_idx\` ON \`payload_locked_documents_rels\` (\`parent_id\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_locked_documents_rels_path_idx\` ON \`payload_locked_documents_rels\` (\`path\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_locked_documents_rels_authors_id_idx\` ON \`payload_locked_documents_rels\` (\`authors_id\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_locked_documents_rels_categories_id_idx\` ON \`payload_locked_documents_rels\` (\`categories_id\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_locked_documents_rels_posts_id_idx\` ON \`payload_locked_documents_rels\` (\`posts_id\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_locked_documents_rels_users_id_idx\` ON \`payload_locked_documents_rels\` (\`users_id\`);`
	);
	await db.run(sql`CREATE TABLE \`payload_preferences\` (
	\`id\` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	\`key\` text,
	\`value\` text,
	\`updated_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
	\`created_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL
  );
  `);
	await db.run(
		sql`CREATE INDEX \`payload_preferences_key_idx\` ON \`payload_preferences\` (\`key\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_preferences_updated_at_idx\` ON \`payload_preferences\` (\`updated_at\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_preferences_created_at_idx\` ON \`payload_preferences\` (\`created_at\`);`
	);
	await db.run(sql`CREATE TABLE \`payload_preferences_rels\` (
	\`id\` integer PRIMARY KEY NOT NULL,
	\`order\` integer,
	\`parent_id\` integer NOT NULL,
	\`path\` text NOT NULL,
	\`users_id\` integer,
	FOREIGN KEY (\`parent_id\`) REFERENCES \`payload_preferences\`(\`id\`) ON UPDATE no action ON DELETE cascade,
	FOREIGN KEY (\`users_id\`) REFERENCES \`users\`(\`id\`) ON UPDATE no action ON DELETE cascade
  );
  `);
	await db.run(
		sql`CREATE INDEX \`payload_preferences_rels_order_idx\` ON \`payload_preferences_rels\` (\`order\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_preferences_rels_parent_idx\` ON \`payload_preferences_rels\` (\`parent_id\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_preferences_rels_path_idx\` ON \`payload_preferences_rels\` (\`path\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_preferences_rels_users_id_idx\` ON \`payload_preferences_rels\` (\`users_id\`);`
	);
	await db.run(sql`CREATE TABLE \`payload_migrations\` (
	\`id\` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	\`name\` text,
	\`batch\` numeric,
	\`updated_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL,
	\`created_at\` text DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) NOT NULL
  );
  `);
	await db.run(
		sql`CREATE INDEX \`payload_migrations_updated_at_idx\` ON \`payload_migrations\` (\`updated_at\`);`
	);
	await db.run(
		sql`CREATE INDEX \`payload_migrations_created_at_idx\` ON \`payload_migrations\` (\`created_at\`);`
	);
}

export async function down({ db, payload, req }: MigrateDownArgs): Promise<void> {
	await db.run(sql`DROP TABLE \`payload_locked_documents_rels\`;`);
	await db.run(sql`DROP TABLE \`payload_preferences_rels\`;`);
	await db.run(sql`DROP TABLE \`users_sessions\`;`);
	await db.run(sql`DROP TABLE \`_posts_v\`;`);
	await db.run(sql`DROP TABLE \`posts\`;`);
	await db.run(sql`DROP TABLE \`payload_locked_documents\`;`);
	await db.run(sql`DROP TABLE \`payload_preferences\`;`);
	await db.run(sql`DROP TABLE \`users\`;`);
	await db.run(sql`DROP TABLE \`authors\`;`);
	await db.run(sql`DROP TABLE \`categories\`;`);
	await db.run(sql`DROP TABLE \`payload_kv\`;`);
	await db.run(sql`DROP TABLE \`payload_migrations\`;`);
}
