import { defineAdminMessages } from "@riducms/plugin";
import { outlineAdminPlugin } from "./outline-plugin";
import { richTextAdminPlugin } from "@riducms/plugin-richtext";
import { seoAdminPlugin } from "@riducms/plugin-seo";
import { formBuilderAdminPlugin } from "@riducms/plugin-form-builder/admin";
import { ar, en, fr } from "@riducms/translations";

import PluginRoute from "./plugin-route.svelte";
import DashboardPanel from "./dashboard-panel.svelte";
import PostInsightsView from "./post-insights-view.svelte";
import PostReadingTimeCell from "./post-reading-time-cell.svelte";
import PostReviewAction from "./post-review-action.svelte";
import LoginFrame from "./login-frame.svelte";
import AccountFrame from "./account-frame.svelte";
import NavigationNote from "./navigation-note.svelte";
import LogoutButton from "./logout-button.svelte";
import CoreViewFrame from "./core-view-frame.svelte";
import BrandComponent from "./brand-component.svelte";
import ShellComponent from "./shell-component.svelte";
import ProviderFrame from "./provider-frame.svelte";
import CompositeRowLabel from "./composite-row-label.svelte";
import UnifiedRowLabel from "./unified-row-label.svelte";

import { defineAdmin, defineRowLabel } from "@riducms/plugin/admin";
import { defineFieldEditor } from "@riducms/plugin/editor";
import LocalTextEditor from "./local-text-editor.svelte";
import PrimitiveTextEditor from "./primitive-text-editor.svelte";
import BasicTextEditor from "./basic-text-editor.svelte";
const contractMessages = defineAdminMessages({
	fallback: {
		"dashboard.eyebrow": "Extension panel",
		"dashboard.title": "Editorial pulse",
		"dashboard.summary": {
			one: "{count} collection is ready for {name}.",
			other: "{count} collections are ready for {name}.",
		},
		"dashboard.team": "the team",
		"navigation.route": "Plugin contract",
		"list.readingTime": "Reading time",
		"documents.insights": "Insights",
	},
	translations: {
		fr: {
			"dashboard.eyebrow": "Panneau d’extension",
			"dashboard.title": "Activité éditoriale",
			"dashboard.summary": {
				one: "{count} collection est prête pour {name}.",
				other: "{count} collections sont prêtes pour {name}.",
			},
			"dashboard.team": "l’équipe",
			"navigation.route": "Contrat de l’extension",
			"list.readingTime": "Temps de lecture",
			"documents.insights": "Analyses",
		},
		ar: {
			"dashboard.eyebrow": "لوحة الإضافة",
			"dashboard.title": "النشاط التحريري",
			"dashboard.summary": {
				zero: "لا توجد مجموعات جاهزة لـ {name} ({count}).",
				one: "هناك مجموعة واحدة جاهزة لـ {name} ({count}).",
				two: "هناك مجموعتان جاهزتان لـ {name} ({count}).",
				few: "هناك {count} مجموعات جاهزة لـ {name}.",
				many: "هناك {count} مجموعة جاهزة لـ {name}.",
				other: "هناك {count} مجموعة جاهزة لـ {name}.",
			},
			"dashboard.team": "الفريق",
			"navigation.route": "عقد الإضافة",
			"list.readingTime": "وقت القراءة",
			"documents.insights": "التحليلات",
		},
	},
});

export default defineAdmin({
	fields: {
		"app:primitiveText": defineFieldEditor({ type: "text-list", component: PrimitiveTextEditor }),
		"app:text": defineFieldEditor({ type: "text", component: BasicTextEditor }),
		"app:capturedText": defineFieldEditor({
			type: "text",
			component: LocalTextEditor,
			decodeConfig(value: unknown) {
				if (typeof value !== "object" || value === null || Array.isArray(value))
					throw new Error("Expected an editor options object");
				if ("capture" in value && typeof value.capture !== "boolean")
					throw new Error("capture must be boolean");
				return { capture: "capture" in value && value.capture === true };
			},
		}),
	},
	rowLabels: {
		"app:unifiedRowLabel": defineRowLabel({
			component: UnifiedRowLabel,
			decodeConfig(value: unknown) {
				if (
					typeof value !== "object" ||
					value === null ||
					!("prefix" in value) ||
					typeof value.prefix !== "string"
				)
					throw new Error("Expected a row label prefix");
				return { prefix: value.prefix };
			},
		}),
		"app:compositeRowLabel": defineRowLabel({
			component: CompositeRowLabel,
			decodeConfig(value: unknown) {
				if (
					typeof value !== "object" ||
					value === null ||
					!("kind" in value) ||
					!["typedOrder", "keyLabel"].includes(String(value.kind))
				)
					throw new Error("Expected a composite row label kind");
				return value;
			},
		}),
	},
	messages: contractMessages,
	routes: [
		{
			path: "plugin-contract",
			component: PluginRoute,
			navigation: {
				label: "Plugin contract",
				labelKey: "app:navigation.route",
			},
		},
	],
	dashboard: [{ key: "editorial-pulse", component: DashboardPanel, position: "before" }],
	login: [{ key: "contract-login", component: LoginFrame, position: "replace" }],
	account: [
		{ key: "contract-profile", surface: "profile", component: AccountFrame, position: "replace" },
	],
	navigation: [{ key: "contract-shell", component: NavigationNote, position: "after" }],
	logoutButton: { key: "contract-logout", component: LogoutButton },
	views: [
		{
			key: "posts-list",
			surface: "collectionList",
			collection: "posts",
			component: CoreViewFrame,
		},
		{
			key: "posts-create",
			surface: "collectionCreate",
			collection: "posts",
			component: CoreViewFrame,
		},
		{
			key: "posts-edit",
			surface: "collectionEdit",
			collection: "posts",
			component: CoreViewFrame,
		},
		{
			key: "site-settings",
			surface: "global",
			global: "site-settings",
			component: CoreViewFrame,
		},
		{ key: "not-found", surface: "notFound", component: CoreViewFrame },
	],
	branding: [
		{ key: "login-brand", surface: "loginLogo", component: BrandComponent },
		{ key: "navigation-brand", surface: "navigationLogo", component: BrandComponent },
		{ key: "account-avatar", surface: "accountAvatar", component: BrandComponent },
	],
	shell: [
		{ key: "contract-header", position: "header", component: ShellComponent },
		{ key: "contract-action", position: "actions", component: ShellComponent },
		{ key: "contract-settings", position: "settingsMenu", component: ShellComponent },
	],
	providers: [{ key: "contract-provider", component: ProviderFrame }],
	listCells: [
		{
			key: "reading-time",
			collection: "posts",
			field: "readingMinutes",
			label: "Reading time",
			labelKey: "app:list.readingTime",
			component: PostReadingTimeCell,
		},
	],
	documentActions: [
		{ key: "request-review", collection: "posts", requires: "update", component: PostReviewAction },
	],
	documentViews: [
		{
			key: "insights",
			label: "Insights",
			labelKey: "app:documents.insights",
			collection: "posts",
			component: PostInsightsView,
		},
	],
	plugins: [richTextAdminPlugin, seoAdminPlugin, formBuilderAdminPlugin, outlineAdminPlugin],
	languages: [en, fr, ar],
});
