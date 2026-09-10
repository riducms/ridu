import type { AdminClient } from "@admin/core/api/admin-client";
import type {
	LiveValidationEnvelope,
	LiveValidationRequest,
	ValidationIssue,
} from "@riducms/protocol";
import type { FieldLiveValidation } from "@riducms/plugin";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import { indexFieldValues, correlateFormIssues } from "@admin/core/forms/form-issue-correlation";
import { captureFieldOccurrence } from "@admin/core/forms/field-occurrence";

export type LiveValidationTransport = (
	input: LiveValidationRequest,
	signal: AbortSignal
) => Promise<LiveValidationEnvelope>;

type Feedback = {
	path: string;
	status: FieldLiveValidation["status"];
	issues: ValidationIssue[];
};

/** One form owns advisory requests; save issues never enter this state. */
export class LiveValidationController {
	#feedback = $state.raw<ReadonlyMap<string, Feedback>>(new Map());
	#active = new Set<string>();
	#unavailableInputs = new Map<
		symbol,
		{ resolve: () => string | undefined; value: string | undefined }
	>();
	#generation = 0;
	#request?: AbortController;
	#timer?: ReturnType<typeof setTimeout>;
	#deadline?: ReturnType<typeof setTimeout>;
	#transport?: LiveValidationTransport;

	constructor(private form: FormController) {}

	configure(transport: LiveValidationTransport) {
		this.reset();
		this.#transport = transport;
	}

	request(input: LiveValidationRequest, signal: AbortSignal) {
		if (!this.#transport) throw new Error("Live validation is unavailable in this form.");
		return this.#transport(input, signal);
	}

	get available() {
		return this.#transport !== undefined;
	}
	get inputAvailable(): boolean {
		return (
			this.#unavailableInputs.size === 0 &&
			this.form.enclosingLiveInputAvailable &&
			serializableInput(this.form.values) &&
			indexFieldValues(this.form.schemaFields, this.form.values).every(({ schema, value }) => {
				if (schema?.type !== "text-list" && schema?.type !== "number-list") return true;
				if (value === null || value === undefined) return true;
				// Numeric list controls retain unfinished text in the form itself. No
				// activated check may mistake that buffer for a usable typed Root value.
				return (
					Array.isArray(value) &&
					Array.from(value).every((item) =>
						schema.type === "text-list"
							? typeof item === "string"
							: typeof item === "number" && Number.isFinite(item)
					)
				);
			})
		);
	}

	/** A control's unfinished buffer is not its last successfully decoded value. */
	inputUnavailable(path: string) {
		let resolve: () => string | undefined;
		try {
			resolve = captureFieldOccurrence(this.form, path).resolve;
		} catch {
			return () => false;
		}
		const lease = Symbol("unfinished-input");
		this.#unavailableInputs.set(lease, { resolve, value: JSON.stringify(this.form.get(path)) });
		this.#schedule(path);
		return () => this.#unavailableInputs.delete(lease);
	}

	get issues() {
		return [...this.#feedback.values()].flatMap((state) => state.issues);
	}

	forField(path: string): FieldLiveValidation {
		const status =
			[...this.#feedback.values()].find((state) => state.path === path)?.status ?? "idle";
		return { status, retry: () => this.flush(path) };
	}

	/** Any edit invalidates activated checks: their Go code can read any sibling/root value. */
	changed(path?: string) {
		for (const [lease, unavailable] of this.#unavailableInputs) {
			const current = unavailable.resolve();
			// An ancestor write can merely reorder a still-mounted unfinished control.
			// Release only a removed occurrence or a real replacement of its value.
			if (
				current === undefined ||
				path === current ||
				JSON.stringify(this.form.get(current)) !== unavailable.value
			)
				this.#unavailableInputs.delete(lease);
		}
		this.#schedule(path);
	}

	#schedule(path?: string) {
		if (!this.available || this.form.submitting || !this.form.editorScopeActive) return;
		this.#cancel();
		const locations = this.#locations();
		const current = new Set(locations.map((location) => location.token));
		this.#active = new Set([...this.#active].filter((token) => current.has(token)));
		if (path !== undefined)
			for (const location of locations)
				if (related(path, location.path)) this.#active.add(location.token);
		const status = this.inputAvailable ? "pending" : "skipped";
		this.#feedback = new Map(
			locations
				.filter(({ token }) => this.#active.has(token))
				.map(({ token, path }) => [token, { path, status, issues: [] }])
		);
		if (this.#active.size && status === "pending") this.#timer = setTimeout(() => this.#run(), 500);
	}

	flush(path: string) {
		if (
			[...this.#feedback.values()].some(
				(state) =>
					(state.status === "pending" || state.status === "failed") && related(state.path, path)
			)
		)
			this.#run();
	}

	/** Saving cancels advisory work but retains activation for later user edits. */
	suspend() {
		this.#cancel();
		this.#feedback = new Map();
	}

	reset() {
		this.suspend();
		this.#active.clear();
		this.#unavailableInputs.clear();
	}

	/** Binding revocation can be discovered during a derived read. Abort synchronously. */
	dispose() {
		this.#cancel();
		this.#active.clear();
		const generation = this.#generation;
		queueMicrotask(() => {
			if (generation === this.#generation) this.#feedback = new Map();
		});
	}

	#cancel() {
		this.#generation += 1;
		this.#request?.abort();
		this.#request = undefined;
		if (this.#timer !== undefined) clearTimeout(this.#timer);
		this.#timer = undefined;
		if (this.#deadline !== undefined) clearTimeout(this.#deadline);
		this.#deadline = undefined;
	}

	#locations() {
		return indexFieldValues(this.form.schemaFields, this.form.values).filter(
			({ schema, path }) =>
				schema?.liveValidation &&
				!schema.admin.readOnly &&
				this.form.includesLiveValidation(path, schema.path)
		);
	}

	async #run() {
		if (
			!this.available ||
			this.form.submitting ||
			!this.form.editorScopeActive ||
			!this.inputAvailable
		)
			return;
		this.#cancel();
		const generation = this.#generation;
		const request = new AbortController();
		this.#request = request;
		const locations = this.#locations().filter(({ token }) => this.#active.has(token));
		if (!locations.length) return;
		this.#feedback = new Map(
			locations.map(({ token, path }) => [token, { path, status: "pending", issues: [] }])
		);
		const current = () =>
			generation === this.#generation &&
			!request.signal.aborted &&
			!this.form.submitting &&
			this.form.editorScopeActive;
		try {
			const values = this.form.snapshot();
			let data: LiveValidationRequest["data"];
			try {
				data = this.form.liveValidationData();
			} catch {
				// A malformed plugin envelope has no usable submitted representation yet.
				this.#feedback = new Map(
					[...this.#feedback].map(([token, state]) => [
						token,
						{ ...state, status: "skipped", issues: [] },
					])
				);
				return;
			}
			// Keep every request within the server's bounded field count, including large forms.
			for (let offset = 0; offset < locations.length; offset += 64) {
				const batch = locations.slice(offset, offset + 64);
				// Leave room above the server's five-second evaluation bound for transport.
				this.#deadline = setTimeout(() => {
					if (!current()) return;
					this.#cancel();
					this.#failPending();
				}, 8_000);
				const result = await this.request(
					{
						...(this.form.resource?.id === undefined ? {} : { id: this.form.resource.id }),
						data,
						fields: batch.map(({ path }) => path),
					},
					request.signal
				);
				if (!current()) return;
				if (this.#deadline !== undefined) clearTimeout(this.#deadline);
				this.#deadline = undefined;
				const feedback = new Map(this.#feedback);
				for (const location of batch) {
					const evaluation = result.evaluations.find((item) => item.path === location.path);
					// A missing evaluation cannot silently become a successful check.
					feedback.set(location.token, {
						path: location.path,
						status: evaluation?.status ?? "failed",
						issues:
							evaluation === undefined
								? []
								: correlateFormIssues(
										this.form.schemaFields,
										values,
										this.form.values,
										evaluation.issues
									),
					});
				}
				this.#feedback = feedback;
			}
		} catch {
			if (!current()) return;
			this.#failPending();
		} finally {
			if (this.#request === request) {
				this.#request = undefined;
				if (this.#deadline !== undefined) clearTimeout(this.#deadline);
				this.#deadline = undefined;
			}
		}
	}

	#failPending() {
		this.#feedback = new Map(
			[...this.#feedback].map(([token, state]) => [
				token,
				state.status === "pending" ? { ...state, status: "failed", issues: [] } : state,
			])
		);
	}
}

function related(left: string, right: string) {
	return left === right || left.startsWith(`${right}.`) || right.startsWith(`${left}.`);
}

/** JSON would coerce NaN/Infinity to null, incorrectly making malformed input look empty. */
function serializableInput(value: unknown, seen = new Set<object>(), depth = 0): boolean {
	if (depth > 100) return false;
	if (typeof value === "number") return Number.isFinite(value);
	if (
		value === null ||
		value === undefined ||
		typeof value === "string" ||
		typeof value === "boolean"
	)
		return true;
	if (typeof value !== "object" || seen.has(value)) return false;
	seen.add(value);
	const available = Object.values(value).every((child) =>
		serializableInput(child, seen, depth + 1)
	);
	seen.delete(value);
	return available;
}

/** Connect a document form to the same authenticated SDK used for its writes. */
export function connectDocumentLiveValidation(form: FormController, client: AdminClient) {
	form.configureLiveValidation((input, signal) => {
		const resource = form.resource;
		if (!resource) throw new Error("No document is open for live validation.");
		const options = { signal, locale: form.contentLocale };
		const { id: _id, ...globalInput } = input;
		return resource.global
			? client.globalLiveValidation(resource.collection, globalInput, options)
			: client.collectionLiveValidation(resource.collection, input, options);
	});
}
