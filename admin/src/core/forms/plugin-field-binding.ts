import { cloneSchemaField } from "@riducms/protocol";
import type {
	PluginFieldBinding as Binding,
	PluginForm,
	RegisteredPluginField,
} from "@riducms/plugin";
import type { SchemaField } from "@riducms/protocol";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import { captureFieldOccurrence } from "@admin/core/forms/field-occurrence";
import { writePluginField } from "@admin/core/forms/plugin-field-write";
import { cloneFormValue } from "@admin/core/forms/form-schema";
import { fieldAccessPath } from "@admin/fields/nested/scoped-field";
import { evaluateFieldCondition } from "@admin/core/forms/field-condition";

/** The form owns values. This lease owns registration, occurrence tracking and child capabilities. */
export class PluginFieldBinding implements Binding<unknown> {
	#stale = false;
	#path: string;
	#resolvePath: () => string | undefined;
	#ancestors: ReturnType<typeof captureFieldOccurrence>["ancestors"];
	#epoch: number;
	#version: unknown;
	#schemaID: string;
	#resource: string;
	#locale: string | undefined;
	#stopPath: () => void;
	#stopObserve: () => void;
	#stopLifetime: () => void;
	#cleanup = new Set<() => void>();
	readonly form: PluginForm;
	constructor(
		private controller: FormController,
		private getSchema: () => SchemaField,
		private registration: Pick<RegisteredPluginField, "decodeValue"> &
			Partial<Pick<RegisteredPluginField, "decodeInput" | "decodeFormValue">>,
		private schemaVersion: () => unknown = () => controller.editorEpoch,
		private owner?: PluginFieldBinding
	) {
		const schema = getSchema();
		this.#path = schema.path;
		const occurrence = captureFieldOccurrence(controller, schema.path);
		this.#resolvePath = occurrence.resolve;
		this.#ancestors = occurrence.ancestors;
		this.#epoch = controller.editorEpoch;
		this.#schemaID = schema.id;
		this.#version = schemaVersion();
		this.#resource = JSON.stringify(controller.resource);
		this.#locale = controller.contentLocale;
		this.#stopPath = controller.register(this.#path);
		this.#stopObserve = controller.observe(this.#path.split(".")[0]!, () => this.#resolve());
		this.#stopLifetime = controller.registerEditorLifetime(this.destroy);
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
		this.#resolve();
		return this.#stale;
	}
	get schema() {
		this.assertActive();
		const schema = cloneSchemaField(this.getSchema());
		return {
			...schema,
			path: this.#path,
			admin: { ...schema.admin, readOnly: this.#isReadOnly() },
		};
	}
	get rawValue() {
		this.assertActive();
		return cloneFormValue(this.controller.get(this.#path));
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
		return this.controller.issuesFor(this.#path).map((issue) => ({ ...issue }));
	}
	get liveValidation() {
		if (this.stale) return { status: "idle" as const, retry: () => {} };
		const feedback = this.controller.liveValidation.forField(this.#path);
		return {
			status: feedback.status,
			retry: () => {
				this.assertActive();
				if (!this.readOnly) this.controller.liveValidation.flush(this.#path);
			},
		};
	}

	get readOnly() {
		return this.controller.editingBlocked || this.#isReadOnly();
	}
	#isReadOnly() {
		if (this.stale || (this.owner !== undefined && this.owner.#isReadOnly())) return true;
		if (
			this.#ancestors.some(({ schema, resolve }) => {
				const path = resolve();
				return (
					path === undefined ||
					schema.admin.readOnly === true ||
					schema.admin.hidden === true ||
					!this.controller.canRead(path, fieldAccessPath(schema)) ||
					!this.controller.canWrite(path, fieldAccessPath(schema)) ||
					(schema.admin.condition !== undefined &&
						!evaluateFieldCondition(schema.admin.condition, path, (other) =>
							this.controller.get(other)
						))
				);
			})
		)
			return true;
		const field = this.getSchema();
		return (
			field.admin.readOnly === true ||
			field.admin.hidden === true ||
			!this.controller.canRead(this.#path, fieldAccessPath(field)) ||
			!this.controller.canWrite(this.#path, fieldAccessPath(field)) ||
			(field.admin.condition !== undefined &&
				!evaluateFieldCondition(field.admin.condition, this.#path, (path) =>
					this.controller.get(path)
				))
		);
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
		const schema = { ...this.getSchema(), path: this.#path };
		writePluginField(this.controller, schema, detached);
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
		this.#cleanup.add(cleanup);
		return () => {
			this.#cleanup.delete(cleanup);
		};
	};
	destroy = () => {
		if (this.#stale) return;
		this.#stale = true;
		this.#stopPath();
		this.#stopObserve();
		this.#stopLifetime();
		for (const cleanup of [...this.#cleanup]) cleanup();
		this.#cleanup.clear();
	};
	#resolve() {
		if (this.#stale) return;
		if (
			!this.controller.editorScopeActive ||
			this.controller.editorEpoch !== this.#epoch ||
			this.schemaVersion() !== this.#version ||
			this.getSchema().id !== this.#schemaID ||
			this.controller.contentLocale !== this.#locale ||
			JSON.stringify(this.controller.resource) !== this.#resource
		) {
			this.destroy();
			return;
		}
		const path = this.#resolvePath();
		if (path === undefined) {
			this.destroy();
			return;
		}
		if (path !== this.#path) {
			this.#stopPath();
			this.#path = path;
			this.#stopPath = this.controller.register(path);
		}
	}
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
