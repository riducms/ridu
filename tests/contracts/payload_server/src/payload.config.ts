import { lexicalEditor } from "@payloadcms/richtext-lexical";
import { formBuilderPlugin } from "@payloadcms/plugin-form-builder";
import { seoPlugin } from "@payloadcms/plugin-seo";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { buildConfig } from "payload";
import sharp from "sharp";

import { collections } from "./collections";
import { seed } from "./seed";
import { slugs } from "./shared";

const filename = fileURLToPath(import.meta.url);
const dirname = path.dirname(filename);
const databaseURL = process.env.PAYLOAD_DATABASE_URL;
const requestedPoolMax = Number(process.env.PAYLOAD_POOL_MAX);
const poolMax =
	Number.isSafeInteger(requestedPoolMax) && requestedPoolMax > 0 ? requestedPoolMax : undefined;
const adminComponent = (exportName: string) => `/admin-components#${exportName}`;
const database = databaseURL?.startsWith("postgres")
	? (await import("@payloadcms/db-postgres")).postgresAdapter({
			pool: { connectionString: databaseURL, ...(poolMax === undefined ? {} : { max: poolMax }) },
			push: true,
		})
	: (await import("@payloadcms/db-sqlite")).sqliteAdapter({
			autoIncrement: true,
			client: { url: databaseURL || "file:./payload-parity.db" },
		});

export default buildConfig({
	admin: {
		autoLogin: {
			email: "admin@riducms.test",
			password: "ridu-admin",
			prefillOnly: true,
		},
		avatar: { Component: adminComponent("AccountAvatar") },
		components: {
			actions: [adminComponent("ShellAction")],
			afterNavLinks: [adminComponent("NavigationNote"), adminComponent("PluginRouteLink")],
			beforeDashboard: [adminComponent("DashboardPanel")],
			beforeLogin: [adminComponent("LoginFrame")],
			graphics: {
				Icon: adminComponent("BrandIcon"),
				Logo: adminComponent("BrandLogo"),
			},
			header: [adminComponent("ShellHeader")],
			logout: { Button: adminComponent("PluginLogoutButton") },
			providers: [adminComponent("ProviderBoundary")],
			settingsMenu: [adminComponent("SettingsMenuItem")],
			views: {
				account: { Component: adminComponent("AccountView") },
				"plugin-contract": {
					Component: "/admin-view#PluginRoute",
					exact: true,
					path: "/plugin-contract",
				},
			},
		},
		importMap: { baseDir: path.resolve(dirname) },
		livePreview: {
			breakpoints: [
				{ name: "mobile", label: "Mobile", width: 375, height: 667 },
				{ name: "tablet", label: "Tablet", width: 768, height: 1024 },
				{ name: "desktop", label: "Desktop", width: 1440, height: 900 },
			],
		},
		user: slugs.users,
	},
	collections,
	db: database,
	editor: lexicalEditor({}),
	globals: [
		{
			slug: "site-settings",
			label: "Site settings (reference)",
			admin: {
				components: {
					elements: {
						beforeDocumentControls: [adminComponent("GlobalDocumentFrame")],
					},
				},
				group: "Comparison reference",
			},
			fields: [
				{ name: "siteName", type: "text", required: true },
				{ name: "announcement", type: "textarea", localized: true },
				{
					name: "navigation",
					type: "array",
					fields: [
						{ name: "label", type: "text", required: true },
						{ name: "page", type: "relationship", relationTo: slugs.pages, required: true },
					],
				},
			],
			versions: { drafts: true },
		},
	],
	localization: {
		defaultLocale: "en",
		fallback: true,
		locales: [
			{ code: "en", label: "English" },
			{ code: "es", label: "Español" },
		],
	},
	onInit: seed,
	plugins: [
		formBuilderPlugin({
			fields: {
				date: true,
				payment: false,
				radio: true,
				upload: true,
			},
			redirectRelationships: [slugs.pages],
			uploadCollections: [slugs.media],
		}),
		seoPlugin({
			collections: [slugs.pages],
			globals: ["site-settings"],
			generateDescription: ({ doc }) => {
				const title = String(doc?.title || doc?.siteName || "this editorial experience").trim();
				return `Explore ${title} in Payload's official SEO plugin comparison fixture, including generated metadata and a live search preview.`;
			},
			generateImage: ({ doc }) =>
				doc?.layout?.find?.((block: { image?: unknown }) => block.image)?.image || "",
			generateTitle: ({ doc }) =>
				`${String(doc?.title || doc?.siteName || "Payload parity").trim()} | Payload`,
			generateURL: ({ collectionConfig, doc, locale }) =>
				collectionConfig
					? `https://payloadcms.test/${locale || "en"}/pages/${doc?.slug || ""}`
					: `https://payloadcms.test/${locale || "en"}`,
			tabbedUI: true,
			uploadsCollection: slugs.media,
		}),
	],
	secret: process.env.PAYLOAD_SECRET || "ridu-payload-parity-fixture-secret",
	sharp,
	telemetry: false,
	typescript: {
		outputFile: path.resolve(dirname, "payload-types.ts"),
	},
});
