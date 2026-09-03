<script lang="ts">
	import { useLocation } from "@hvniel/svelte-router";

	import { ADMIN_NAME } from "@admin/app-meta";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

	const runtime = getAdminRuntime();
	const location = $derived(useLocation());
	const pageTitle = $derived.by(resolvePageTitle);

	function resolvePageTitle() {
		const segments = location.pathname.split("/").filter(Boolean);
		if (segments.length === 0) return titled(runtime.i18n.t("dashboard:heading"));
		if (segments[0] === "create-first-user") return titled(runtime.i18n.t("auth:createFirstUser"));
		if (segments[0] === "login") return titled(runtime.i18n.t("auth:login"));
		if (segments[0] === "forgot-password") return titled(runtime.i18n.t("auth:forgotPassword"));
		if (segments[0] === "reset-password") return titled(runtime.i18n.t("auth:resetPassword"));
		if (segments[0] === "request-verification")
			return titled(runtime.i18n.t("auth:requestVerification"));
		if (segments[0] === "verify-email") return titled(runtime.i18n.t("auth:verifyEmail"));
		if (segments[0] === "account") {
			return titled(
				segments[1] === "security"
					? runtime.i18n.t("account:accountSecurity")
					: runtime.i18n.t("account:account")
			);
		}
		if (segments[0] === "collections" && segments[1] !== undefined) {
			const collection = runtime.manifest?.collections.find(
				(candidate) => candidate.slug === segments[1]
			);
			const singular =
				collection === undefined
					? humanize(segments[1])
					: runtime.i18n.text(collection.labels.singular, collection.labels.singularTranslations);
			const plural =
				collection === undefined
					? singular
					: runtime.i18n.text(collection.labels.plural, collection.labels.pluralTranslations);
			if (segments[2] === undefined) return titled(plural);
			if (segments[2] === "create") {
				return titled(
					segments[3] === "api"
						? runtime.i18n.t("documents:apiFor", { label: singular })
						: runtime.i18n.t("collections:createNew", { label: singular })
				);
			}
			if (segments[2] === "trash")
				return titled(
					runtime.i18n.t("collections:trashFor", {
						label: plural,
					})
				);
			if (segments[3] === "api")
				return titled(runtime.i18n.t("documents:apiFor", { label: singular }));
			if (segments[3] === "versions")
				return titled(runtime.i18n.t("documents:versionsFor", { label: singular }));
			return titled(runtime.i18n.t("documents:editing", { label: singular }));
		}
		if (segments[0] === "globals" && segments[1] !== undefined) {
			const global = runtime.manifest?.globals?.find((candidate) => candidate.slug === segments[1]);
			const label =
				global === undefined
					? humanize(segments[1])
					: runtime.i18n.text(global.labels.singular, global.labels.singularTranslations);
			if (segments[2] === "api") return titled(runtime.i18n.t("documents:apiFor", { label }));
			if (segments[2] === "versions")
				return titled(runtime.i18n.t("documents:versionsFor", { label }));
			return titled(label);
		}
		const pluginPath = segments.join("/");
		const pluginRoute = runtime.pluginRoutes.find((route) => route.path === pluginPath);
		if (pluginRoute?.navigation?.labelKey !== undefined)
			return titled(runtime.i18n.t(pluginRoute.navigation.labelKey));
		if (pluginRoute?.navigation?.label !== undefined) return titled(pluginRoute.navigation.label);
		return titled(humanize(segments.at(-1) ?? runtime.i18n.t("navigation:page")));
	}

	function titled(value: string) {
		const application = runtime.manifest?.application;
		const name =
			application === undefined
				? ADMIN_NAME
				: runtime.i18n.text(application.name, application.nameTranslations);
		return `${value} - ${name}`;
	}

	function humanize(value: string) {
		const decoded = decodeURIComponent(value).replaceAll("-", " ");
		return decoded.charAt(0).toLocaleUpperCase(runtime.i18n.language) + decoded.slice(1);
	}
</script>

<svelte:head><title>{pageTitle}</title></svelte:head>
