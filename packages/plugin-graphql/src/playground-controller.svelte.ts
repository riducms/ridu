import { isRecord } from "@riducms/protocol";

import {
	executeOperation,
	operationNameAt,
	parseVariables,
	type OperationResponse,
} from "./operation";

export type PlaygroundResult =
	| { kind: "empty" }
	| { kind: "running" }
	| { kind: "response"; response: OperationResponse }
	| { kind: "failed"; message: string };

interface SavedSession {
	query: string;
	variables: string;
}

/**
 * Owns the playground's documents, its one in-flight request, and the docs explorer's trail.
 * Only the latest run may set the result: a superseded or cancelled request is aborted and ignored.
 */
export class PlaygroundController {
	query = "";
	variables = "";
	result: PlaygroundResult = $state.raw({ kind: "empty" });
	variablesInvalid = $state(false);
	/** Types opened in the docs explorer, most recent last; undefined when the explorer is closed. */
	docs: readonly string[] | undefined = $state.raw();

	readonly #endpoint: string;
	readonly #storageKey: string;
	readonly #fetch: typeof fetch | undefined;
	#request: AbortController | undefined;
	#generation = 0;

	constructor(endpoint: string, starter: string, fetchOperation?: typeof fetch) {
		this.#endpoint = endpoint;
		this.#storageKey = `ridu:graphql-playground:${endpoint}`;
		this.#fetch = fetchOperation;
		const saved = this.#read();
		this.query = saved?.query ?? starter;
		this.variables = saved?.variables ?? "";
	}

	get running() {
		return this.result.kind === "running";
	}

	async run(cursor: number) {
		const variables = parseVariables(this.variables);
		if (!variables.ok) {
			this.variablesInvalid = true;
			return;
		}
		this.variablesInvalid = false;
		this.#request?.abort();
		const request = new AbortController();
		const generation = ++this.#generation;
		this.#request = request;
		const query = this.query;
		const operationName = operationNameAt(query, cursor);
		this.result = { kind: "running" };
		this.save();
		try {
			const response = await executeOperation(
				this.#endpoint,
				{
					query,
					...(variables.value === undefined ? {} : { variables: variables.value }),
					...(operationName === undefined ? {} : { operationName }),
				},
				request.signal,
				this.#fetch
			);
			if (generation === this.#generation) this.result = { kind: "response", response };
		} catch (error) {
			if (generation !== this.#generation) return;
			this.result = {
				kind: "failed",
				message: error instanceof Error ? error.message : String(error),
			};
		} finally {
			if (this.#request === request) this.#request = undefined;
		}
	}

	cancel() {
		if (!this.running) return;
		this.#generation++;
		this.#request?.abort();
		this.result = { kind: "empty" };
	}

	variablesEdited() {
		this.variablesInvalid = false;
	}

	openDocs(type?: string) {
		const trail = this.docs ?? [];
		this.docs = type === undefined || trail.at(-1) === type ? trail : [...trail, type];
	}

	docsBack() {
		if (this.docs !== undefined) this.docs = this.docs.slice(0, -1);
	}

	closeDocs() {
		this.docs = undefined;
	}

	/** Keeps the documents for this browser, so returning to the page restores the last session. */
	save() {
		try {
			const session: SavedSession = { query: this.query, variables: this.variables };
			localStorage.setItem(this.#storageKey, JSON.stringify(session));
		} catch {
			// Storage can be unavailable (private windows, blocked site data); the session still works.
		}
	}

	dispose() {
		this.save();
		this.#generation++;
		this.#request?.abort();
	}

	#read(): SavedSession | undefined {
		try {
			const value: unknown = JSON.parse(localStorage.getItem(this.#storageKey) ?? "null");
			if (isRecord(value) && typeof value.query === "string" && typeof value.variables === "string")
				return { query: value.query, variables: value.variables };
		} catch {
			// Treat unreadable storage as an empty session.
		}
		return undefined;
	}
}
