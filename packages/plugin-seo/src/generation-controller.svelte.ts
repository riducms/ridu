import type { FieldAuthoringHost, FieldDocumentForm } from "@riducms/plugin";

import { generationScopeToken, generationSnapshotToken } from "@plugin-seo/generation-snapshot";

interface GenerationResponse {
	result: string;
}

export class GenerationController {
	status = $state<"idle" | "pending" | "error">("idle");
	#generation = 0;
	#request: AbortController | undefined;

	async run(
		authoring: FieldAuthoringHost | undefined,
		form: FieldDocumentForm,
		path: string
	): Promise<string | undefined> {
		const requestPlugin = authoring?.requestPlugin;
		const snapshot = form.snapshot;
		const resource = form.resource;
		if (requestPlugin === undefined || snapshot === undefined || resource === undefined) {
			this.status = "error";
			return undefined;
		}
		this.#request?.abort();
		const request = new AbortController();
		this.#request = request;
		const generation = ++this.#generation;
		this.status = "pending";
		const document = snapshot.call(form);
		const documentRevision = generationSnapshotToken(document);
		const scope = generationScopeToken(resource, form.contentLocale);
		try {
			const response = await requestPlugin<unknown>(
				path,
				{
					...(resource.global
						? { global: resource.collection }
						: { collection: resource.collection }),
					id: resource.id,
					locale: form.contentLocale,
					document,
				},
				request.signal
			);
			if (
				generation !== this.#generation ||
				request.signal.aborted ||
				generationSnapshotToken(snapshot.call(form)) !== documentRevision ||
				generationScopeToken(form.resource, form.contentLocale) !== scope
			) {
				if (generation === this.#generation) this.status = "idle";
				return undefined;
			}
			if (!isGenerationResponse(response))
				throw new Error("SEO generation returned an invalid response");
			this.status = "idle";
			return response.result;
		} catch {
			if (generation !== this.#generation || request.signal.aborted) return undefined;
			this.status = "error";
			return undefined;
		} finally {
			if (this.#request === request) this.#request = undefined;
		}
	}

	cancel() {
		this.#generation += 1;
		this.#request?.abort();
		this.#request = undefined;
		if (this.status === "pending") this.status = "idle";
	}
}

function isGenerationResponse(value: unknown): value is GenerationResponse {
	return (
		typeof value === "object" &&
		value !== null &&
		"result" in value &&
		typeof value.result === "string"
	);
}
