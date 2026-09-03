import {
	errorPayload,
	isErrorEnvelope,
	type ErrorCode,
	type ErrorPayload,
	type ValidationIssue,
} from "@riducms/protocol";

export class RiduError extends Error {
	readonly code: ErrorCode;
	readonly status: number;
	readonly requestId: string | undefined;
	readonly issues: readonly ValidationIssue[];
	readonly details: unknown;

	constructor(payload: ErrorPayload) {
		super(payload.message);
		this.name = "RiduError";
		this.code = payload.code;
		this.status = payload.status;
		this.requestId = payload.requestId;
		this.issues = [...payload.issues];
		this.details = payload.details;
	}

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
