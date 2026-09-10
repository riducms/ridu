export {
	cloneSchemaField,
	bindSchemaManifest,
	resolveBlockTypes,
	mapBlockTypes,
} from "./schema-registry.js";
export * from "./generated.js";

import type {
	ErrorCode,
	ErrorEnvelope,
	ErrorPayload,
	PageEnvelope,
	Pagination,
	ValidationIssue,
} from "./generated.js";

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
	"invalid_preview_token",
	"selection_too_large",
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

export function isPageEnvelope<Document>(value: unknown): value is PageEnvelope<Document> {
	return isRecord(value) && Array.isArray(value.docs) && isPagination(value.pagination);
}

export function errorPayload(envelope: ErrorEnvelope): ErrorPayload {
	return envelope.error;
}

function isPagination(value: unknown): value is Pagination {
	return (
		isRecord(value) &&
		isInteger(value.page) &&
		isInteger(value.limit) &&
		isInteger(value.totalDocs) &&
		isInteger(value.totalPages) &&
		typeof value.hasNextPage === "boolean" &&
		typeof value.hasPrevPage === "boolean"
	);
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isInteger(value: unknown): value is number {
	return typeof value === "number" && Number.isInteger(value);
}
