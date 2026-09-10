import { sqliteAdapter } from "@payloadcms/db-sqlite";
import { BlocksFeature, lexicalEditor } from "@payloadcms/richtext-lexical";
import path from "node:path";
import { buildConfig, type Field, type TextField } from "payload";
import { trace } from "./trace";

const password = process.env.PAYLOAD_TEST_PASSWORD;
const secret = process.env.PAYLOAD_SECRET;
if (!password || !secret)
	throw new Error(
		"Set disposable PAYLOAD_TEST_PASSWORD and PAYLOAD_SECRET before starting this fixture."
	);
function checked(name: string, label: string, skipOnChange = false): TextField {
	return {
		name,
		label,
		type: "text",
		validate: async (value, args) => {
			const { data, siblingData, req, event, operation, previousValue, id } = args;
			const supplier =
				(siblingData as { supplier?: unknown })?.supplier ??
				(data as { supplier?: unknown })?.supplier;
			const token = `${label}:${Date.now()}:${Math.random()}`;
			trace({
				phase: "start",
				token,
				label,
				value,
				event,
				operation,
				previousValue,
				id,
				data,
				siblingData,
				locale: req.locale,
				actor: req.user?.id,
				hasPayload: !!req.payload,
				blockData: args.blockData,
				path: args.path,
			});
			if (skipOnChange && event === "onChange") {
				trace({ phase: "skip", token, label });
				return true;
			}
			if (value === "slow-invalid") await new Promise((resolve) => setTimeout(resolve, 1600));
			if (value === "fast-good") await new Promise((resolve) => setTimeout(resolve, 20));
			if (value === "throw") throw new Error("private fixture failure");
			const result =
				typeof value !== "string" || !value
					? `${label}: enter a value`
					: value === "slow-invalid" ||
						  value === "invalid" ||
						  value === "unavailable" ||
						  (label === "SKU" && supplier === "blocked")
						? `${label}: unavailable for supplier ${String(supplier || "")}`
						: true;
			trace({ phase: "end", token, label, value, result });
			return result;
		},
	};
}
const nestedFields = (prefix: string): Field[] => [
	checked("code", `${prefix} code`),
	{ name: "links", type: "array", fields: [checked("url", `${prefix} URL`)] },
];
export default buildConfig({
	secret,
	telemetry: false,
	admin: {
		user: "users",
		importMap: { baseDir: path.resolve("src") },
		autoLogin: { email: "fixture@example.invalid", password, prefillOnly: false },
	},
	db: sqliteAdapter({ client: { url: "file:./fixture.db" } }),
	localization: { defaultLocale: "en", fallback: true, locales: ["en", "fr"] },
	editor: lexicalEditor({
		features: ({ defaultFeatures }) => [
			...defaultFeatures,
			BlocksFeature({ blocks: [{ slug: "card", fields: nestedFields("Embedded") }] }),
		],
	}),
	collections: [
		{ slug: "users", auth: true, fields: [] },
		{
			slug: "products",
			fields: [
				{ name: "supplier", type: "text" },
				checked("sku", "SKU"),
				checked("saveOnly", "Save only", true),
				{ name: "quantity", type: "number", required: true },
				{ name: "seo", type: "group", fields: [checked("title", "SEO title")] },
				{ name: "variants", type: "array", fields: nestedFields("Variant") },
				{
					name: "sections",
					type: "blocks",
					blocks: [{ slug: "card", fields: nestedFields("Block") }],
				},
				{ ...checked("translation", "Translation"), localized: true },
				{ name: "body", type: "richText" },
			],
			hooks: {
				beforeValidate: [
					({ operation, data }) => {
						trace({ phase: "beforeValidate", operation, data });
					},
				],
				afterChange: [
					({ operation, doc }) => {
						trace({ phase: "afterChange", operation, id: doc.id });
					},
				],
			},
		},
	],
	onInit: async (payload) => {
		const users = await payload.find({ collection: "users", limit: 1 });
		if (users.totalDocs === 0)
			await payload.create({
				collection: "users",
				data: { email: "fixture@example.invalid", password },
			});
		const products = await payload.find({ collection: "products", limit: 1 });
		if (products.totalDocs === 0)
			await payload.create({
				collection: "products",
				locale: "en",
				data: {
					supplier: "acme",
					sku: "persisted",
					saveOnly: "valid",
					quantity: 1,
					seo: { title: "persisted SEO" },
					variants: [{ code: "persisted Variant", links: [{ url: "valid" }] }],
					sections: [{ blockType: "card", code: "persisted Block", links: [{ url: "valid" }] }],
					translation: "English persisted",
					body: {
						root: {
							type: "root",
							version: 1,
							format: "",
							indent: 0,
							direction: null,
							children: [
								{
									type: "block",
									version: 2,
									format: "",
									fields: {
										id: "embedded-card",
										blockType: "card",
										blockName: "Embedded card",
										code: "persisted Embedded",
										links: [{ id: "embedded-link", url: "valid" }],
									},
								},
							],
						},
					},
				},
			});
	},
});
