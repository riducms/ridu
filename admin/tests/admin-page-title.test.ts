import { describe, expect, test } from "bun:test";
import type { Component } from "svelte";

import type { AdminDocumentView, AdminRoute } from "@riducms/plugin";
import {
	SCHEMA_MANIFEST_VERSION,
	type SchemaCollection,
	type SchemaManifest,
} from "@riducms/protocol";
import type { AdminI18n } from "@riducms/translations";

import { adminApplicationName } from "@admin/app-meta";
import { resolveAdminPageLabel } from "@admin/app/admin-page-title";

const component = (() => undefined) as unknown as Component;

const messages = {
	"account:account": "Account",
	"account:accountSecurity": "Account security",
	"auth:createFirstUser": "Create first user",
	"auth:forgotPassword": "Forgot password?",
	"auth:login": "Sign in",
	"auth:requestVerification": "Request verification email",
	"auth:resetPassword": "Reset password",
	"auth:verifyEmail": "Verify email",
	"collections:createNew": "New {label}",
	"collections:trashFor": "Trash - {label}",
	"dashboard:heading": "Dashboard",
	"documents:apiFor": "API - {label}",
	"documents:editing": "Editing - {label}",
	"documents:versionsFor": "Versions - {label}",
	"errors:notFoundCode": "Error 404",
	"navigation:page": "Page",
	"uploads:bulkUpload": "Bulk upload",
	"app:insights": "Translated insights",
	"app:salesReports": "Sales reports",
} as const;

const i18n = {
	language: "en",
	t(key, variables) {
		const message = messages[key as keyof typeof messages] ?? key;
		return message.replaceAll(/\{(\w+)\}/g, (_, name: string) => String(variables?.[name] ?? ""));
	},
	text(canonical, translations) {
		return translations?.[this.language] ?? canonical;
	},
} satisfies Pick<AdminI18n, "language" | "t" | "text">;

const manifest: SchemaManifest = {
	version: SCHEMA_MANIFEST_VERSION,
	application: {
		name: "Studio",
		nameTranslations: { fr: "Atelier" },
	},
	collections: [
		resource("articles", "Article", "Articles"),
		{ ...resource("learner-events", "Learner event", "Learner events"), admin: { hidden: true } },
	],
	globals: [
		resource("site-settings", "Site settings", "Site settings"),
		{ ...resource("sync-state", "Sync state", "Sync state"), admin: { hidden: true } },
	],
	plugins: [],
};

const routes: AdminRoute[] = [
	{
		path: "reports/sales/weekly",
		component,
		navigation: { label: "Sales", labelKey: "app:salesReports" },
	},
];

const documentViews: AdminDocumentView[] = [
	{
		key: "insights",
		label: "Insights",
		labelKey: "app:insights",
		component,
	},
];

describe("admin page title labels", () => {
	for (const [pathname, expected] of [
		["/", "Dashboard"],
		["/create-first-user", "Create first user"],
		["/login", "Sign in"],
		["/forgot-password", "Forgot password?"],
		["/reset-password", "Reset password"],
		["/request-verification", "Request verification email"],
		["/verify-email", "Verify email"],
		["/account", "Account"],
		["/account/security", "Account security"],
		["/collections/articles", "Articles"],
		["/collections/articles/create", "New Article"],
		["/collections/articles/create/api", "API - Article"],
		["/collections/articles/trash", "Trash - Articles"],
		["/collections/articles/upload", "Bulk upload"],
		["/collections/articles/article-1", "Editing - Article"],
		["/collections/articles/article-1/api", "API - Article"],
		["/collections/articles/article-1/versions", "Versions - Article"],
		["/collections/articles/article-1/insights", "Translated insights"],
		["/globals/site-settings", "Site settings"],
		["/globals/site-settings/api", "API - Site settings"],
		["/globals/site-settings/versions", "Versions - Site settings"],
		["/globals/site-settings/insights", "Translated insights"],
		["/reports/sales/weekly", "Sales reports"],
		["/unknown/sales_report", "Sales report"],
	] as const) {
		test(pathname, () => {
			expect(resolve(pathname)).toBe(expected);
		});
	}

	test("does not throw for malformed encoded fallback paths", () => {
		expect(resolve("/unknown/%E0%A4%A")).toBe("%E0%A4%A");
	});

	// A hidden resource renders the not-found page; its title says so rather
	// than naming the resource, whatever the path's case or depth.
	test("does not name a hidden collection or global", () => {
		for (const pathname of [
			"/collections/learner-events",
			"/Collections/learner-events",
			"/collections/learner-events/create",
			"/collections/learner-events/event-1/versions",
			"/collections/learner-events/event-1/insights",
			"/globals/sync-state",
			"/globals/sync-state/api",
		]) {
			expect(resolve(pathname)).toBe("Error 404");
		}
	});

	test("uses an extension view only for its configured resource", () => {
		const scopedViews: AdminDocumentView[] = [
			{ key: "insights", label: "Article insights", collection: "articles", component },
		];
		expect(resolve("/collections/articles/article-1/insights", scopedViews)).toBe(
			"Article insights"
		);
		expect(resolve("/globals/site-settings/insights", scopedViews)).toBe("Site settings");
	});
});

describe("admin application title", () => {
	test("uses the localized manifest name and falls back before the manifest loads", () => {
		const french = { ...i18n, language: "fr" };
		expect(adminApplicationName(manifest, french, "Ridu")).toBe("Atelier");
		expect(adminApplicationName(undefined, french, "Ridu")).toBe("Ridu");
	});
});

function resolve(pathname: string, views = documentViews) {
	return resolveAdminPageLabel({ pathname, manifest, routes, documentViews: views, i18n });
}

function resource(slug: string, singular: string, plural: string): SchemaCollection {
	return {
		id: slug,
		slug,
		labels: { singular, plural },
		admin: {},
		capabilities: {
			auth: false,
			upload: false,
			versions: true,
			trash: true,
			locking: false,
		},
		fields: [],
	};
}
