import { MigrateUpArgs, MigrateDownArgs, sql } from "@payloadcms/db-postgres";

export async function up({ db, payload, req }: MigrateUpArgs): Promise<void> {
	await db.execute(sql`
   ALTER TABLE "posts" ADD COLUMN "deleted_at" timestamp(3) with time zone;
  ALTER TABLE "_posts_v" ADD COLUMN "version_deleted_at" timestamp(3) with time zone;
  CREATE INDEX "posts_deleted_at_idx" ON "posts" USING btree ("deleted_at");
  CREATE INDEX "_posts_v_version_version_deleted_at_idx" ON "_posts_v" USING btree ("version_deleted_at");`);
}

export async function down({ db, payload, req }: MigrateDownArgs): Promise<void> {
	await db.execute(sql`
   DROP INDEX "posts_deleted_at_idx";
  DROP INDEX "_posts_v_version_version_deleted_at_idx";
  ALTER TABLE "posts" DROP COLUMN "deleted_at";
  ALTER TABLE "_posts_v" DROP COLUMN "version_deleted_at";`);
}
