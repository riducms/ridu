export {
	blockDefinitionPath,
	cloneSchemaField,
	bindSchemaManifest,
	resolveBlockTypes,
	mapBlockTypes,
} from "./schema-registry.js";
export * from "./generated.js";

// The generator owns versioned wire shapes; this authored entry point owns the
// executable decoders so regeneration cannot overwrite boundary validation.
import type {
	ErrorCode,
	ErrorEnvelope,
	ErrorPayload,
	PageEnvelope,
	Pagination,
	UncountedPagination,
	ValidationIssue,
	AdminCollectionListDataV1,
	AccessCapabilitiesEnvelope,
	AdminPreparedRouteDataV1,
	AdminReadResultV1,
	FieldCapabilities,
	SchemaField,
} from "./generated.js";
import { blockDefinitionPath } from "./schema-registry.js";

/**
 * The capabilities of a block definition's field at a placement without an entry of
 * its own in `fields`, such as a row the client has yet to add: the definition's
 * entry, located through the bound fields along the canonical path.
 */
export function blockFieldCapabilities(
	access: AccessCapabilitiesEnvelope,
	fields: readonly SchemaField[],
	canonicalPath: string
): FieldCapabilities | undefined {
	if (access.blockFields === undefined) return undefined;
	const located = blockDefinitionPath(fields, canonicalPath);
	return located && access.blockFields[located.block]?.[located.path];
}

/** Validate prepared domain data once, before route controllers receive it. */
export function isAdminPreparedRouteData(value: unknown): value is AdminPreparedRouteDataV1 {
	if (!isRecord(value)) return false;
	switch (value.kind) {
		case "login":
		case "setup":
		case "dashboard":
		case "custom":
		case "not-found":
		case "error":
			return true;
		case "collection-list":
		case "collection-trash":
			return (
				isAdminCollectionListData(value.data) &&
				value.data.page !== undefined &&
				value.data.preferences !== undefined
			);
		case "collection-create":
			return (
				isRecord(value.create) &&
				isRecord(value.create.values) &&
				isReadResult(value.create.access, isAccessCapabilities)
			);
		case "collection-document":
		case "global-document":
		case "account":
		case "collection-api":
		case "global-api":
			return isDocumentData(value.document);
		case "upload":
			return isReadResult(value.access, isAccessCapabilities);
		case "collection-versions":
		case "global-versions": {
			const versions = value.versions;
			return (
				isRecord(versions) &&
				isReadResult(
					versions.history,
					(history) => Array.isArray(history) && history.every(isDocumentVersion)
				) &&
				(versions.detail === undefined || isReadResult(versions.detail, isDocumentVersion)) &&
				isDocumentData(versions.document)
			);
		}
		case "security": {
			const security = value.security;
			return (
				isRecord(security) &&
				isReadResult(
					security.sessions,
					(sessions) =>
						Array.isArray(sessions) &&
						sessions.every(
							(session) =>
								isRecord(session) &&
								typeof session.id === "string" &&
								typeof session.createdAt === "string" &&
								typeof session.lastSeenAt === "string" &&
								typeof session.expiresAt === "string" &&
								typeof session.current === "boolean"
						)
				) &&
				isReadResult(
					security.apiKeys,
					(keys) =>
						Array.isArray(keys) &&
						keys.every(
							(key) =>
								isRecord(key) &&
								typeof key.id === "string" &&
								typeof key.name === "string" &&
								typeof key.createdAt === "string"
						)
				)
			);
		}
		default:
			return false;
	}
}

function isReadResult(value: unknown, valid: (value: unknown) => boolean) {
	return (
		isRecord(value) &&
		(value.error === undefined
			? "value" in value && valid(value.value)
			: !("value" in value) && isErrorEnvelope(value))
	);
}

/**
 * Validate the shared loader-result envelope. Application-specific values remain
 * unknown here and are decoded by their generated loader references.
 */
export function isAdminLoaderResults(
	value: unknown
): value is Record<string, AdminReadResultV1<unknown>> {
	return (
		isRecord(value) && Object.values(value).every((result) => isReadResult(result, () => true))
	);
}

function isDocumentData(value: unknown) {
	return (
		isRecord(value) &&
		isReadResult(value.document, isAdminDocument) &&
		isReadResult(value.access, isAccessCapabilities)
	);
}

function isAdminDocument(value: unknown) {
	return (
		isRecord(value) &&
		typeof value.id === "string" &&
		(value._revision === undefined || isInteger(value._revision)) &&
		(value._publishedRevision === undefined ||
			(isInteger(value._publishedRevision) && value._publishedRevision > 0)) &&
		(value._hasDraftChanges === undefined || typeof value._hasDraftChanges === "boolean") &&
		(value._status === undefined || value._status === "draft" || value._status === "published") &&
		(value._localization === undefined ||
			(isRecord(value._localization) &&
				isRecord(value._localization.sources) &&
				Object.values(value._localization.sources).every((source) => typeof source === "string")))
	);
}

function isDocumentVersion(value: unknown) {
	return (
		isRecord(value) &&
		typeof value.ID === "string" &&
		typeof value.DocumentID === "string" &&
		isInteger(value.Revision) &&
		(value.Status === "draft" || value.Status === "published") &&
		isAdminDocument(value.Snapshot) &&
		typeof value.CreatedAt === "string"
	);
}

export function isAccessCapabilities(value: unknown): value is AccessCapabilitiesEnvelope {
	if (!isRecord(value) || !isRecord(value.operations) || !isRecord(value.fields)) return false;
	const operations = value.operations;
	const isFieldCapabilities = (field: unknown) =>
		isRecord(field) && ["read", "create", "update"].every((key) => typeof field[key] === "boolean");
	const blockFields = value.blockFields;
	if (
		blockFields !== undefined &&
		!(
			isRecord(blockFields) &&
			Object.values(blockFields).every(
				(definition) => isRecord(definition) && Object.values(definition).every(isFieldCapabilities)
			)
		)
	)
		return false;
	return (
		[
			"admin",
			"create",
			"read",
			"readVersions",
			"update",
			"delete",
			"duplicate",
			"publish",
			"unpublish",
			"restoreDeleted",
			"deletePermanent",
			"selectAll",
		].every((key) => typeof operations[key] === "boolean") &&
		Object.values(value.fields).every(isFieldCapabilities)
	);
}

/** Decode the semantic list result once at the HTTP/bootstrap boundary. */
export function isAdminCollectionListData(value: unknown): value is AdminCollectionListDataV1 {
	if (!isRecord(value) || !isRecord(value.query) || typeof value.query.trash !== "boolean")
		return false;
	if (value.query.locale !== undefined && typeof value.query.locale !== "string") return false;
	if (value.query.where !== undefined && !isRecord(value.query.where)) return false;
	if (value.query.countWhere !== undefined && !isRecord(value.query.countWhere)) return false;
	if (
		value.page !== undefined &&
		!isReadResult(value.page, (page) => {
			if (!isRecord(page) || !isPageEnvelope(page) || !isRecord(page.access)) return false;
			const { collection, documents } = page.access;
			return (
				isAccessCapabilities(collection) &&
				isRecord(documents) &&
				page.docs.every(
					(doc) =>
						isRecord(doc) && typeof doc.id === "string" && isAccessCapabilities(documents[doc.id])
				)
			);
		})
	)
		return false;
	if (
		!isRecord(value.counts) ||
		!Object.values(value.counts).every((count) => isReadResult(count, isInteger))
	)
		return false;
	return (
		value.preferences === undefined ||
		(isRecord(value.preferences) &&
			isReadResult(value.preferences.workspace, () => true) &&
			isReadResult(value.preferences.presets, () => true))
	);
}

const ERROR_CODES: ReadonlySet<ErrorCode> = new Set([
	"validation",
	"access_denied",
	"not_found",
	"conflict",
	"delete_restricted",
	"bad_request",
	"internal",
	"rate_limited",
	"email_not_verified",
	"auth_feature_disabled",
	"invalid_auth_token",
	"invalid_credential",
	"invalid_preview_token",
	"rejected",
	"selection_too_large",
	"bad_query",
	"publish_required",
]);

export function isValidationIssue(value: unknown): value is ValidationIssue {
	return (
		isRecord(value) &&
		typeof value.code === "string" &&
		typeof value.path === "string" &&
		typeof value.message === "string" &&
		["target", "fieldId", "collectionId", "globalId", "locale"].every(
			(key) => value[key] === undefined || typeof value[key] === "string"
		)
	);
}

export function isErrorEnvelope(value: unknown): value is ErrorEnvelope {
	if (!isRecord(value) || !isRecord(value.error)) {
		return false;
	}
	const error = value.error;
	return (
		typeof error.code === "string" &&
		ERROR_CODES.has(error.code as ErrorCode) &&
		typeof error.status === "number" &&
		Number.isInteger(error.status) &&
		typeof error.message === "string" &&
		(error.requestId === undefined || typeof error.requestId === "string") &&
		Array.isArray(error.issues) &&
		error.issues.every(isValidationIssue)
	);
}

/** Validate a counted page, the default list response, including both totals. */
export function isPageEnvelope<Document>(value: unknown): value is PageEnvelope<Document> {
	return (
		isRecord(value) &&
		Array.isArray(value.docs) &&
		isPageMetadata(value.pagination) &&
		isInteger(value.pagination.totalDocs) &&
		isInteger(value.pagination.totalPages)
	);
}

/** Validate a `pagination=false` page, which must carry neither total. */
export function isUncountedPageEnvelope<Document>(
	value: unknown
): value is PageEnvelope<Document, UncountedPagination> {
	return (
		isRecord(value) &&
		Array.isArray(value.docs) &&
		isPageMetadata(value.pagination) &&
		!("totalDocs" in value.pagination) &&
		!("totalPages" in value.pagination)
	);
}

export function errorPayload(envelope: ErrorEnvelope): ErrorPayload {
	return envelope.error;
}

function isPageMetadata(
	value: unknown
): value is Omit<Pagination, "totalDocs" | "totalPages"> & Record<string, unknown> {
	return (
		isRecord(value) &&
		isInteger(value.page) &&
		isInteger(value.limit) &&
		typeof value.hasNextPage === "boolean" &&
		typeof value.hasPrevPage === "boolean"
	);
}

/** Narrow a non-null, non-array object; its fields and prototype remain unchecked. */
export function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isInteger(value: unknown): value is number {
	return typeof value === "number" && Number.isInteger(value);
}
