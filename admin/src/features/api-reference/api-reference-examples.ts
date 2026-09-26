import {
	resolveBlockTypes,
	isRecord,
	type ErrorCode,
	type SchemaCollection,
	type SchemaField,
	type SchemaManifest,
} from "@riducms/protocol";
import type { AdminI18n } from "@riducms/plugin";
import { fieldsUseLocalization } from "@admin/core/schema/field-localization";
import type { CollectionReferenceSchema } from "@admin/features/api-reference/api-reference-schema";

export const referenceOperations = ["list", "read", "create", "update", "delete"] as const;
export type ReferenceOperation = (typeof referenceOperations)[number];
export type ReferenceLanguage = "typescript" | "curl" | "go";

export type ReferenceContext = {
	collection: SchemaCollection;
	manifest: SchemaManifest;
	schema: CollectionReferenceSchema;
	origin: string;
	documentID?: string;
};

export function referenceRequest(context: ReferenceContext, operation: ReferenceOperation) {
	const { collection, schema, origin, documentID = "DOCUMENT_ID" } = context;
	const slug = encodeURIComponent(collection.slug);
	const base = `/api/collections/${slug}`;
	const authCreate = operation === "create" && collection.capabilities.auth;
	const uploadCreate = operation === "create" && collection.capabilities.upload && !authCreate;
	const uploadUpdate = operation === "update" && collection.capabilities.upload;
	const path = authCreate
		? `/api/auth/${slug}/create-user`
		: operation === "list" || operation === "create"
			? base
			: `${base}/${encodeURIComponent(documentID)}${uploadUpdate ? "/upload" : ""}`;
	const method =
		operation === "create"
			? "POST"
			: operation === "update"
				? "PATCH"
				: operation === "delete"
					? "DELETE"
					: "GET";
	const query: Record<string, string> = operation === "list" ? { page: "1", limit: "10" } : {};

	if (operation === "list" && schema.filter) {
		query.where = JSON.stringify({ [schema.filter.path]: { equals: schema.filter.value } });
	}

	const data = operation === "create" ? schema.create : schema.update;
	const body = authCreate
		? { data, password: "REPLACE_WITH_A_PASSWORD" }
		: uploadUpdate
			? { data }
			: operation === "create" || operation === "update"
				? data
				: undefined;

	return {
		method,
		url: new URL(path, origin).href,
		query,
		body,
		uploadCreate,
		uploadUpdate,
		authCreate,
	};
}

export function referenceCode(
	context: ReferenceContext,
	operation: ReferenceOperation,
	language: ReferenceLanguage
): string {
	const request = referenceRequest(context, operation);
	const { collection, schema, origin, documentID = "DOCUMENT_ID" } = context;
	const slug = JSON.stringify(collection.slug);
	const id = JSON.stringify(documentID);
	const data = JSON.stringify(operation === "create" ? schema.create : schema.update, null, 2);

	if (language === "curl") {
		const lines = [
			`curl --request ${request.method} ${shellQuote(request.url)}`,
			"  --header 'Authorization: Bearer REPLACE_WITH_TOKEN'",
		];
		if (operation === "list") {
			lines.push(
				"  --get",
				...Object.entries(request.query).map(
					([key, value]) => `  --data-urlencode ${shellQuote(`${key}=${value}`)}`
				)
			);
		} else if (request.uploadCreate) {
			lines.push(
				"  --form 'file=@/path/to/file'",
				`  --form-string ${shellQuote(`data=${JSON.stringify(request.body)}`)}`
			);
		} else if (request.body !== undefined) {
			lines.push(
				"  --header 'Content-Type: application/json'",
				`  --data-raw ${shellQuote(JSON.stringify(request.body, null, 2))}`
			);
		}
		return lines.join(" \\\n");
	}

	if (language === "go") return goExample(context, operation);

	const setup = `import { createClient } from "@riducms/sdk";\nimport type { RiduConfig } from "./generated/ridu.generated";\n\nconst ridu = createClient<RiduConfig>({\n  baseURL: ${JSON.stringify(origin)},\n  credentials: "include",\n});\n\n`;
	let call: string;

	switch (operation) {
		case "list": {
			const options = {
				page: 1,
				limit: 10,
				...(schema.filter
					? { where: { [schema.filter.path]: { equals: schema.filter.value } } }
					: {}),
			};
			call = `const page = await ridu.list(${slug}, ${JSON.stringify(options, null, 2)});`;
			break;
		}
		case "read":
			call = `const doc = await ridu.find(${slug}, ${id});`;
			break;
		case "create":
			if (request.authCreate)
				call = `const doc = await ridu.createAuthUser(${slug}, ${data}, "REPLACE_WITH_A_PASSWORD");`;
			else if (request.uploadCreate)
				call = `async function createUpload(file: File) {\n  const data: RiduConfig["collections"][${slug}]["create"] = ${data};\n  return ridu.upload(${slug}, file, { data });\n}`;
			else call = `const doc = await ridu.create(${slug}, ${data});`;
			break;
		case "update":
			call = request.uploadUpdate
				? `const doc = await ridu.updateUpload(${slug}, ${id}, {\n  data: ${data}\n});`
				: `const doc = await ridu.update(${slug}, ${id}, ${data});`;
			break;
		case "delete":
			call = `const result = await ridu.delete(${slug}, ${id});`;
			break;
	}

	return setup + call;
}

function goExample(
	{ collection, schema, documentID = "DOCUMENT_ID" }: ReferenceContext,
	operation: ReferenceOperation
): string {
	const slug = goQuote(collection.slug);
	const id = goQuote(documentID);
	const imports = ["context", "github.com/riducms/ridu", "github.com/riducms/ridu/store"];
	let optionType =
		operation === "list" ? "ListOptions" : operation === "read" ? "FindOptions" : "MutationOptions";
	const resultType = operation === "list" ? "store.Page" : "store.Document";
	const lines: string[] = [];
	let call: string;

	if (operation === "list") {
		lines.push("options.Page = 1", "options.Limit = 10");
		if (schema.filter) {
			imports.push("github.com/riducms/ridu/query");
			lines.push(
				`path, err := query.NewPath(${goQuote(schema.filter.path)})`,
				"if err != nil { return store.Page{}, err }"
			);
			const kind =
				typeof schema.filter.value === "number"
					? "Number"
					: typeof schema.filter.value === "boolean"
						? "Boolean"
						: "String";
			lines.push(
				`options.Where = query.Equal(path, query.${kind}(${goLiteral(schema.filter.value)}))`
			);
		}
		call = `app.Local().List(ctx, ${slug}, options)`;
	} else if (operation === "read") call = `app.Local().Find(ctx, ${slug}, ${id}, options)`;
	else if (operation === "delete") call = `app.Local().Delete(ctx, ${slug}, ${id}, options)`;
	else {
		const values = goValues(operation === "create" ? schema.create : schema.update);
		if (operation === "create" && collection.capabilities.auth) {
			lines.push(`data := ${values}`);
			call = `app.CreateAuthUser(ctx, ${slug}, data, "REPLACE_WITH_A_PASSWORD", options)`;
		} else if (collection.capabilities.upload) {
			optionType = operation === "create" ? "UploadInput" : "UpdateUploadInput";
			lines.push(`options.Data = ${values}`);
			call =
				operation === "create"
					? `app.Upload(ctx, ${slug}, options)`
					: `app.UpdateUpload(ctx, ${slug}, ${id}, options)`;
		} else {
			lines.push(`data := ${values}`);
			call =
				operation === "create"
					? `app.Local().Create(ctx, ${slug}, data, options)`
					: `app.Local().Update(ctx, ${slug}, ${id}, data, options)`;
		}
	}

	lines.push(`return ${call}`);
	return `package example\n\nimport (\n${imports.map((path) => `\t${goQuote(path)}`).join("\n")}\n)\n\nfunc Example(ctx context.Context, app *ridu.App, options ridu.${optionType}) (${resultType}, error) {\n${lines
		.join("\n")
		.split("\n")
		.map((line) => `\t${line}`)
		.join("\n")}\n}`;
}

function goValues(value: Record<string, unknown>, depth = 0): string {
	const entries = Object.entries(value).map(
		([key, item]) => `${"\t".repeat(depth + 1)}${goQuote(key)}: ${goValue(item, depth + 1)},`
	);
	return entries.length
		? `store.Values{\n${entries.join("\n")}\n${"\t".repeat(depth)}}`
		: "store.Values{}";
}

function goValue(value: unknown, depth = 0): string {
	if (value === null) return "store.Null()";
	if (Array.isArray(value))
		return `store.List(${value.map((item) => goValue(item, depth)).join(", ")})`;
	if (typeof value === "object")
		return `store.Object(${goValues(value as Record<string, unknown>, depth)})`;
	const kind =
		typeof value === "boolean" ? "Boolean" : typeof value === "number" ? "Number" : "String";
	return `store.${kind}(${goLiteral(value)})`;
}

function goLiteral(value: unknown): string {
	return typeof value === "string" ? goQuote(value) : JSON.stringify(value);
}

function goQuote(value: string): string {
	return JSON.stringify(value);
}

function shellQuote(value: string): string {
	return `'${value.replaceAll("'", "'\\''")}'`;
}

function outputExample(
	fields: SchemaField[],
	values: Record<string, unknown>
): Record<string, unknown> {
	const result = { ...values };
	for (const field of fields) {
		const value = values[field.name];
		if (field.type === "group" && isRecord(value))
			result[field.name] = outputExample(field.nested?.fields ?? [], value);
		if ((field.type === "array" || field.type === "blocks") && Array.isArray(value)) {
			result[field.name] = value.map((row, index) => {
				if (!isRecord(row)) return row;
				const children =
					field.type === "array"
						? field.nested?.fields
						: resolveBlockTypes(field.blocks).find((block) => block.slug === row.blockType)?.fields;
				return { _key: `example-row-${index + 1}`, ...outputExample(children ?? [], row) };
			});
		}
	}
	return result;
}

export function referenceResponses(context: ReferenceContext, operation: ReferenceOperation) {
	const { collection, schema, documentID = "DOCUMENT_ID" } = context;
	const doc = {
		id: operation === "create" ? "NEW_DOCUMENT_ID" : documentID,
		...outputExample(collection.fields, schema.create),
		createdAt: "2026-01-01T12:00:00Z",
		updatedAt: "2026-01-01T12:00:00Z",
		...(collection.capabilities.versions || collection.capabilities.upload ? { _revision: 1 } : {}),
		...(collection.capabilities.versions ? { _status: "published" } : {}),
		...(collection.capabilities.upload
			? {
					filename: "example.png",
					mimeType: "image/png",
					filesize: 1024,
					width: 100,
					height: 100,
					objectKey: "EXAMPLE_STORAGE_KEY/example.png",
					url: `/api/uploads/${encodeURIComponent(collection.slug)}/EXAMPLE_STORAGE_KEY/example.png`,
				}
			: {}),
	};
	const success =
		operation === "list"
			? {
					docs: [doc],
					pagination: {
						page: 1,
						limit: 10,
						totalDocs: 1,
						totalPages: 1,
						hasNextPage: false,
						hasPrevPage: false,
					},
				}
			: operation === "delete"
				? { id: documentID, deleted: true }
				: { doc };
	const error = (status: number, code: ErrorCode, message: string) => ({
		status,
		value: { error: { code, status, message, issues: [] } },
	});
	const responses: { status: number; value: unknown }[] = [
		{ status: operation === "create" ? 201 : 200, value: success },
		error(403, "access_denied", "Access denied"),
	];
	if (operation !== "create" && operation !== "list")
		responses.push(error(404, "not_found", "Document not found"));
	if (operation === "create" || operation === "update") {
		responses.push({
			status: 422,
			value: {
				error: {
					code: "validation",
					status: 422,
					message: "Validation failed",
					issues: [
						{
							code: "required",
							path:
								schema.fields.find((field) => field.writable && field.requiredOnCreate)?.path ??
								"FIELD_PATH",
							message: "This field is required.",
						},
					],
				},
			},
		});
		responses.push(
			error(409, "conflict", "The document has changed. Read the current revision before retrying.")
		);
	}
	return responses;
}

export function referenceParameters(
	context: ReferenceContext,
	operation: ReferenceOperation,
	i18n: AdminI18n
) {
	const names: string[] = [];
	if (operation === "list") names.push("page", "limit", "where", "sort", "include-access");
	if (operation === "list" || operation === "read") names.push("depth", "select", "populate");
	if (operation === "list" && context.collection.capabilities.trash) names.push("trash");
	if (
		context.manifest.application.localization &&
		fieldsUseLocalization(context.collection.fields)
	) {
		names.push("locale");
		names.push("fallback-locale");
	}
	if (
		operation === "create" &&
		context.collection.versionSettings?.drafts &&
		!context.collection.capabilities.upload &&
		!context.collection.capabilities.auth
	)
		names.push("draft");
	const descriptions = {
		page: "apiReference:queryPage",
		limit: "apiReference:queryLimit",
		where: "apiReference:queryWhere",
		sort: "apiReference:querySort",
		"include-access": "apiReference:queryAccess",
		depth: "apiReference:queryDepth",
		select: "apiReference:querySelect",
		populate: "apiReference:queryPopulate",
		trash: "apiReference:queryTrash",
		locale: "apiReference:queryLocale",
		"fallback-locale": "apiReference:queryFallback",
		draft: "apiReference:queryDraft",
	} as const;
	return names.map((name) => ({
		name,
		description: i18n.t(descriptions[name as keyof typeof descriptions]),
	}));
}
