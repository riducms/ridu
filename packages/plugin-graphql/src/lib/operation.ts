import { isRecord } from "@riducms/protocol";
import { Kind, parse, type OperationDefinitionNode } from "graphql";

export interface OperationRequest {
	query: string;
	variables?: Record<string, unknown>;
	operationName?: string;
}

export interface OperationResponse {
	status: number;
	durationMs: number;
	/** The response body, indented when it is JSON. */
	body: string;
}

export type VariablesResult = { ok: true; value?: Record<string, unknown> } | { ok: false };

/** Variables are optional; when present they must be one JSON object. */
export function parseVariables(text: string): VariablesResult {
	if (text.trim() === "") return { ok: true };
	try {
		const value: unknown = JSON.parse(text);
		if (isRecord(value)) return { ok: true, value };
	} catch {
		// Reported below as invalid variables.
	}
	return { ok: false };
}

/**
 * GraphQL needs an operation name when a document holds several operations. Pick the operation
 * under the cursor, as GraphiQL does. A document that does not parse is sent unchanged so the
 * server reports the syntax error in its usual response shape.
 */
export function operationNameAt(query: string, offset: number): string | undefined {
	let operations: OperationDefinitionNode[];
	try {
		operations = parse(query).definitions.filter(
			(definition): definition is OperationDefinitionNode =>
				definition.kind === Kind.OPERATION_DEFINITION
		);
	} catch {
		return undefined;
	}
	let selected = operations[0];
	for (const operation of operations) {
		if (operation.loc === undefined) continue;
		if (offset >= operation.loc.start && offset < operation.loc.end) {
			selected = operation;
			break;
		}
		if (offset >= operation.loc.end) selected = operation;
	}
	return operations.length > 1 ? selected?.name?.value : undefined;
}

/**
 * Send one operation with the current admin credentials. The admin is served by the same Go binary
 * as the API, so same-origin credentials authenticate the request when present.
 */
export async function executeOperation(
	endpoint: string,
	request: OperationRequest,
	signal: AbortSignal,
	fetchOperation: typeof fetch = fetch
): Promise<OperationResponse> {
	const started = performance.now();
	const response = await fetchOperation(endpoint, {
		method: "POST",
		credentials: "same-origin",
		headers: { accept: "application/json", "content-type": "application/json" },
		body: JSON.stringify(request),
		signal,
	});
	const text = await response.text();
	return {
		status: response.status,
		durationMs: Math.round(performance.now() - started),
		body: indentJSON(text),
	};
}

function indentJSON(text: string) {
	try {
		return JSON.stringify(JSON.parse(text), null, 2);
	} catch {
		return text;
	}
}
