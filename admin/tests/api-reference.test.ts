import { describe, expect, it } from "bun:test";
import { mkdtemp, rm, mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import {
	SCHEMA_MANIFEST_VERSION,
	type SchemaCollection,
	type SchemaField,
	type SchemaManifest,
} from "@riducms/protocol";
import { createAdminI18n } from "@riducms/translations";
import type { AdminConfig } from "@admin/core/api/admin-client";
import { initialFormValues } from "@admin/core/forms/form-schema";
import { createClient } from "@riducms/sdk";
import { collectionReferenceSchema } from "@admin/features/api-reference/api-reference-schema";
import {
	referenceCode,
	referenceOperations,
	referenceParameters,
	referenceRequest,
	referenceResponses,
	type ReferenceContext,
} from "@admin/features/api-reference/api-reference-examples";

const i18n = createAdminI18n();
const field = (name: string, options: Partial<SchemaField> = {}): SchemaField => ({
	id: name,
	name,
	path: name,
	type: "text",
	category: "scalar",
	required: false,
	unique: false,
	admin: { label: name },
	...options,
});

function context(kind: "ordinary" | "auth" | "upload" = "ordinary"): ReferenceContext {
	const collection: SchemaCollection = {
		id: "articles",
		slug: "articles",
		labels: { singular: "Article", plural: "Articles" },
		admin: {},
		capabilities: {
			auth: kind === "auth",
			upload: kind === "upload",
			versions: false,
			trash: false,
			global: false,
			locking: false,
		},
		fields: [
			field("headline", { required: true }),
			field("location", { type: "point" }),
			field("kind", { type: "select", select: { options: [{ value: "news", label: "News" }] } }),
		],
	};
	const manifest: SchemaManifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "Examples" },
		collections: [collection],
		plugins: [],
	};
	return {
		collection,
		manifest,
		schema: collectionReferenceSchema(collection, manifest, i18n),
		origin: "https://ridu.example",
		documentID: "article-1",
	};
}

function reproject(value: ReferenceContext) {
	value.schema = collectionReferenceSchema(value.collection, value.manifest, i18n);
	return value;
}

describe("collection API reference", () => {
	it("uses schema paths, output ownership and exact point/relationship shapes", () => {
		const source = context();
		source.collection.fields.push(
			field("owner", {
				type: "relationship",
				relationship: {
					polymorphic: true,
					onDelete: "nullify",
					targets: [{ collectionId: "users", collectionSlug: "users" }],
				},
			}),
			field("slug", { admin: { label: "Slug", readOnly: true } }),
			field("computed", { type: "virtual", virtual: { valueType: "string" } }),
			field("seo", {
				type: "group",
				localized: true,
				nested: { fields: [field("title", { required: true })] },
			}),
			field("tags", {
				type: "array",
				nested: { minRows: 2, fields: [field("label", { required: true })] },
			}),
			field("body", { type: "plugin", plugin: { key: "example:richtext", config: {} } })
		);
		const { schema } = reproject(source);
		expect(schema.create.location).toEqual([-0.12, 51.5]);
		expect(schema.create.owner).toEqual({ relationTo: "users", id: "RELATED_ID" });
		expect(schema.create).toHaveProperty("slug");
		expect(schema.create).not.toHaveProperty("computed");
		expect(schema.fields.find((item) => item.path === "seo.title")).toMatchObject({
			requiredOnCreate: false,
			localized: true,
		});
		expect(schema.create.tags).toHaveLength(2);
		expect(schema.fields.some((item) => item.path === "tags[].label")).toBe(true);
		expect(schema.omitted).toEqual(["body"]);
		expect(schema.filter?.path).toBe("headline");
	});

	it("keeps deeply recursive and large required examples bounded and explicit", () => {
		const source = context();
		const recursive = field("children", { type: "group", nested: { fields: [] } });
		recursive.nested!.fields.push(recursive);
		source.collection.fields.push(
			recursive,
			field("many", { type: "text-list", list: { minRows: 10000 } })
		);
		const { schema } = reproject(source);
		expect(schema.omitted).toEqual(["children.children", "many"]);
		expect(schema.fields.length).toBeLessThan(10);
	});

	it("uses the canonical manifest decoder for literal defaults", () => {
		const source = context();
		source.collection.fields = [
			field("status", { default: "draft" }),
			field("publishedOn", {
				type: "date",
				default: "2027-02-03",
				date: { format: "date" },
			}),
			field("priority", { type: "number", default: "2.5" }),
			field("featured", { type: "checkbox", default: "false" }),
			field("keywords", {
				type: "text-list",
				default: '["ridu","cms"]',
				list: {},
			}),
			field("kind", {
				type: "select",
				default: "opinion",
				select: {
					options: [
						{ value: "news", label: "News" },
						{ value: "opinion", label: "Opinion" },
					],
				},
			}),
			field("audiences", {
				type: "select",
				select: {
					hasMany: true,
					defaultValues: ["editors", "authors"],
					options: [
						{ value: "editors", label: "Editors" },
						{ value: "authors", label: "Authors" },
					],
				},
			}),
		];
		const defaults = initialFormValues(source.collection.fields);
		const { create } = reproject(source).schema;

		for (const name of Object.keys(defaults)) expect(create[name]).toEqual(defaults[name]);
	});

	it("reports create requiredness from generated input semantics and its ancestors", () => {
		const source = context();
		source.collection.fields = [
			field("title", { required: true }),
			field("literalDefault", { required: true, default: "generated" }),
			field("dynamicDefault", { required: true, dynamicDefault: true }),
			field("slug", {
				required: true,
				text: { slug: { sourcePath: "title" } },
			}),
			field("defaultedSelection", {
				type: "select",
				required: true,
				select: {
					hasMany: true,
					defaultValues: ["news"],
					options: [{ value: "news", label: "News" }],
				},
			}),
			field("requiredList", { type: "text-list", list: { minRows: 1 } }),
			field("optionalGroup", {
				type: "group",
				nested: { fields: [field("requiredChild", { required: true })] },
			}),
			field("requiredGroup", {
				type: "group",
				required: true,
				nested: { fields: [field("requiredChild", { required: true })] },
			}),
		];

		const required = Object.fromEntries(
			reproject(source).schema.fields.map((item) => [item.path, item.requiredOnCreate])
		);
		expect(required).toEqual({
			title: true,
			literalDefault: false,
			dynamicDefault: false,
			slug: false,
			defaultedSelection: false,
			requiredList: true,
			optionalGroup: false,
			"optionalGroup.requiredChild": false,
			requiredGroup: true,
			"requiredGroup.requiredChild": true,
		});
	});

	it("uses REST locale parameter names and only advertises them for localized fields", async () => {
		const source = context();
		source.manifest.application.localization = {
			defaultLocale: "en",
			fallback: true,
			locales: [
				{ code: "en", label: "English" },
				{ code: "fr", label: "French" },
			],
		};

		expect(referenceParameters(source, "list", i18n).map((item) => item.name)).not.toContain(
			"locale"
		);
		source.collection.fields[0]!.localized = true;

		const openAPI = await Bun.file(
			resolve(import.meta.dir, "../../tests/contracts/unifiedfields/generated/openapi.json")
		).json();
		const paths = openAPI.paths as Record<
			string,
			Record<string, { parameters?: { name: string }[] }>
		>;
		const operations = {
			list: ["/api/collections/unified-articles", "get"],
			read: ["/api/collections/unified-articles/{id}", "get"],
			create: ["/api/collections/unified-articles", "post"],
			update: ["/api/collections/unified-articles/{id}", "patch"],
			delete: ["/api/collections/unified-articles/{id}", "delete"],
		} as const;

		for (const operation of referenceOperations) {
			const [path, method] = operations[operation];
			const canonical = (paths[path]![method]!.parameters ?? []).map((parameter) => parameter.name);
			const advertised = referenceParameters(source, operation, i18n).map(
				(parameter) => parameter.name
			);
			expect(advertised).toEqual(canonical);
		}
	});

	it("does not offer managed upload fields or put passwords in ordinary document values", () => {
		const upload = context("upload");
		upload.collection.fields.push(field("filename"), field("width", { type: "number" }));
		reproject(upload);
		expect(upload.schema.create).not.toHaveProperty("filename");
		expect(upload.schema.fields.find((item) => item.path === "width")?.writable).toBe(false);
		expect(referenceRequest(upload, "update")).toMatchObject({
			method: "PATCH",
			url: "https://ridu.example/api/collections/articles/article-1/upload",
			body: { data: upload.schema.update },
		});
		const auth = context("auth");
		expect(referenceRequest(auth, "create")).toMatchObject({
			url: "https://ridu.example/api/auth/articles/create-user",
			body: { data: auth.schema.create, password: "REPLACE_WITH_A_PASSWORD" },
		});
		expect(auth.schema.create).not.toHaveProperty("password");
	});

	it("matches SDK requests and response envelopes for all documented operations", async () => {
		for (const kind of ["ordinary", "auth", "upload"] as const) {
			const value = context(kind);
			let actual: Request | undefined;
			const client = createClient<AdminConfig>({
				baseURL: value.origin,
				fetch: async (input, init) => {
					actual = new Request(input, init);
					const op =
						actual.method === "DELETE"
							? "delete"
							: actual.method === "POST"
								? "create"
								: actual.method === "PATCH"
									? "update"
									: actual.url.includes("?")
										? "list"
										: "read";
					return Response.json(referenceResponses(value, op)[0]!.value);
				},
			});
			for (const operation of referenceOperations) {
				const request = referenceRequest(value, operation);
				switch (operation) {
					case "list":
						await client.list("articles", {
							page: 1,
							limit: 10,
							where: { headline: { equals: value.schema.filter!.value } },
						});
						break;
					case "read":
						await client.find("articles", "article-1");
						break;
					case "create":
						if (kind === "auth")
							await client.auth.createUser({
								collection: "articles",
								data: value.schema.create,
								password: "REPLACE_WITH_A_PASSWORD",
							});
						else if (kind === "upload")
							await client.upload("articles", new File(["text"], "test.txt"), {
								data: value.schema.create,
							});
						else await client.create("articles", value.schema.create);
						break;
					case "update":
						if (kind === "upload")
							await client.updateUpload("articles", "article-1", { data: value.schema.update });
						else await client.update("articles", "article-1", value.schema.update);
						break;
					case "delete":
						await client.delete("articles", "article-1");
						break;
				}
				expect(actual!.method).toBe(request.method);
				const url = new URL(actual!.url);
				expect(url.origin + url.pathname).toBe(request.url);
				expect(Object.fromEntries(url.searchParams)).toEqual(request.query);
				if (request.uploadCreate)
					expect((await actual!.formData()).get("data")).toBe(JSON.stringify(request.body));
				else if (request.body) expect(await actual!.json()).toEqual(request.body);
			}
		}
	});

	it("quotes cURL data literally even with shell metacharacters, quotes and newlines", async () => {
		const source = context();
		source.schema.create.headline =
			"It's `printf unexpected` $(printf unexpected)\n\\backslash\t café";
		const code = referenceCode(source, "create", "curl");
		const process = Bun.spawn(["sh", "-c", `curl() { printf '%s\\0' "$@"; };\n${code}`], {
			stdout: "pipe",
			stderr: "pipe",
		});
		const args = (await new Response(process.stdout).text()).split("\0");
		expect(await process.exited).toBe(0);
		expect(JSON.parse(args[args.indexOf("--data-raw") + 1]!)).toEqual(source.schema.create);
	});

	it("compiles generated Go examples against the real public API", async () => {
		const root = resolve(import.meta.dir, "../..");
		await mkdir(resolve(root, ".ridu"), { recursive: true });
		const directory = await mkdtemp(resolve(root, ".ridu/api-reference-go-"));
		try {
			for (const kind of ["ordinary", "auth", "upload"] as const) {
				const source = context(kind);
				source.schema.create.headline = "Unicode café and \\backslash\b\f\n";
				for (const operation of referenceOperations) {
					const code = referenceCode(source, operation, "go").replace(
						"func Example(",
						`func Example_${kind}_${operation}(`
					);
					await writeFile(resolve(directory, `${kind}_${operation}.go`), code);
				}
			}
			const process = Bun.spawn(["go", "test", directory], {
				cwd: root,
				stdout: "pipe",
				stderr: "pipe",
			});
			const output = await new Response(process.stderr).text();
			expect(await process.exited, output).toBe(0);
		} finally {
			await rm(directory, { recursive: true, force: true });
		}
	}, 30000);
});

it("type-checks the TypeScript examples including upload field literal unions", async () => {
	const root = resolve(import.meta.dir, "../..");
	const directory = await mkdtemp(resolve(root, ".ridu/api-reference-ts-"));
	try {
		await mkdir(resolve(directory, "generated"));
		await writeFile(
			resolve(directory, "generated/ridu.generated.ts"),
			`
import type { CollectionContract } from "@riducms/sdk";
type Data = { headline: string; location?: [number, number]; kind?: "news" };
export interface RiduConfig { collections: { articles: CollectionContract & {
 auth: true; upload: true; versions: false; drafts: false; trash: false;
 output: Data & { id: string }; create: Data; update: Partial<Data>;
 where: { headline?: { equals?: string } }; select: Record<string, boolean>; populate: {};
} } }
`
		);
		for (const kind of ["ordinary", "auth", "upload"] as const) {
			for (const operation of referenceOperations) {
				await writeFile(
					resolve(directory, `${kind}-${operation}.ts`),
					referenceCode(context(kind), operation, "typescript")
				);
			}
		}
		await writeFile(
			resolve(directory, "tsconfig.json"),
			JSON.stringify({
				compilerOptions: {
					target: "ES2022",
					module: "ESNext",
					moduleResolution: "Bundler",
					strict: true,
					skipLibCheck: true,
					noEmit: true,
				},
				include: ["**/*.ts"],
			})
		);
		const process = Bun.spawn(["bun", "x", "tsc", "-p", directory], {
			cwd: root,
			stdout: "pipe",
			stderr: "pipe",
		});
		const output = await new Response(process.stdout).text();
		expect(await process.exited, output).toBe(0);
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
}, 30000);

it("documents output row identity and upload metadata independently from inputs", () => {
	const source = context("upload");
	source.collection.fields.push(
		field("tags", {
			type: "array",
			required: true,
			nested: { fields: [field("label", { required: true })] },
		})
	);
	reproject(source);
	const response = referenceResponses(source, "create")[0]!.value;
	expect(response).toMatchObject({
		doc: {
			id: "NEW_DOCUMENT_ID",
			_revision: 1,
			filename: "example.png",
			tags: [{ _key: "example-row-1", label: "Example" }],
		},
	});
	expect(source.schema.create.tags).toEqual([{ label: "Example" }]);
	expect(referenceResponses(source, "read")[1]!.value).toMatchObject({
		error: { code: "access_denied", status: 403 },
	});
});

it("keeps constrained email samples valid or explicitly omitted", () => {
	const source = context();
	source.collection.fields = [
		field("short", { type: "email", text: { maxLength: 10 } }),
		field("long", { type: "email", text: { minLength: 30 } }),
		field("custom", { type: "email", text: { minLength: 100 } }),
	];
	reproject(source);
	expect(source.schema.create.short).toBe("perso@a.co");
	expect(String(source.schema.create.long)).toHaveLength(30);
	expect(String(source.schema.create.long)).toEndWith("@example.com");
	expect(source.schema.omitted).toEqual(["custom"]);
});
