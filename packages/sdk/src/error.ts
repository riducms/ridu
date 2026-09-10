import {
	errorPayload,
	isErrorEnvelope,
	type ErrorCode,
	type ErrorPayload,
	type ValidationIssue,
} from "@riducms/protocol";

/**
 * A structured failure returned by Ridu's typed SDK operations.
 *
 * Inspect `code` for stable program flow and `issues` for field-level validation feedback. The
 * raw `RiduClient.request` escape hatch returns its `Response` unchanged and does not throw this
 * error for non-success status codes.
 */
export class RiduError extends Error {
	/** Stable machine-readable category supplied by the Ridu error envelope. */
	readonly code: ErrorCode;
	/** HTTP response status associated with the failure. */
	readonly status: number;
	/** Server request identifier, when the response supplied one. */
	readonly requestId: string | undefined;
	/** Path-aware validation or operation issues associated with the failure. */
	readonly issues: readonly ValidationIssue[];
	/** Additional error context supplied by the server. */
	readonly details: unknown;

	/** Create a structured SDK error from a normalized Ridu error payload. */
	constructor(payload: ErrorPayload) {
		super(payload.message);
		this.name = "RiduError";
		this.code = payload.code;
		this.status = payload.status;
		this.requestId = payload.requestId;
		this.issues = [...payload.issues];
		this.details = payload.details;
	}

	/** Convert a non-success response into a Ridu error, including a status-based fallback. */
	static async fromResponse(response: Response): Promise<RiduError> {
		const body = await readJSON(response);
		if (isErrorEnvelope(body)) {
			return new RiduError(errorPayload(body));
		}
		return new RiduError({
			code: fallbackCode(response.status),
			status: response.status,
			message: response.statusText
				? `Request failed: ${response.status} ${response.statusText}`
				: `Request failed with status ${response.status}`,
			issues: [],
		});
	}
}

async function readJSON(response: Response) {
	try {
		return await response.json();
	} catch {
		return undefined;
	}
}

function fallbackCode(status: number) {
	switch (status) {
		case 400:
			return "bad_request";
		case 401:
		case 403:
			return "access_denied";
		case 404:
			return "not_found";
		case 409:
			return "conflict";
		case 422:
			return "validation";
		default:
			return "internal";
	}
}
