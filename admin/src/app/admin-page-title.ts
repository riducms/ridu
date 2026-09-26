import type { AdminDocumentView, AdminRoute } from "@riducms/plugin";
import type { SchemaManifest } from "@riducms/protocol";
import type { AdminI18n, ExtensionTranslationKey } from "@riducms/translations";

import { adminPathSegments, humanizeAdminPathSegment } from "@admin/core/routing/admin-paths";

type PageTitleI18n = Pick<AdminI18n, "language" | "t" | "text">;

interface AdminPageLabelOptions {
	pathname: string;
	manifest: SchemaManifest | undefined;
	routes: readonly AdminRoute[];
	documentViews: readonly AdminDocumentView[];
	i18n: PageTitleI18n;
}

export function resolveAdminPageLabel(options: AdminPageLabelOptions): string {
	const segments = adminPathSegments(options.pathname);
	const [root, resource, action, view] = segments;

	switch (root) {
		case undefined:
			return options.i18n.t("dashboard:heading");
		case "create-first-user":
			return options.i18n.t("auth:createFirstUser");
		case "login":
			return options.i18n.t("auth:login");
		case "forgot-password":
			return options.i18n.t("auth:forgotPassword");
		case "reset-password":
			return options.i18n.t("auth:resetPassword");
		case "request-verification":
			return options.i18n.t("auth:requestVerification");
		case "verify-email":
			return options.i18n.t("auth:verifyEmail");
		case "account":
			return options.i18n.t(
				resource === "security" ? "account:accountSecurity" : "account:account"
			);
		case "collections":
			return resolveCollectionLabel(options, resource, action, view);
		case "globals":
			return resolveGlobalLabel(options, resource, action);
		default:
			return resolvePluginOrFallbackLabel(options, segments);
	}
}

function resolveCollectionLabel(
	options: AdminPageLabelOptions,
	resource: string | undefined,
	action: string | undefined,
	view: string | undefined
) {
	if (resource === undefined) return humanize("collections", options.i18n);

	const collection = options.manifest?.collections.find((candidate) => candidate.slug === resource);
	const singular =
		collection === undefined
			? humanize(resource, options.i18n)
			: options.i18n.text(collection.labels.singular, collection.labels.singularTranslations);
	const plural =
		collection === undefined
			? singular
			: options.i18n.text(collection.labels.plural, collection.labels.pluralTranslations);

	if (action === undefined) return plural;
	if (action === "create") {
		return view === "api"
			? options.i18n.t("documents:apiFor", { label: singular })
			: options.i18n.t("collections:createNew", { label: singular });
	}
	if (action === "trash") return options.i18n.t("collections:trashFor", { label: plural });
	if (action === "upload") return options.i18n.t("uploads:bulkUpload");
	if (view === "api") return options.i18n.t("documents:apiFor", { label: singular });
	if (view === "versions") return options.i18n.t("documents:versionsFor", { label: singular });

	return (
		resolveDocumentViewLabel(options, resource, view) ??
		options.i18n.t("documents:editing", { label: singular })
	);
}

function resolveGlobalLabel(
	options: AdminPageLabelOptions,
	resource: string | undefined,
	view: string | undefined
) {
	if (resource === undefined) return humanize("globals", options.i18n);

	const global = options.manifest?.globals?.find((candidate) => candidate.slug === resource);
	const label =
		global === undefined
			? humanize(resource, options.i18n)
			: options.i18n.text(global.labels.singular, global.labels.singularTranslations);

	if (view === "api") return options.i18n.t("documents:apiFor", { label });
	if (view === "versions") return options.i18n.t("documents:versionsFor", { label });
	return resolveDocumentViewLabel(options, resource, view) ?? label;
}

function resolveDocumentViewLabel(
	options: AdminPageLabelOptions,
	resource: string,
	view: string | undefined
) {
	if (view === undefined) return undefined;
	return resolveExtensionLabel(
		options.documentViews.find(
			(candidate) =>
				candidate.key === view &&
				(candidate.collection === undefined || candidate.collection === resource)
		),
		options.i18n
	);
}

function resolvePluginOrFallbackLabel(options: AdminPageLabelOptions, segments: string[]) {
	const route = options.routes.find((candidate) => candidate.path === segments.join("/"));
	return (
		resolveExtensionLabel(route?.navigation, options.i18n) ??
		humanize(segments.at(-1) ?? options.i18n.t("navigation:page"), options.i18n)
	);
}

function resolveExtensionLabel(
	extension: { label: string; labelKey?: ExtensionTranslationKey } | undefined,
	i18n: PageTitleI18n
) {
	if (extension?.labelKey !== undefined) return i18n.t(extension.labelKey);
	return extension?.label;
}

function humanize(value: string, i18n: PageTitleI18n) {
	return humanizeAdminPathSegment(value, i18n.language);
}
