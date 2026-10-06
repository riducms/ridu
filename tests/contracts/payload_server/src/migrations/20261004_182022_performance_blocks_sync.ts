import { MigrateUpArgs, MigrateDownArgs, sql } from "@payloadcms/db-postgres";

export async function up({ db, payload, req }: MigrateUpArgs): Promise<void> {
	await db.execute(sql`
   CREATE TYPE "public"."enum_block_articles_status" AS ENUM('draft', 'published');
  CREATE TYPE "public"."enum__block_articles_v_version_status" AS ENUM('draft', 'published');
  CREATE TYPE "public"."enum__block_articles_v_published_locale" AS ENUM('en', 'es');
  CREATE TABLE "block_articles" (
  	"id" serial PRIMARY KEY NOT NULL,
  	"title" varchar,
  	"caption" varchar,
  	"body" jsonb,
  	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
  	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
  	"_status" "enum_block_articles_status" DEFAULT 'draft'
  );
  
  CREATE TABLE "block_articles_locales" (
  	"localized_body" jsonb,
  	"id" serial PRIMARY KEY NOT NULL,
  	"_locale" "_locales" NOT NULL,
  	"_parent_id" integer NOT NULL
  );
  
  CREATE TABLE "_block_articles_v" (
  	"id" serial PRIMARY KEY NOT NULL,
  	"parent_id" integer,
  	"version_title" varchar,
  	"version_caption" varchar,
  	"version_body" jsonb,
  	"version_updated_at" timestamp(3) with time zone,
  	"version_created_at" timestamp(3) with time zone,
  	"version__status" "enum__block_articles_v_version_status" DEFAULT 'draft',
  	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
  	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
  	"snapshot" boolean,
  	"published_locale" "enum__block_articles_v_published_locale",
  	"latest" boolean
  );
  
  CREATE TABLE "_block_articles_v_locales" (
  	"version_localized_body" jsonb,
  	"id" serial PRIMARY KEY NOT NULL,
  	"_locale" "_locales" NOT NULL,
  	"_parent_id" integer NOT NULL
  );
  
  ALTER TABLE "payload_locked_documents_rels" ADD COLUMN "block_articles_id" integer;
  ALTER TABLE "block_articles_locales" ADD CONSTRAINT "block_articles_locales_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."block_articles"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_block_articles_v" ADD CONSTRAINT "_block_articles_v_parent_id_block_articles_id_fk" FOREIGN KEY ("parent_id") REFERENCES "public"."block_articles"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "_block_articles_v_locales" ADD CONSTRAINT "_block_articles_v_locales_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."_block_articles_v"("id") ON DELETE cascade ON UPDATE no action;
  CREATE INDEX "block_articles_updated_at_idx" ON "block_articles" USING btree ("updated_at");
  CREATE INDEX "block_articles_created_at_idx" ON "block_articles" USING btree ("created_at");
  CREATE INDEX "block_articles__status_idx" ON "block_articles" USING btree ("_status");
  CREATE UNIQUE INDEX "block_articles_locales_locale_parent_id_unique" ON "block_articles_locales" USING btree ("_locale","_parent_id");
  CREATE INDEX "_block_articles_v_parent_idx" ON "_block_articles_v" USING btree ("parent_id");
  CREATE INDEX "_block_articles_v_version_version_updated_at_idx" ON "_block_articles_v" USING btree ("version_updated_at");
  CREATE INDEX "_block_articles_v_version_version_created_at_idx" ON "_block_articles_v" USING btree ("version_created_at");
  CREATE INDEX "_block_articles_v_version_version__status_idx" ON "_block_articles_v" USING btree ("version__status");
  CREATE INDEX "_block_articles_v_created_at_idx" ON "_block_articles_v" USING btree ("created_at");
  CREATE INDEX "_block_articles_v_updated_at_idx" ON "_block_articles_v" USING btree ("updated_at");
  CREATE INDEX "_block_articles_v_snapshot_idx" ON "_block_articles_v" USING btree ("snapshot");
  CREATE INDEX "_block_articles_v_published_locale_idx" ON "_block_articles_v" USING btree ("published_locale");
  CREATE INDEX "_block_articles_v_latest_idx" ON "_block_articles_v" USING btree ("latest");
  CREATE UNIQUE INDEX "_block_articles_v_locales_locale_parent_id_unique" ON "_block_articles_v_locales" USING btree ("_locale","_parent_id");
  ALTER TABLE "payload_locked_documents_rels" ADD CONSTRAINT "payload_locked_documents_rels_block_articles_fk" FOREIGN KEY ("block_articles_id") REFERENCES "public"."block_articles"("id") ON DELETE cascade ON UPDATE no action;
  CREATE INDEX "payload_locked_documents_rels_block_articles_id_idx" ON "payload_locked_documents_rels" USING btree ("block_articles_id");`);
}

export async function down({ db, payload, req }: MigrateDownArgs): Promise<void> {
	await db.execute(sql`
   ALTER TABLE "payload_locked_documents_rels" DROP CONSTRAINT "payload_locked_documents_rels_block_articles_fk";
  ALTER TABLE "block_articles" DISABLE ROW LEVEL SECURITY;
  ALTER TABLE "block_articles_locales" DISABLE ROW LEVEL SECURITY;
  ALTER TABLE "_block_articles_v" DISABLE ROW LEVEL SECURITY;
  ALTER TABLE "_block_articles_v_locales" DISABLE ROW LEVEL SECURITY;
  DROP TABLE "block_articles" CASCADE;
  DROP TABLE "block_articles_locales" CASCADE;
  DROP TABLE "_block_articles_v" CASCADE;
  DROP TABLE "_block_articles_v_locales" CASCADE;
  DROP INDEX "payload_locked_documents_rels_block_articles_id_idx";
  ALTER TABLE "payload_locked_documents_rels" DROP COLUMN "block_articles_id";
  DROP TYPE "public"."enum_block_articles_status";
  DROP TYPE "public"."enum__block_articles_v_version_status";
  DROP TYPE "public"."enum__block_articles_v_published_locale";`);
}
