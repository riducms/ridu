import { mountAdmin } from "@riducms/admin";
import {
	ADMIN_PLUGIN_API_VERSION,
	defineAdminMessages,
	defineAdminPlugin,
	defineRowLabelPlugin,
} from "@riducms/plugin";
import { richTextAdminPlugin } from "@riducms/plugin-richtext";
import { seoAdminPlugin } from "@riducms/plugin-seo";
import { formBuilderAdminPlugin } from "@riducms/plugin-form-builder/admin";
import { createClient } from "@riducms/sdk";
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

const contractPlugin = defineAdminPlugin({
	apiVersion: ADMIN_PLUGIN_API_VERSION,
	key: "contract-route",
	pairingVersion: 1,
	fields: [],
	rowLabels: [
		defineRowLabelPlugin({
			key: "contract-route",
			componentKey: "compositeRowLabel",
			component: CompositeRowLabel,
		}),
	],
	messages: contractMessages,
	routes: [
		{
			path: "plugin-contract",
			component: PluginRoute,
			navigation: {
				label: "Plugin contract",
				labelKey: "plugin.contract-route:navigation.route",
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
			labelKey: "plugin.contract-route:list.readingTime",
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
			labelKey: "plugin.contract-route:documents.insights",
			collection: "posts",
			component: PostInsightsView,
		},
	],
});

const target = document.getElementById("app");
if (target === null) throw new Error('Ridu admin contract requires an element with id "app".');

mountAdmin({
	target,
	clientFactory: () => createClient({ baseURL: window.location.origin }),
	plugins: [contractPlugin, richTextAdminPlugin, seoAdminPlugin, formBuilderAdminPlugin],
	languages: [en, fr, ar],
});
