import { createClient } from "@riducms/sdk";

import {
	checkConsent,
	describeError,
	sendDecision,
	type ConsentRequest,
	type PluginRequester,
} from "#lib/consent.js";

export type ConsentState =
	| { kind: "loading" }
	| { kind: "ready"; request: ConsentRequest; submitting: boolean; error?: string }
	| { kind: "refused"; message: string; redirect?: string }
	| { kind: "failed"; message: string }
	| { kind: "redirecting" };

/** The consent page's state: check the client's request, then send the person's decision. */
export class ConsentController {
	state = $state<ConsentState>({ kind: "loading" });
	actAs = $state("");

	readonly #query: string;
	readonly #client: PluginRequester = createClient({ baseURL: window.location.origin });

	constructor(query: string) {
		this.#query = query;
	}

	async load() {
		this.state = { kind: "loading" };
		const check = await checkConsent(this.#client, this.#query);
		if (check.kind === "ready") {
			this.actAs = check.request.actAs[0]!.id;
			this.state = { kind: "ready", request: check.request, submitting: false };
		} else {
			this.state = check;
		}
	}

	async decide(allow: boolean) {
		if (this.state.kind !== "ready" || this.state.submitting) return;
		const ready = this.state;
		this.state = { ...ready, submitting: true };
		try {
			const redirect = await sendDecision(this.#client, this.#query, allow, this.actAs);
			this.return(redirect);
		} catch (error) {
			this.state = { ...ready, submitting: false, error: describeError(error) };
		}
	}

	/** Send the browser back to the app, which reads the code or error from the address. */
	return(redirect: string) {
		this.state = { kind: "redirecting" };
		window.location.assign(redirect);
	}
}
