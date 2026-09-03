import { MigrateUpArgs, MigrateDownArgs, sql } from "@payloadcms/db-postgres";

export async function up({ db, payload, req }: MigrateUpArgs): Promise<void> {
	await db.execute(sql`
   CREATE TYPE "public"."_locales" AS ENUM('en', 'es');
  CREATE TYPE "public"."enum_users_role" AS ENUM('administrator', 'editor', 'contributor');
  CREATE TYPE "public"."enum_media_kind" AS ENUM('image', 'illustration', 'screenshot');
  CREATE TYPE "public"."enum_posts_blocks_callout_tone" AS ENUM('note', 'warning', 'success');
  CREATE TYPE "public"."enum_posts_status" AS ENUM('draft', 'published');
  CREATE TYPE "public"."enum__posts_v_blocks_callout_tone" AS ENUM('note', 'warning', 'success');
  CREATE TYPE "public"."enum__posts_v_version_status" AS ENUM('draft', 'published');
  CREATE TYPE "public"."enum__posts_v_published_locale" AS ENUM('en', 'es');
  CREATE TYPE "public"."enum_pages_template" AS ENUM('default', 'landing', 'legal');
  CREATE TYPE "public"."enum_pages_status" AS ENUM('draft', 'published');
  CREATE TYPE "public"."enum__pages_v_version_template" AS ENUM('default', 'landing', 'legal');
  CREATE TYPE "public"."enum__pages_v_version_status" AS ENUM('draft', 'published');
  CREATE TYPE "public"."enum__pages_v_published_locale" AS ENUM('en', 'es');
  CREATE TYPE "public"."enum_redirects_type" AS ENUM('permanent', 'temporary');
  CREATE TYPE "public"."enum_payload_capabilities_presentation" AS ENUM('article', 'gallery', 'video');
  CREATE TYPE "public"."enum_payload_jobs_log_task_slug" AS ENUM('inline', 'schedulePublish');
  CREATE TYPE "public"."enum_payload_jobs_log_state" AS ENUM('failed', 'succeeded');
  CREATE TYPE "public"."enum_payload_jobs_task_slug" AS ENUM('inline', 'schedulePublish');
  CREATE TYPE "public"."enum_site_settings_status" AS ENUM('draft', 'published');
  CREATE TYPE "public"."enum__site_settings_v_version_status" AS ENUM('draft', 'published');
  CREATE TYPE "public"."enum__site_settings_v_published_locale" AS ENUM('en', 'es');
  CREATE TABLE "users_sessions" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"created_at" timestamp(3) with time zone,
	"expires_at" timestamp(3) with time zone NOT NULL
  );

  CREATE TABLE "users" (
	"id" serial PRIMARY KEY NOT NULL,
	"name" varchar NOT NULL,
	"role" "enum_users_role" DEFAULT 'contributor' NOT NULL,
	"contact_email" varchar,
	"profile_bio" varchar,
	"profile_location" varchar,
	"profile_timezone" varchar DEFAULT 'Europe/London',
	"profile_available_for_review" boolean DEFAULT true,
	"private_notes" varchar,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"email" varchar NOT NULL,
	"reset_password_token" varchar,
	"reset_password_expiration" timestamp(3) with time zone,
	"salt" varchar,
	"hash" varchar,
	"login_attempts" numeric DEFAULT 0,
	"lock_until" timestamp(3) with time zone
  );

  CREATE TABLE "media_tags" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"label" varchar NOT NULL
  );

  CREATE TABLE "media" (
	"id" serial PRIMARY KEY NOT NULL,
	"alt" varchar NOT NULL,
	"caption" varchar,
	"credit_id" integer,
	"kind" "enum_media_kind" DEFAULT 'image',
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"url" varchar,
	"thumbnail_u_r_l" varchar,
	"filename" varchar,
	"mime_type" varchar,
	"filesize" numeric,
	"width" numeric,
	"height" numeric,
	"focal_x" numeric,
	"focal_y" numeric,
	"sizes_card_url" varchar,
	"sizes_card_width" numeric,
	"sizes_card_height" numeric,
	"sizes_card_mime_type" varchar,
	"sizes_card_filesize" numeric,
	"sizes_card_filename" varchar,
	"sizes_thumbnail_url" varchar,
	"sizes_thumbnail_width" numeric,
	"sizes_thumbnail_height" numeric,
	"sizes_thumbnail_mime_type" varchar,
	"sizes_thumbnail_filesize" numeric,
	"sizes_thumbnail_filename" varchar
  );

  CREATE TABLE "categories" (
	"id" serial PRIMARY KEY NOT NULL,
	"name" varchar NOT NULL,
	"slug" varchar NOT NULL,
	"seo_title" varchar,
	"seo_canonical_u_r_l" varchar,
	"seo_description" varchar,
	"seo_social_image_id" integer,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL
  );

  CREATE TABLE "posts_links" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"label" varchar,
	"url" varchar,
	"new_window" boolean DEFAULT false
  );

  CREATE TABLE "posts_blocks_callout" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"_path" text NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"tone" "enum_posts_blocks_callout_tone" DEFAULT 'note',
	"body" varchar,
	"block_name" varchar
  );

  CREATE TABLE "posts_blocks_quote" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"_path" text NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"quote" varchar,
	"attribution" varchar,
	"block_name" varchar
  );

  CREATE TABLE "posts_blocks_related_posts" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"_path" text NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"block_name" varchar
  );

  CREATE TABLE "posts" (
	"id" serial PRIMARY KEY NOT NULL,
	"title" varchar,
	"slug" varchar,
	"summary" varchar,
	"author_id" integer,
	"category_id" integer,
	"status" "enum_posts_status" DEFAULT 'draft',
	"featured" boolean DEFAULT false,
	"reading_minutes" numeric DEFAULT 5,
	"publish_date" timestamp(3) with time zone,
	"cover_id" integer,
	"content" jsonb,
	"seo_title" varchar,
	"seo_canonical_u_r_l" varchar,
	"seo_description" varchar,
	"seo_social_image_id" integer,
	"metadata" jsonb,
	"internal_notes" varchar,
	"last_edited_by_id" integer,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"_status" "enum_posts_status" DEFAULT 'draft'
  );

  CREATE TABLE "posts_rels" (
	"id" serial PRIMARY KEY NOT NULL,
	"order" integer,
	"parent_id" integer NOT NULL,
	"path" varchar NOT NULL,
	"media_id" integer,
	"users_id" integer,
	"posts_id" integer,
	"pages_id" integer
  );

  CREATE TABLE "_posts_v_version_links" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"id" serial PRIMARY KEY NOT NULL,
	"label" varchar,
	"url" varchar,
	"new_window" boolean DEFAULT false,
	"_uuid" varchar
  );

  CREATE TABLE "_posts_v_blocks_callout" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"_path" text NOT NULL,
	"id" serial PRIMARY KEY NOT NULL,
	"tone" "enum__posts_v_blocks_callout_tone" DEFAULT 'note',
	"body" varchar,
	"_uuid" varchar,
	"block_name" varchar
  );

  CREATE TABLE "_posts_v_blocks_quote" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"_path" text NOT NULL,
	"id" serial PRIMARY KEY NOT NULL,
	"quote" varchar,
	"attribution" varchar,
	"_uuid" varchar,
	"block_name" varchar
  );

  CREATE TABLE "_posts_v_blocks_related_posts" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"_path" text NOT NULL,
	"id" serial PRIMARY KEY NOT NULL,
	"_uuid" varchar,
	"block_name" varchar
  );

  CREATE TABLE "_posts_v" (
	"id" serial PRIMARY KEY NOT NULL,
	"parent_id" integer,
	"version_title" varchar,
	"version_slug" varchar,
	"version_summary" varchar,
	"version_author_id" integer,
	"version_category_id" integer,
	"version_status" "enum__posts_v_version_status" DEFAULT 'draft',
	"version_featured" boolean DEFAULT false,
	"version_reading_minutes" numeric DEFAULT 5,
	"version_publish_date" timestamp(3) with time zone,
	"version_cover_id" integer,
	"version_content" jsonb,
	"version_seo_title" varchar,
	"version_seo_canonical_u_r_l" varchar,
	"version_seo_description" varchar,
	"version_seo_social_image_id" integer,
	"version_metadata" jsonb,
	"version_internal_notes" varchar,
	"version_last_edited_by_id" integer,
	"version_updated_at" timestamp(3) with time zone,
	"version_created_at" timestamp(3) with time zone,
	"version__status" "enum__posts_v_version_status" DEFAULT 'draft',
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"snapshot" boolean,
	"published_locale" "enum__posts_v_published_locale",
	"latest" boolean,
	"autosave" boolean
  );

  CREATE TABLE "_posts_v_rels" (
	"id" serial PRIMARY KEY NOT NULL,
	"order" integer,
	"parent_id" integer NOT NULL,
	"path" varchar NOT NULL,
	"media_id" integer,
	"users_id" integer,
	"posts_id" integer,
	"pages_id" integer
  );

  CREATE TABLE "pages_blocks_hero" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"_path" text NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"heading" varchar,
	"lede" varchar,
	"image_id" integer,
	"block_name" varchar
  );

  CREATE TABLE "pages_blocks_rich_text" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"_path" text NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"body" jsonb,
	"block_name" varchar
  );

  CREATE TABLE "pages_blocks_featured_post" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"_path" text NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"post_id" integer,
	"block_name" varchar
  );

  CREATE TABLE "pages" (
	"id" serial PRIMARY KEY NOT NULL,
	"title" varchar,
	"slug" varchar,
	"template" "enum_pages_template" DEFAULT 'default',
	"navigation_label" varchar,
	"navigation_show_in_header" boolean DEFAULT false,
	"navigation_show_in_footer" boolean DEFAULT false,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"_status" "enum_pages_status" DEFAULT 'draft'
  );

  CREATE TABLE "_pages_v_blocks_hero" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"_path" text NOT NULL,
	"id" serial PRIMARY KEY NOT NULL,
	"heading" varchar,
	"lede" varchar,
	"image_id" integer,
	"_uuid" varchar,
	"block_name" varchar
  );

  CREATE TABLE "_pages_v_blocks_rich_text" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"_path" text NOT NULL,
	"id" serial PRIMARY KEY NOT NULL,
	"body" jsonb,
	"_uuid" varchar,
	"block_name" varchar
  );

  CREATE TABLE "_pages_v_blocks_featured_post" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"_path" text NOT NULL,
	"id" serial PRIMARY KEY NOT NULL,
	"post_id" integer,
	"_uuid" varchar,
	"block_name" varchar
  );

  CREATE TABLE "_pages_v" (
	"id" serial PRIMARY KEY NOT NULL,
	"parent_id" integer,
	"version_title" varchar,
	"version_slug" varchar,
	"version_template" "enum__pages_v_version_template" DEFAULT 'default',
	"version_navigation_label" varchar,
	"version_navigation_show_in_header" boolean DEFAULT false,
	"version_navigation_show_in_footer" boolean DEFAULT false,
	"version_updated_at" timestamp(3) with time zone,
	"version_created_at" timestamp(3) with time zone,
	"version__status" "enum__pages_v_version_status" DEFAULT 'draft',
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"snapshot" boolean,
	"published_locale" "enum__pages_v_published_locale",
	"latest" boolean,
	"autosave" boolean
  );

  CREATE TABLE "events_schedule" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"time" timestamp(3) with time zone NOT NULL,
	"title" varchar NOT NULL,
	"speaker_id" integer
  );

  CREATE TABLE "events" (
	"id" serial PRIMARY KEY NOT NULL,
	"name" varchar NOT NULL,
	"starts_at" timestamp(3) with time zone NOT NULL,
	"ends_at" timestamp(3) with time zone,
	"capacity" numeric DEFAULT 50,
	"online" boolean DEFAULT false,
	"venue" varchar,
	"meeting_u_r_l" varchar,
	"contact" varchar NOT NULL,
	"registration_settings" jsonb,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL
  );

  CREATE TABLE "editorial_notes" (
	"id" serial PRIMARY KEY NOT NULL,
	"title" varchar NOT NULL,
	"owner_id" integer NOT NULL,
	"note" varchar NOT NULL,
	"confidential_details" varchar,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL
  );

  CREATE TABLE "redirects" (
	"id" serial PRIMARY KEY NOT NULL,
	"from" varchar NOT NULL,
	"to" varchar NOT NULL,
	"type" "enum_redirects_type" DEFAULT 'permanent',
	"enabled" boolean DEFAULT true,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL
  );

  CREATE TABLE "payload_capabilities_bounded_rows" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"label" varchar NOT NULL
  );

  CREATE TABLE "payload_capabilities" (
	"id" serial PRIMARY KEY NOT NULL,
	"presentation" "enum_payload_capabilities_presentation",
	"location" geometry(Point),
	"source_code" varchar,
	"minimum" numeric,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"deleted_at" timestamp(3) with time zone
  );

  CREATE TABLE "payload_capabilities_locales" (
	"title" varchar NOT NULL,
	"id" serial PRIMARY KEY NOT NULL,
	"_locale" "_locales" NOT NULL,
	"_parent_id" integer NOT NULL
  );

  CREATE TABLE "payload_kv" (
	"id" serial PRIMARY KEY NOT NULL,
	"key" varchar NOT NULL,
	"data" jsonb NOT NULL
  );

  CREATE TABLE "payload_jobs_log" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"executed_at" timestamp(3) with time zone NOT NULL,
	"completed_at" timestamp(3) with time zone NOT NULL,
	"task_slug" "enum_payload_jobs_log_task_slug" NOT NULL,
	"task_i_d" varchar NOT NULL,
	"input" jsonb,
	"output" jsonb,
	"state" "enum_payload_jobs_log_state" NOT NULL,
	"error" jsonb
  );

  CREATE TABLE "payload_jobs" (
	"id" serial PRIMARY KEY NOT NULL,
	"input" jsonb,
	"completed_at" timestamp(3) with time zone,
	"total_tried" numeric DEFAULT 0,
	"has_error" boolean DEFAULT false,
	"error" jsonb,
	"task_slug" "enum_payload_jobs_task_slug",
	"queue" varchar DEFAULT 'default',
	"wait_until" timestamp(3) with time zone,
	"processing" boolean DEFAULT false,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL
  );

  CREATE TABLE "payload_locked_documents" (
	"id" serial PRIMARY KEY NOT NULL,
	"global_slug" varchar,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL
  );

  CREATE TABLE "payload_locked_documents_rels" (
	"id" serial PRIMARY KEY NOT NULL,
	"order" integer,
	"parent_id" integer NOT NULL,
	"path" varchar NOT NULL,
	"users_id" integer,
	"media_id" integer,
	"categories_id" integer,
	"posts_id" integer,
	"pages_id" integer,
	"events_id" integer,
	"editorial_notes_id" integer,
	"redirects_id" integer,
	"payload_capabilities_id" integer
  );

  CREATE TABLE "payload_preferences" (
	"id" serial PRIMARY KEY NOT NULL,
	"key" varchar,
	"value" jsonb,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL
  );

  CREATE TABLE "payload_preferences_rels" (
	"id" serial PRIMARY KEY NOT NULL,
	"order" integer,
	"parent_id" integer NOT NULL,
	"path" varchar NOT NULL,
	"users_id" integer
  );

  CREATE TABLE "payload_migrations" (
	"id" serial PRIMARY KEY NOT NULL,
	"name" varchar,
	"batch" numeric,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL
  );

  CREATE TABLE "site_settings_navigation" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"id" varchar PRIMARY KEY NOT NULL,
	"label" varchar,
	"page_id" integer
  );

  CREATE TABLE "site_settings" (
	"id" serial PRIMARY KEY NOT NULL,
	"site_name" varchar,
	"_status" "enum_site_settings_status" DEFAULT 'draft',
	"updated_at" timestamp(3) with time zone,
	"created_at" timestamp(3) with time zone
  );

  CREATE TABLE "site_settings_locales" (
	"announcement" varchar,
	"id" serial PRIMARY KEY NOT NULL,
	"_locale" "_locales" NOT NULL,
	"_parent_id" integer NOT NULL
  );

  CREATE TABLE "_site_settings_v_version_navigation" (
	"_order" integer NOT NULL,
	"_parent_id" integer NOT NULL,
	"id" serial PRIMARY KEY NOT NULL,
	"label" varchar,
	"page_id" integer,
	"_uuid" varchar
  );

  CREATE TABLE "_site_settings_v" (
	"id" serial PRIMARY KEY NOT NULL,
	"version_site_name" varchar,
	"version__status" "enum__site_settings_v_version_status" DEFAULT 'draft',
	"version_updated_at" timestamp(3) with time zone,
	"version_created_at" timestamp(3) with time zone,
	"created_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"updated_at" timestamp(3) with time zone DEFAULT now() NOT NULL,
	"snapshot" boolean,
	"published_locale" "enum__site_settings_v_published_locale",
	"latest" boolean
  );

  CREATE TABLE "_site_settings_v_locales" (
	"version_announcement" varchar,
	"id" serial PRIMARY KEY NOT NULL,
	"_locale" "_locales" NOT NULL,
	"_parent_id" integer NOT NULL
  );

  ALTER TABLE "users_sessions" ADD CONSTRAINT "users_sessions_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."users"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "media_tags" ADD CONSTRAINT "media_tags_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."media"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "media" ADD CONSTRAINT "media_credit_id_users_id_fk" FOREIGN KEY ("credit_id") REFERENCES "public"."users"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "categories" ADD CONSTRAINT "categories_seo_social_image_id_media_id_fk" FOREIGN KEY ("seo_social_image_id") REFERENCES "public"."media"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "posts_links" ADD CONSTRAINT "posts_links_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."posts"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "posts_blocks_callout" ADD CONSTRAINT "posts_blocks_callout_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."posts"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "posts_blocks_quote" ADD CONSTRAINT "posts_blocks_quote_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."posts"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "posts_blocks_related_posts" ADD CONSTRAINT "posts_blocks_related_posts_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."posts"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "posts" ADD CONSTRAINT "posts_author_id_users_id_fk" FOREIGN KEY ("author_id") REFERENCES "public"."users"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "posts" ADD CONSTRAINT "posts_category_id_categories_id_fk" FOREIGN KEY ("category_id") REFERENCES "public"."categories"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "posts" ADD CONSTRAINT "posts_cover_id_media_id_fk" FOREIGN KEY ("cover_id") REFERENCES "public"."media"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "posts" ADD CONSTRAINT "posts_seo_social_image_id_media_id_fk" FOREIGN KEY ("seo_social_image_id") REFERENCES "public"."media"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "posts" ADD CONSTRAINT "posts_last_edited_by_id_users_id_fk" FOREIGN KEY ("last_edited_by_id") REFERENCES "public"."users"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "posts_rels" ADD CONSTRAINT "posts_rels_parent_fk" FOREIGN KEY ("parent_id") REFERENCES "public"."posts"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "posts_rels" ADD CONSTRAINT "posts_rels_media_fk" FOREIGN KEY ("media_id") REFERENCES "public"."media"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "posts_rels" ADD CONSTRAINT "posts_rels_users_fk" FOREIGN KEY ("users_id") REFERENCES "public"."users"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "posts_rels" ADD CONSTRAINT "posts_rels_posts_fk" FOREIGN KEY ("posts_id") REFERENCES "public"."posts"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "posts_rels" ADD CONSTRAINT "posts_rels_pages_fk" FOREIGN KEY ("pages_id") REFERENCES "public"."pages"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_posts_v_version_links" ADD CONSTRAINT "_posts_v_version_links_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."_posts_v"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_posts_v_blocks_callout" ADD CONSTRAINT "_posts_v_blocks_callout_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."_posts_v"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_posts_v_blocks_quote" ADD CONSTRAINT "_posts_v_blocks_quote_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."_posts_v"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_posts_v_blocks_related_posts" ADD CONSTRAINT "_posts_v_blocks_related_posts_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."_posts_v"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_posts_v" ADD CONSTRAINT "_posts_v_parent_id_posts_id_fk" FOREIGN KEY ("parent_id") REFERENCES "public"."posts"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "_posts_v" ADD CONSTRAINT "_posts_v_version_author_id_users_id_fk" FOREIGN KEY ("version_author_id") REFERENCES "public"."users"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "_posts_v" ADD CONSTRAINT "_posts_v_version_category_id_categories_id_fk" FOREIGN KEY ("version_category_id") REFERENCES "public"."categories"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "_posts_v" ADD CONSTRAINT "_posts_v_version_cover_id_media_id_fk" FOREIGN KEY ("version_cover_id") REFERENCES "public"."media"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "_posts_v" ADD CONSTRAINT "_posts_v_version_seo_social_image_id_media_id_fk" FOREIGN KEY ("version_seo_social_image_id") REFERENCES "public"."media"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "_posts_v" ADD CONSTRAINT "_posts_v_version_last_edited_by_id_users_id_fk" FOREIGN KEY ("version_last_edited_by_id") REFERENCES "public"."users"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "_posts_v_rels" ADD CONSTRAINT "_posts_v_rels_parent_fk" FOREIGN KEY ("parent_id") REFERENCES "public"."_posts_v"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_posts_v_rels" ADD CONSTRAINT "_posts_v_rels_media_fk" FOREIGN KEY ("media_id") REFERENCES "public"."media"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_posts_v_rels" ADD CONSTRAINT "_posts_v_rels_users_fk" FOREIGN KEY ("users_id") REFERENCES "public"."users"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_posts_v_rels" ADD CONSTRAINT "_posts_v_rels_posts_fk" FOREIGN KEY ("posts_id") REFERENCES "public"."posts"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_posts_v_rels" ADD CONSTRAINT "_posts_v_rels_pages_fk" FOREIGN KEY ("pages_id") REFERENCES "public"."pages"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "pages_blocks_hero" ADD CONSTRAINT "pages_blocks_hero_image_id_media_id_fk" FOREIGN KEY ("image_id") REFERENCES "public"."media"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "pages_blocks_hero" ADD CONSTRAINT "pages_blocks_hero_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."pages"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "pages_blocks_rich_text" ADD CONSTRAINT "pages_blocks_rich_text_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."pages"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "pages_blocks_featured_post" ADD CONSTRAINT "pages_blocks_featured_post_post_id_posts_id_fk" FOREIGN KEY ("post_id") REFERENCES "public"."posts"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "pages_blocks_featured_post" ADD CONSTRAINT "pages_blocks_featured_post_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."pages"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_pages_v_blocks_hero" ADD CONSTRAINT "_pages_v_blocks_hero_image_id_media_id_fk" FOREIGN KEY ("image_id") REFERENCES "public"."media"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "_pages_v_blocks_hero" ADD CONSTRAINT "_pages_v_blocks_hero_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."_pages_v"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_pages_v_blocks_rich_text" ADD CONSTRAINT "_pages_v_blocks_rich_text_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."_pages_v"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_pages_v_blocks_featured_post" ADD CONSTRAINT "_pages_v_blocks_featured_post_post_id_posts_id_fk" FOREIGN KEY ("post_id") REFERENCES "public"."posts"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "_pages_v_blocks_featured_post" ADD CONSTRAINT "_pages_v_blocks_featured_post_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."_pages_v"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_pages_v" ADD CONSTRAINT "_pages_v_parent_id_pages_id_fk" FOREIGN KEY ("parent_id") REFERENCES "public"."pages"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "events_schedule" ADD CONSTRAINT "events_schedule_speaker_id_users_id_fk" FOREIGN KEY ("speaker_id") REFERENCES "public"."users"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "events_schedule" ADD CONSTRAINT "events_schedule_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."events"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "editorial_notes" ADD CONSTRAINT "editorial_notes_owner_id_users_id_fk" FOREIGN KEY ("owner_id") REFERENCES "public"."users"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "payload_capabilities_bounded_rows" ADD CONSTRAINT "payload_capabilities_bounded_rows_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."payload_capabilities"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_capabilities_locales" ADD CONSTRAINT "payload_capabilities_locales_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."payload_capabilities"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_jobs_log" ADD CONSTRAINT "payload_jobs_log_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."payload_jobs"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_locked_documents_rels" ADD CONSTRAINT "payload_locked_documents_rels_parent_fk" FOREIGN KEY ("parent_id") REFERENCES "public"."payload_locked_documents"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_locked_documents_rels" ADD CONSTRAINT "payload_locked_documents_rels_users_fk" FOREIGN KEY ("users_id") REFERENCES "public"."users"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_locked_documents_rels" ADD CONSTRAINT "payload_locked_documents_rels_media_fk" FOREIGN KEY ("media_id") REFERENCES "public"."media"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_locked_documents_rels" ADD CONSTRAINT "payload_locked_documents_rels_categories_fk" FOREIGN KEY ("categories_id") REFERENCES "public"."categories"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_locked_documents_rels" ADD CONSTRAINT "payload_locked_documents_rels_posts_fk" FOREIGN KEY ("posts_id") REFERENCES "public"."posts"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_locked_documents_rels" ADD CONSTRAINT "payload_locked_documents_rels_pages_fk" FOREIGN KEY ("pages_id") REFERENCES "public"."pages"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_locked_documents_rels" ADD CONSTRAINT "payload_locked_documents_rels_events_fk" FOREIGN KEY ("events_id") REFERENCES "public"."events"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_locked_documents_rels" ADD CONSTRAINT "payload_locked_documents_rels_editorial_notes_fk" FOREIGN KEY ("editorial_notes_id") REFERENCES "public"."editorial_notes"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_locked_documents_rels" ADD CONSTRAINT "payload_locked_documents_rels_redirects_fk" FOREIGN KEY ("redirects_id") REFERENCES "public"."redirects"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_locked_documents_rels" ADD CONSTRAINT "payload_locked_documents_rels_payload_capabilities_fk" FOREIGN KEY ("payload_capabilities_id") REFERENCES "public"."payload_capabilities"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_preferences_rels" ADD CONSTRAINT "payload_preferences_rels_parent_fk" FOREIGN KEY ("parent_id") REFERENCES "public"."payload_preferences"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "payload_preferences_rels" ADD CONSTRAINT "payload_preferences_rels_users_fk" FOREIGN KEY ("users_id") REFERENCES "public"."users"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "site_settings_navigation" ADD CONSTRAINT "site_settings_navigation_page_id_pages_id_fk" FOREIGN KEY ("page_id") REFERENCES "public"."pages"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "site_settings_navigation" ADD CONSTRAINT "site_settings_navigation_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."site_settings"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "site_settings_locales" ADD CONSTRAINT "site_settings_locales_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."site_settings"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_site_settings_v_version_navigation" ADD CONSTRAINT "_site_settings_v_version_navigation_page_id_pages_id_fk" FOREIGN KEY ("page_id") REFERENCES "public"."pages"("id") ON DELETE set null ON UPDATE no action;
  ALTER TABLE "_site_settings_v_version_navigation" ADD CONSTRAINT "_site_settings_v_version_navigation_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."_site_settings_v"("id") ON DELETE cascade ON UPDATE no action;
  ALTER TABLE "_site_settings_v_locales" ADD CONSTRAINT "_site_settings_v_locales_parent_id_fk" FOREIGN KEY ("_parent_id") REFERENCES "public"."_site_settings_v"("id") ON DELETE cascade ON UPDATE no action;
  CREATE INDEX "users_sessions_order_idx" ON "users_sessions" USING btree ("_order");
  CREATE INDEX "users_sessions_parent_id_idx" ON "users_sessions" USING btree ("_parent_id");
  CREATE INDEX "users_updated_at_idx" ON "users" USING btree ("updated_at");
  CREATE INDEX "users_created_at_idx" ON "users" USING btree ("created_at");
  CREATE UNIQUE INDEX "users_email_idx" ON "users" USING btree ("email");
  CREATE INDEX "media_tags_order_idx" ON "media_tags" USING btree ("_order");
  CREATE INDEX "media_tags_parent_id_idx" ON "media_tags" USING btree ("_parent_id");
  CREATE INDEX "media_credit_idx" ON "media" USING btree ("credit_id");
  CREATE INDEX "media_updated_at_idx" ON "media" USING btree ("updated_at");
  CREATE INDEX "media_created_at_idx" ON "media" USING btree ("created_at");
  CREATE UNIQUE INDEX "media_filename_idx" ON "media" USING btree ("filename");
  CREATE INDEX "media_sizes_card_sizes_card_filename_idx" ON "media" USING btree ("sizes_card_filename");
  CREATE INDEX "media_sizes_thumbnail_sizes_thumbnail_filename_idx" ON "media" USING btree ("sizes_thumbnail_filename");
  CREATE UNIQUE INDEX "categories_slug_idx" ON "categories" USING btree ("slug");
  CREATE INDEX "categories_seo_seo_social_image_idx" ON "categories" USING btree ("seo_social_image_id");
  CREATE INDEX "categories_updated_at_idx" ON "categories" USING btree ("updated_at");
  CREATE INDEX "categories_created_at_idx" ON "categories" USING btree ("created_at");
  CREATE INDEX "posts_links_order_idx" ON "posts_links" USING btree ("_order");
  CREATE INDEX "posts_links_parent_id_idx" ON "posts_links" USING btree ("_parent_id");
  CREATE INDEX "posts_blocks_callout_order_idx" ON "posts_blocks_callout" USING btree ("_order");
  CREATE INDEX "posts_blocks_callout_parent_id_idx" ON "posts_blocks_callout" USING btree ("_parent_id");
  CREATE INDEX "posts_blocks_callout_path_idx" ON "posts_blocks_callout" USING btree ("_path");
  CREATE INDEX "posts_blocks_quote_order_idx" ON "posts_blocks_quote" USING btree ("_order");
  CREATE INDEX "posts_blocks_quote_parent_id_idx" ON "posts_blocks_quote" USING btree ("_parent_id");
  CREATE INDEX "posts_blocks_quote_path_idx" ON "posts_blocks_quote" USING btree ("_path");
  CREATE INDEX "posts_blocks_related_posts_order_idx" ON "posts_blocks_related_posts" USING btree ("_order");
  CREATE INDEX "posts_blocks_related_posts_parent_id_idx" ON "posts_blocks_related_posts" USING btree ("_parent_id");
  CREATE INDEX "posts_blocks_related_posts_path_idx" ON "posts_blocks_related_posts" USING btree ("_path");
  CREATE UNIQUE INDEX "posts_slug_idx" ON "posts" USING btree ("slug");
  CREATE INDEX "posts_author_idx" ON "posts" USING btree ("author_id");
  CREATE INDEX "posts_category_idx" ON "posts" USING btree ("category_id");
  CREATE INDEX "posts_cover_idx" ON "posts" USING btree ("cover_id");
  CREATE INDEX "posts_seo_seo_social_image_idx" ON "posts" USING btree ("seo_social_image_id");
  CREATE INDEX "posts_last_edited_by_idx" ON "posts" USING btree ("last_edited_by_id");
  CREATE INDEX "posts_updated_at_idx" ON "posts" USING btree ("updated_at");
  CREATE INDEX "posts_created_at_idx" ON "posts" USING btree ("created_at");
  CREATE INDEX "posts__status_idx" ON "posts" USING btree ("_status");
  CREATE INDEX "posts_rels_order_idx" ON "posts_rels" USING btree ("order");
  CREATE INDEX "posts_rels_parent_idx" ON "posts_rels" USING btree ("parent_id");
  CREATE INDEX "posts_rels_path_idx" ON "posts_rels" USING btree ("path");
  CREATE INDEX "posts_rels_media_id_idx" ON "posts_rels" USING btree ("media_id");
  CREATE INDEX "posts_rels_users_id_idx" ON "posts_rels" USING btree ("users_id");
  CREATE INDEX "posts_rels_posts_id_idx" ON "posts_rels" USING btree ("posts_id");
  CREATE INDEX "posts_rels_pages_id_idx" ON "posts_rels" USING btree ("pages_id");
  CREATE INDEX "_posts_v_version_links_order_idx" ON "_posts_v_version_links" USING btree ("_order");
  CREATE INDEX "_posts_v_version_links_parent_id_idx" ON "_posts_v_version_links" USING btree ("_parent_id");
  CREATE INDEX "_posts_v_blocks_callout_order_idx" ON "_posts_v_blocks_callout" USING btree ("_order");
  CREATE INDEX "_posts_v_blocks_callout_parent_id_idx" ON "_posts_v_blocks_callout" USING btree ("_parent_id");
  CREATE INDEX "_posts_v_blocks_callout_path_idx" ON "_posts_v_blocks_callout" USING btree ("_path");
  CREATE INDEX "_posts_v_blocks_quote_order_idx" ON "_posts_v_blocks_quote" USING btree ("_order");
  CREATE INDEX "_posts_v_blocks_quote_parent_id_idx" ON "_posts_v_blocks_quote" USING btree ("_parent_id");
  CREATE INDEX "_posts_v_blocks_quote_path_idx" ON "_posts_v_blocks_quote" USING btree ("_path");
  CREATE INDEX "_posts_v_blocks_related_posts_order_idx" ON "_posts_v_blocks_related_posts" USING btree ("_order");
  CREATE INDEX "_posts_v_blocks_related_posts_parent_id_idx" ON "_posts_v_blocks_related_posts" USING btree ("_parent_id");
  CREATE INDEX "_posts_v_blocks_related_posts_path_idx" ON "_posts_v_blocks_related_posts" USING btree ("_path");
  CREATE INDEX "_posts_v_parent_idx" ON "_posts_v" USING btree ("parent_id");
  CREATE INDEX "_posts_v_version_version_slug_idx" ON "_posts_v" USING btree ("version_slug");
  CREATE INDEX "_posts_v_version_version_author_idx" ON "_posts_v" USING btree ("version_author_id");
  CREATE INDEX "_posts_v_version_version_category_idx" ON "_posts_v" USING btree ("version_category_id");
  CREATE INDEX "_posts_v_version_version_cover_idx" ON "_posts_v" USING btree ("version_cover_id");
  CREATE INDEX "_posts_v_version_seo_version_seo_social_image_idx" ON "_posts_v" USING btree ("version_seo_social_image_id");
  CREATE INDEX "_posts_v_version_version_last_edited_by_idx" ON "_posts_v" USING btree ("version_last_edited_by_id");
  CREATE INDEX "_posts_v_version_version_updated_at_idx" ON "_posts_v" USING btree ("version_updated_at");
  CREATE INDEX "_posts_v_version_version_created_at_idx" ON "_posts_v" USING btree ("version_created_at");
  CREATE INDEX "_posts_v_version_version__status_idx" ON "_posts_v" USING btree ("version__status");
  CREATE INDEX "_posts_v_created_at_idx" ON "_posts_v" USING btree ("created_at");
  CREATE INDEX "_posts_v_updated_at_idx" ON "_posts_v" USING btree ("updated_at");
  CREATE INDEX "_posts_v_snapshot_idx" ON "_posts_v" USING btree ("snapshot");
  CREATE INDEX "_posts_v_published_locale_idx" ON "_posts_v" USING btree ("published_locale");
  CREATE INDEX "_posts_v_latest_idx" ON "_posts_v" USING btree ("latest");
  CREATE INDEX "_posts_v_autosave_idx" ON "_posts_v" USING btree ("autosave");
  CREATE INDEX "_posts_v_rels_order_idx" ON "_posts_v_rels" USING btree ("order");
  CREATE INDEX "_posts_v_rels_parent_idx" ON "_posts_v_rels" USING btree ("parent_id");
  CREATE INDEX "_posts_v_rels_path_idx" ON "_posts_v_rels" USING btree ("path");
  CREATE INDEX "_posts_v_rels_media_id_idx" ON "_posts_v_rels" USING btree ("media_id");
  CREATE INDEX "_posts_v_rels_users_id_idx" ON "_posts_v_rels" USING btree ("users_id");
  CREATE INDEX "_posts_v_rels_posts_id_idx" ON "_posts_v_rels" USING btree ("posts_id");
  CREATE INDEX "_posts_v_rels_pages_id_idx" ON "_posts_v_rels" USING btree ("pages_id");
  CREATE INDEX "pages_blocks_hero_order_idx" ON "pages_blocks_hero" USING btree ("_order");
  CREATE INDEX "pages_blocks_hero_parent_id_idx" ON "pages_blocks_hero" USING btree ("_parent_id");
  CREATE INDEX "pages_blocks_hero_path_idx" ON "pages_blocks_hero" USING btree ("_path");
  CREATE INDEX "pages_blocks_hero_image_idx" ON "pages_blocks_hero" USING btree ("image_id");
  CREATE INDEX "pages_blocks_rich_text_order_idx" ON "pages_blocks_rich_text" USING btree ("_order");
  CREATE INDEX "pages_blocks_rich_text_parent_id_idx" ON "pages_blocks_rich_text" USING btree ("_parent_id");
  CREATE INDEX "pages_blocks_rich_text_path_idx" ON "pages_blocks_rich_text" USING btree ("_path");
  CREATE INDEX "pages_blocks_featured_post_order_idx" ON "pages_blocks_featured_post" USING btree ("_order");
  CREATE INDEX "pages_blocks_featured_post_parent_id_idx" ON "pages_blocks_featured_post" USING btree ("_parent_id");
  CREATE INDEX "pages_blocks_featured_post_path_idx" ON "pages_blocks_featured_post" USING btree ("_path");
  CREATE INDEX "pages_blocks_featured_post_post_idx" ON "pages_blocks_featured_post" USING btree ("post_id");
  CREATE UNIQUE INDEX "pages_slug_idx" ON "pages" USING btree ("slug");
  CREATE INDEX "pages_updated_at_idx" ON "pages" USING btree ("updated_at");
  CREATE INDEX "pages_created_at_idx" ON "pages" USING btree ("created_at");
  CREATE INDEX "pages__status_idx" ON "pages" USING btree ("_status");
  CREATE INDEX "_pages_v_blocks_hero_order_idx" ON "_pages_v_blocks_hero" USING btree ("_order");
  CREATE INDEX "_pages_v_blocks_hero_parent_id_idx" ON "_pages_v_blocks_hero" USING btree ("_parent_id");
  CREATE INDEX "_pages_v_blocks_hero_path_idx" ON "_pages_v_blocks_hero" USING btree ("_path");
  CREATE INDEX "_pages_v_blocks_hero_image_idx" ON "_pages_v_blocks_hero" USING btree ("image_id");
  CREATE INDEX "_pages_v_blocks_rich_text_order_idx" ON "_pages_v_blocks_rich_text" USING btree ("_order");
  CREATE INDEX "_pages_v_blocks_rich_text_parent_id_idx" ON "_pages_v_blocks_rich_text" USING btree ("_parent_id");
  CREATE INDEX "_pages_v_blocks_rich_text_path_idx" ON "_pages_v_blocks_rich_text" USING btree ("_path");
  CREATE INDEX "_pages_v_blocks_featured_post_order_idx" ON "_pages_v_blocks_featured_post" USING btree ("_order");
  CREATE INDEX "_pages_v_blocks_featured_post_parent_id_idx" ON "_pages_v_blocks_featured_post" USING btree ("_parent_id");
  CREATE INDEX "_pages_v_blocks_featured_post_path_idx" ON "_pages_v_blocks_featured_post" USING btree ("_path");
  CREATE INDEX "_pages_v_blocks_featured_post_post_idx" ON "_pages_v_blocks_featured_post" USING btree ("post_id");
  CREATE INDEX "_pages_v_parent_idx" ON "_pages_v" USING btree ("parent_id");
  CREATE INDEX "_pages_v_version_version_slug_idx" ON "_pages_v" USING btree ("version_slug");
  CREATE INDEX "_pages_v_version_version_updated_at_idx" ON "_pages_v" USING btree ("version_updated_at");
  CREATE INDEX "_pages_v_version_version_created_at_idx" ON "_pages_v" USING btree ("version_created_at");
  CREATE INDEX "_pages_v_version_version__status_idx" ON "_pages_v" USING btree ("version__status");
  CREATE INDEX "_pages_v_created_at_idx" ON "_pages_v" USING btree ("created_at");
  CREATE INDEX "_pages_v_updated_at_idx" ON "_pages_v" USING btree ("updated_at");
  CREATE INDEX "_pages_v_snapshot_idx" ON "_pages_v" USING btree ("snapshot");
  CREATE INDEX "_pages_v_published_locale_idx" ON "_pages_v" USING btree ("published_locale");
  CREATE INDEX "_pages_v_latest_idx" ON "_pages_v" USING btree ("latest");
  CREATE INDEX "_pages_v_autosave_idx" ON "_pages_v" USING btree ("autosave");
  CREATE INDEX "events_schedule_order_idx" ON "events_schedule" USING btree ("_order");
  CREATE INDEX "events_schedule_parent_id_idx" ON "events_schedule" USING btree ("_parent_id");
  CREATE INDEX "events_schedule_speaker_idx" ON "events_schedule" USING btree ("speaker_id");
  CREATE INDEX "events_updated_at_idx" ON "events" USING btree ("updated_at");
  CREATE INDEX "events_created_at_idx" ON "events" USING btree ("created_at");
  CREATE INDEX "editorial_notes_owner_idx" ON "editorial_notes" USING btree ("owner_id");
  CREATE INDEX "editorial_notes_updated_at_idx" ON "editorial_notes" USING btree ("updated_at");
  CREATE INDEX "editorial_notes_created_at_idx" ON "editorial_notes" USING btree ("created_at");
  CREATE UNIQUE INDEX "redirects_from_idx" ON "redirects" USING btree ("from");
  CREATE INDEX "redirects_updated_at_idx" ON "redirects" USING btree ("updated_at");
  CREATE INDEX "redirects_created_at_idx" ON "redirects" USING btree ("created_at");
  CREATE INDEX "payload_capabilities_bounded_rows_order_idx" ON "payload_capabilities_bounded_rows" USING btree ("_order");
  CREATE INDEX "payload_capabilities_bounded_rows_parent_id_idx" ON "payload_capabilities_bounded_rows" USING btree ("_parent_id");
  CREATE INDEX "payload_capabilities_updated_at_idx" ON "payload_capabilities" USING btree ("updated_at");
  CREATE INDEX "payload_capabilities_created_at_idx" ON "payload_capabilities" USING btree ("created_at");
  CREATE INDEX "payload_capabilities_deleted_at_idx" ON "payload_capabilities" USING btree ("deleted_at");
  CREATE UNIQUE INDEX "payload_capabilities_locales_locale_parent_id_unique" ON "payload_capabilities_locales" USING btree ("_locale","_parent_id");
  CREATE UNIQUE INDEX "payload_kv_key_idx" ON "payload_kv" USING btree ("key");
  CREATE INDEX "payload_jobs_log_order_idx" ON "payload_jobs_log" USING btree ("_order");
  CREATE INDEX "payload_jobs_log_parent_id_idx" ON "payload_jobs_log" USING btree ("_parent_id");
  CREATE INDEX "payload_jobs_completed_at_idx" ON "payload_jobs" USING btree ("completed_at");
  CREATE INDEX "payload_jobs_total_tried_idx" ON "payload_jobs" USING btree ("total_tried");
  CREATE INDEX "payload_jobs_has_error_idx" ON "payload_jobs" USING btree ("has_error");
  CREATE INDEX "payload_jobs_task_slug_idx" ON "payload_jobs" USING btree ("task_slug");
  CREATE INDEX "payload_jobs_queue_idx" ON "payload_jobs" USING btree ("queue");
  CREATE INDEX "payload_jobs_wait_until_idx" ON "payload_jobs" USING btree ("wait_until");
  CREATE INDEX "payload_jobs_processing_idx" ON "payload_jobs" USING btree ("processing");
  CREATE INDEX "payload_jobs_updated_at_idx" ON "payload_jobs" USING btree ("updated_at");
  CREATE INDEX "payload_jobs_created_at_idx" ON "payload_jobs" USING btree ("created_at");
  CREATE INDEX "payload_locked_documents_global_slug_idx" ON "payload_locked_documents" USING btree ("global_slug");
  CREATE INDEX "payload_locked_documents_updated_at_idx" ON "payload_locked_documents" USING btree ("updated_at");
  CREATE INDEX "payload_locked_documents_created_at_idx" ON "payload_locked_documents" USING btree ("created_at");
  CREATE INDEX "payload_locked_documents_rels_order_idx" ON "payload_locked_documents_rels" USING btree ("order");
  CREATE INDEX "payload_locked_documents_rels_parent_idx" ON "payload_locked_documents_rels" USING btree ("parent_id");
  CREATE INDEX "payload_locked_documents_rels_path_idx" ON "payload_locked_documents_rels" USING btree ("path");
  CREATE INDEX "payload_locked_documents_rels_users_id_idx" ON "payload_locked_documents_rels" USING btree ("users_id");
  CREATE INDEX "payload_locked_documents_rels_media_id_idx" ON "payload_locked_documents_rels" USING btree ("media_id");
  CREATE INDEX "payload_locked_documents_rels_categories_id_idx" ON "payload_locked_documents_rels" USING btree ("categories_id");
  CREATE INDEX "payload_locked_documents_rels_posts_id_idx" ON "payload_locked_documents_rels" USING btree ("posts_id");
  CREATE INDEX "payload_locked_documents_rels_pages_id_idx" ON "payload_locked_documents_rels" USING btree ("pages_id");
  CREATE INDEX "payload_locked_documents_rels_events_id_idx" ON "payload_locked_documents_rels" USING btree ("events_id");
  CREATE INDEX "payload_locked_documents_rels_editorial_notes_id_idx" ON "payload_locked_documents_rels" USING btree ("editorial_notes_id");
  CREATE INDEX "payload_locked_documents_rels_redirects_id_idx" ON "payload_locked_documents_rels" USING btree ("redirects_id");
  CREATE INDEX "payload_locked_documents_rels_payload_capabilities_id_idx" ON "payload_locked_documents_rels" USING btree ("payload_capabilities_id");
  CREATE INDEX "payload_preferences_key_idx" ON "payload_preferences" USING btree ("key");
  CREATE INDEX "payload_preferences_updated_at_idx" ON "payload_preferences" USING btree ("updated_at");
  CREATE INDEX "payload_preferences_created_at_idx" ON "payload_preferences" USING btree ("created_at");
  CREATE INDEX "payload_preferences_rels_order_idx" ON "payload_preferences_rels" USING btree ("order");
  CREATE INDEX "payload_preferences_rels_parent_idx" ON "payload_preferences_rels" USING btree ("parent_id");
  CREATE INDEX "payload_preferences_rels_path_idx" ON "payload_preferences_rels" USING btree ("path");
  CREATE INDEX "payload_preferences_rels_users_id_idx" ON "payload_preferences_rels" USING btree ("users_id");
  CREATE INDEX "payload_migrations_updated_at_idx" ON "payload_migrations" USING btree ("updated_at");
  CREATE INDEX "payload_migrations_created_at_idx" ON "payload_migrations" USING btree ("created_at");
  CREATE INDEX "site_settings_navigation_order_idx" ON "site_settings_navigation" USING btree ("_order");
  CREATE INDEX "site_settings_navigation_parent_id_idx" ON "site_settings_navigation" USING btree ("_parent_id");
  CREATE INDEX "site_settings_navigation_page_idx" ON "site_settings_navigation" USING btree ("page_id");
  CREATE INDEX "site_settings__status_idx" ON "site_settings" USING btree ("_status");
  CREATE UNIQUE INDEX "site_settings_locales_locale_parent_id_unique" ON "site_settings_locales" USING btree ("_locale","_parent_id");
  CREATE INDEX "_site_settings_v_version_navigation_order_idx" ON "_site_settings_v_version_navigation" USING btree ("_order");
  CREATE INDEX "_site_settings_v_version_navigation_parent_id_idx" ON "_site_settings_v_version_navigation" USING btree ("_parent_id");
  CREATE INDEX "_site_settings_v_version_navigation_page_idx" ON "_site_settings_v_version_navigation" USING btree ("page_id");
  CREATE INDEX "_site_settings_v_version_version__status_idx" ON "_site_settings_v" USING btree ("version__status");
  CREATE INDEX "_site_settings_v_created_at_idx" ON "_site_settings_v" USING btree ("created_at");
  CREATE INDEX "_site_settings_v_updated_at_idx" ON "_site_settings_v" USING btree ("updated_at");
  CREATE INDEX "_site_settings_v_snapshot_idx" ON "_site_settings_v" USING btree ("snapshot");
  CREATE INDEX "_site_settings_v_published_locale_idx" ON "_site_settings_v" USING btree ("published_locale");
  CREATE INDEX "_site_settings_v_latest_idx" ON "_site_settings_v" USING btree ("latest");
  CREATE UNIQUE INDEX "_site_settings_v_locales_locale_parent_id_unique" ON "_site_settings_v_locales" USING btree ("_locale","_parent_id");`);
}

export async function down({ db, payload, req }: MigrateDownArgs): Promise<void> {
	await db.execute(sql`
   DROP TABLE "users_sessions" CASCADE;
  DROP TABLE "users" CASCADE;
  DROP TABLE "media_tags" CASCADE;
  DROP TABLE "media" CASCADE;
  DROP TABLE "categories" CASCADE;
  DROP TABLE "posts_links" CASCADE;
  DROP TABLE "posts_blocks_callout" CASCADE;
  DROP TABLE "posts_blocks_quote" CASCADE;
  DROP TABLE "posts_blocks_related_posts" CASCADE;
  DROP TABLE "posts" CASCADE;
  DROP TABLE "posts_rels" CASCADE;
  DROP TABLE "_posts_v_version_links" CASCADE;
  DROP TABLE "_posts_v_blocks_callout" CASCADE;
  DROP TABLE "_posts_v_blocks_quote" CASCADE;
  DROP TABLE "_posts_v_blocks_related_posts" CASCADE;
  DROP TABLE "_posts_v" CASCADE;
  DROP TABLE "_posts_v_rels" CASCADE;
  DROP TABLE "pages_blocks_hero" CASCADE;
  DROP TABLE "pages_blocks_rich_text" CASCADE;
  DROP TABLE "pages_blocks_featured_post" CASCADE;
  DROP TABLE "pages" CASCADE;
  DROP TABLE "_pages_v_blocks_hero" CASCADE;
  DROP TABLE "_pages_v_blocks_rich_text" CASCADE;
  DROP TABLE "_pages_v_blocks_featured_post" CASCADE;
  DROP TABLE "_pages_v" CASCADE;
  DROP TABLE "events_schedule" CASCADE;
  DROP TABLE "events" CASCADE;
  DROP TABLE "editorial_notes" CASCADE;
  DROP TABLE "redirects" CASCADE;
  DROP TABLE "payload_capabilities_bounded_rows" CASCADE;
  DROP TABLE "payload_capabilities" CASCADE;
  DROP TABLE "payload_capabilities_locales" CASCADE;
  DROP TABLE "payload_kv" CASCADE;
  DROP TABLE "payload_jobs_log" CASCADE;
  DROP TABLE "payload_jobs" CASCADE;
  DROP TABLE "payload_locked_documents" CASCADE;
  DROP TABLE "payload_locked_documents_rels" CASCADE;
  DROP TABLE "payload_preferences" CASCADE;
  DROP TABLE "payload_preferences_rels" CASCADE;
  DROP TABLE "payload_migrations" CASCADE;
  DROP TABLE "site_settings_navigation" CASCADE;
  DROP TABLE "site_settings" CASCADE;
  DROP TABLE "site_settings_locales" CASCADE;
  DROP TABLE "_site_settings_v_version_navigation" CASCADE;
  DROP TABLE "_site_settings_v" CASCADE;
  DROP TABLE "_site_settings_v_locales" CASCADE;
  DROP TYPE "public"."_locales";
  DROP TYPE "public"."enum_users_role";
  DROP TYPE "public"."enum_media_kind";
  DROP TYPE "public"."enum_posts_blocks_callout_tone";
  DROP TYPE "public"."enum_posts_status";
  DROP TYPE "public"."enum__posts_v_blocks_callout_tone";
  DROP TYPE "public"."enum__posts_v_version_status";
  DROP TYPE "public"."enum__posts_v_published_locale";
  DROP TYPE "public"."enum_pages_template";
  DROP TYPE "public"."enum_pages_status";
  DROP TYPE "public"."enum__pages_v_version_template";
  DROP TYPE "public"."enum__pages_v_version_status";
  DROP TYPE "public"."enum__pages_v_published_locale";
  DROP TYPE "public"."enum_redirects_type";
  DROP TYPE "public"."enum_payload_capabilities_presentation";
  DROP TYPE "public"."enum_payload_jobs_log_task_slug";
  DROP TYPE "public"."enum_payload_jobs_log_state";
  DROP TYPE "public"."enum_payload_jobs_task_slug";
  DROP TYPE "public"."enum_site_settings_status";
  DROP TYPE "public"."enum__site_settings_v_version_status";
  DROP TYPE "public"."enum__site_settings_v_published_locale";`);
}
