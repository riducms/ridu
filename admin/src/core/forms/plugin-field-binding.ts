import { FieldBindingLifetime } from "@admin/core/forms/field-binding-lifetime";
import type {
	PluginFieldBinding as Binding,
	PluginForm,
	RegisteredPluginField,
} from "@riducms/plugin";
import type { SchemaField, ValidationIssue } from "@riducms/protocol";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import { captureFieldOccurrence } from "@admin/core/forms/field-occurrence";
import { writePluginField } from "@admin/core/forms/plugin-field-write";
import { cloneFormValue } from "@admin/core/forms/form-schema";

/** The form owns values. This lease owns registration, occurrence tracking and child capabilities. */
export class PluginFieldBinding implements Binding<unknown> {
	#lifetime: FieldBindingLifetime;
	#releasePendingEdit?: () => void;
	readonly form: PluginForm;
	constructor(
		private controller: FormController,
		getSchema: () => SchemaField,
		private registration: Pick<RegisteredPluginField, "decodeValue"> &
			Partial<Pick<RegisteredPluginField, "decodeInput" | "decodeFormValue">>,
		schemaVersion: () => unknown = () => controller.editorEpoch,
		private owner?: PluginFieldBinding
	) {
		this.#lifetime = new FieldBindingLifetime(controller, getSchema, schemaVersion);
		const binding = this;
		this.form = {
			get contentLocale() {
				binding.assertActive();
				return controller.contentLocale;
			},
			get resource() {
				binding.assertActive();
				return cloneFormValue(controller.resource) as typeof controller.resource;
			},
			get: (path) => {
				this.assertActive();
				return cloneFormValue(controller.get(path));
			},
			issuesFor: (path) => {
				this.assertActive();
				return controller.issuesFor(path).map((issue) => ({ ...issue }));
			},
			snapshot: () => {
				this.assertActive();
				return controller.snapshot();
			},
			bind: (path) => {
				this.assertActive();
				const captured = captureFieldOccurrence(controller, path);
				const child = new PluginFieldBinding(
					controller,
					() => captured.schema,
					{ decodeValue: (value) => value },
					schemaVersion,
					this
				);
				this.onDestroy(child.destroy);
				return child;
			},
		};
	}
	get stale() {
		return this.#lifetime.stale;
	}
	get schema() {
		this.assertActive();
		const schema = this.#lifetime.schema;
		return {
			...schema,
			path: this.#lifetime.path,
			admin: { ...schema.admin, readOnly: this.#isReadOnly() },
		};
	}
	get rawValue() {
		this.assertActive();
		return cloneFormValue(this.controller.get(this.#lifetime.path));
	}
	get value() {
		const raw = this.rawValue;
		const value =
			raw === null || raw === undefined
				? raw
				: (this.registration.decodeFormValue ?? this.registration.decodeValue)(raw);
		this.assertActive();
		return copyValue(value);
	}
	get issues() {
		this.assertActive();
		return this.controller.issuesFor(this.#lifetime.path).map((issue) => ({ ...issue }));
	}
	get liveValidation() {
		if (this.stale) return { status: "idle" as const, retry: () => {} };
		const feedback = this.controller.liveValidation.forField(this.#lifetime.path);
		return {
			status: feedback.status,
			retry: () => {
				this.assertActive();
				if (!this.readOnly) this.controller.liveValidation.flush(this.#lifetime.path);
			},
		};
	}

	get readOnly() {
		return this.controller.editingBlocked || this.#isReadOnly();
	}
	#isReadOnly(): boolean {
		return this.#lifetime.schemaReadOnly || (this.owner !== undefined && this.owner.#isReadOnly());
	}
	set = (value: unknown) => {
		this.assertEditable();
		if (value === undefined) throw new Error("Use null to clear a field value.");
		const supplied = copyValue(value);
		const decoded =
			supplied === null
				? null
				: (this.registration.decodeInput ?? this.registration.decodeValue)(supplied);
		// Decoders and getters are application code and may revoke the binding synchronously.
		const detached = copyValue(decoded);
		this.assertEditable();
		const schema = this.#lifetime.schema;
		writePluginField(this.controller, schema, detached);
	};
	reportPendingEdit = (issues: readonly ValidationIssue[]) => {
		this.assertActive();
		this.#releasePendingEdit?.();
		this.#releasePendingEdit = undefined;
		if (issues.length === 0) return;
		const reported = this.#lifetime.path;
		// Keep each path relative to the field so pending input follows its logical row.
		const pending = issues.map((issue) => ({
			issue: { ...issue },
			suffix:
				issue.path === reported || issue.path.startsWith(`${reported}.`)
					? issue.path.slice(reported.length)
					: "",
		}));
		const stop = this.controller.registerPendingEdit(() => {
			if (this.#lifetime.stale) return [];
			const path = this.#lifetime.path;
			return pending.map(({ issue, suffix }) => ({ ...issue, path: path + suffix }));
		});
		// A derived read can discover revocation; the stale report is already empty.
		const unsubscribe = this.#lifetime.onDestroy(() => queueMicrotask(stop));
		this.#releasePendingEdit = () => {
			unsubscribe();
			stop();
		};
	};
	assertActive = () => {
		if (this.stale)
			throw new Error(
				"This plugin field binding is stale. Use the current mounted field occurrence."
			);
	};
	assertEditable = () => {
		this.assertActive();
		if (this.readOnly) throw new Error("This plugin field is read-only.");
	};
	onDestroy = (cleanup: () => void) => {
		this.assertActive();
		return this.#lifetime.onDestroy(cleanup);
	};
	destroy = () => this.#lifetime.destroy();
}

/** Reject non-JSON mutable objects before they can enter the form (including decoder outputs). */
function copyValue(value: unknown, seen = new Set<object>(), depth = 0): unknown {
	if (depth > 100) throw new Error("Plugin value is too deeply nested.");
	if (
		value === null ||
		value === undefined ||
		typeof value === "string" ||
		typeof value === "boolean"
	)
		return value;
	if (typeof value === "number" && Number.isFinite(value)) return value;
	if (
		typeof value !== "object" ||
		seen.has(value) ||
		(!Array.isArray(value) &&
			Object.getPrototypeOf(value) !== Object.prototype &&
			Object.getPrototypeOf(value) !== null)
	)
		throw new Error("Plugin values must be finite, acyclic JSON data.");
	seen.add(value);
	const result = Array.isArray(value)
		? value.map((item) => copyValue(item, seen, depth + 1))
		: Object.fromEntries(
				Object.entries(value).map(([key, item]) => [key, copyValue(item, seen, depth + 1)])
			);
	seen.delete(value);
	return result;
}
