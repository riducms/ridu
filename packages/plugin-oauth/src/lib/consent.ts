import { isRecord } from "@riducms/protocol";

/** A user the app may act as, as the Go plugin offers it. The approver comes first. */
export interface ActAsChoice {
	id: string;
	label: string;
}

/** An authorization request the person may approve. */
export interface ConsentRequest {
	client: { id: string; name: string };
	/** Host of the address the browser returns to after the decision. */
	redirectHost: string;
	/** Whether that address is on this computer, as for a native app such as Claude Code. */
	loopback: boolean;
	actAs: ActAsChoice[];
}

/** A request that can't go on. With a redirect, the app should hear why too. */
export interface ConsentRefusal {
	error: string;
	errorDescription: string;
	redirect: string;
}

function isChoice(value: unknown): value is ActAsChoice {
	return isRecord(value) && typeof value.id === "string" && typeof value.label === "string";
}

/** Decode the `authorization` endpoint's answer, which comes from the network. */
export function decodeAuthorization(value: unknown): ConsentRequest | ConsentRefusal {
	if (!isRecord(value)) throw new Error("the authorization check returned no object");
	if (typeof value.error === "string") {
		if (typeof value.errorDescription !== "string" || typeof value.redirect !== "string")
			throw new Error("the authorization check returned an incomplete refusal");
		return {
			error: value.error,
			errorDescription: value.errorDescription,
			redirect: value.redirect,
		};
	}
	const client = value.client;
	if (
		!isRecord(client) ||
		typeof client.id !== "string" ||
		typeof client.name !== "string" ||
		typeof value.redirectHost !== "string" ||
		typeof value.loopback !== "boolean" ||
		!Array.isArray(value.actAs) ||
		value.actAs.length === 0 ||
		!value.actAs.every(isChoice)
	)
		throw new Error("the authorization check returned an unexpected request");
	return {
		client: { id: client.id, name: client.name },
		redirectHost: value.redirectHost,
		loopback: value.loopback,
		actAs: value.actAs.map(({ id, label }) => ({ id, label })),
	};
}

/** Decode the `decision` endpoint's answer: where to send the browser back to the app. */
export function decodeRedirect(value: unknown): string {
	if (!isRecord(value) || typeof value.redirect !== "string" || !isWebURL(value.redirect))
		throw new Error("the decision returned no address to return to");
	return value.redirect;
}

function isWebURL(value: string) {
	try {
		const url = new URL(value);
		return url.protocol === "https:" || url.protocol === "http:";
	} catch {
		return false;
	}
}

export function isRefusal(value: ConsentRequest | ConsentRefusal): value is ConsentRefusal {
	return "error" in value;
}

/** The part of the SDK client the consent page uses. */
export interface PluginRequester {
	requestPlugin<Result = unknown>(plugin: string, path: string, body: unknown): Promise<Result>;
}

/** What checking the client's request shows the person. */
export type ConsentCheck =
	| { kind: "ready"; request: ConsentRequest }
	| { kind: "refused"; message: string; redirect?: string }
	| { kind: "failed"; message: string };

/** Ask the Go plugin whether the request the browser arrived with can be approved. */
export async function checkConsent(client: PluginRequester, query: string): Promise<ConsentCheck> {
	try {
		const answer = decodeAuthorization(
			await client.requestPlugin("oauth", "authorization", { query })
		);
		if (isRefusal(answer))
			return { kind: "refused", message: answer.errorDescription, redirect: answer.redirect };
		return { kind: "ready", request: answer };
	} catch (error) {
		// A bad request that only the person may hear about arrives as a 400 error envelope.
		if (error instanceof Error && "status" in error && error.status === 400)
			return { kind: "refused", message: error.message };
		return { kind: "failed", message: describeError(error) };
	}
}

/** Send the person's decision and return the address that takes the browser back to the app. */
export async function sendDecision(
	client: PluginRequester,
	query: string,
	allow: boolean,
	actAs: string
): Promise<string> {
	return decodeRedirect(await client.requestPlugin("oauth", "decision", { query, allow, actAs }));
}

export function describeError(error: unknown) {
	return error instanceof Error ? error.message : String(error);
}
