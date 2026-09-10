import { cloneSchemaField } from "@riducms/protocol";
import type {
	FieldBinding,
	FieldEditorProps,
	FieldEditorType,
	FieldEditorValue,
} from "@riducms/plugin/editor";
import type { SchemaField } from "@riducms/protocol";
import { fieldControlARIA } from "@riducms/ui";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import { fieldAccessPath } from "@admin/fields/nested/scoped-field";
import { cloneFormValue } from "@admin/core/forms/form-schema";
import { evaluateFieldCondition } from "@admin/core/forms/field-condition";
import { captureFieldOccurrence, type FieldOccurrence } from "@admin/core/forms/field-occurrence";

/** The host owns this capability until unmount or an invalidating form transition. */
export class FieldEditorBinding<Type extends FieldEditorType> implements FieldBinding<Type> {
	#form: FormController;
	#schema: () => SchemaField;
	#occurrence: FieldOccurrence;
	#path: string;
	#stale = false;
	#stopPath: () => void;
	#stopObserve: () => void;
	#stopLifetime: () => void;
	#epoch: number;
	#resource: string;
	#locale: string | undefined;
	#type: Type;
	#schemaID: string;
	#schemaVersion: () => unknown;
	#initialSchemaVersion: unknown;
	readonly form: FieldEditorProps["form"];

	constructor(
		form: FormController,
		schema: () => SchemaField,
		type: Type,
		schemaVersion: () => unknown = () => form.editorEpoch
	) {
		this.#form = form;
		this.#schemaID = schema().id;
		this.#schemaVersion = schemaVersion;
		this.#initialSchemaVersion = schemaVersion();
		this.#schema = schema;
		this.#type = type;
		this.#epoch = form.editorEpoch;
		this.#resource = JSON.stringify(form.resource);
		this.#locale = form.contentLocale;
		this.#path = schema().path;
		this.#occurrence = captureFieldOccurrence(form, this.#path);
		this.#stopPath = form.register(this.#path);
		this.#stopObserve = form.observe(this.#path.split(".")[0]!, () => this.#resolve());
		this.#stopLifetime = form.registerEditorLifetime(this.destroy);
		const binding = this;
		this.form = {
			get contentLocale() {
				binding.assertActive();
				return binding.#locale;
			},
			get resource() {
				binding.assertActive();
				return form.resource === undefined ? undefined : { ...form.resource };
			},
			get: (path) => {
				this.assertActive();
				return cloneFormValue(form.get(path));
			},
			issuesFor: (path) => {
				this.assertActive();
				return form.issuesFor(path).map((issue) => ({ ...issue }));
			},
			snapshot: () => {
				this.assertActive();
				return form.snapshot();
			},
		};
	}

	get schema(): SchemaField & { type: Type } {
		// A revoked host can render once before its keyed subtree unmounts. Metadata
		// remains a detached snapshot; values and writes stay revoked immediately.
		this.#resolve();
		return {
			...cloneSchemaField(this.#schema()),
			type: this.#type,
			path: this.#path,
		};
	}
	get stale() {
		this.#resolve();
		return this.#stale;
	}
	get value(): FieldEditorValue<Type> | null | undefined {
		if (this.stale) return undefined;
		const value = this.#form.get(this.#path);
		assertEditorValue(this.#type, value);
		return cloneFormValue(value) as FieldEditorValue<Type> | null | undefined;
	}
	get issues() {
		return this.stale ? [] : this.#form.issuesFor(this.#path).map((issue) => ({ ...issue }));
	}
	get liveValidation() {
		if (this.stale) return { status: "idle" as const, retry: () => {} };
		const feedback = this.#form.liveValidation.forField(this.#path);
		return {
			status: feedback.status,
			retry: () => {
				this.assertActive();
				if (!this.readOnly) this.#form.liveValidation.flush(this.#path);
			},
		};
	}

	get readOnly() {
		if (this.stale) return true;
		if (
			this.#occurrence.ancestors.some(({ schema, resolve }) => {
				const path = resolve();
				return (
					path === undefined ||
					schema.admin.readOnly === true ||
					schema.admin.hidden === true ||
					!this.#form.canRead(path, fieldAccessPath(schema)) ||
					!this.#form.canWrite(path, fieldAccessPath(schema)) ||
					(schema.admin.condition !== undefined &&
						!evaluateFieldCondition(schema.admin.condition, path, (other) => this.#form.get(other)))
				);
			})
		)
			return true;
		const schema = this.#schema();
		const accessPath = fieldAccessPath(schema);
		return (
			this.#form.editingBlocked ||
			schema.admin.readOnly === true ||
			schema.admin.hidden === true ||
			!this.#form.canRead(this.#path, accessPath) ||
			!this.#form.canWrite(this.#path, accessPath) ||
			(schema.admin.condition !== undefined &&
				!evaluateFieldCondition(schema.admin.condition, this.#path, (path) => this.#form.get(path)))
		);
	}
	get inputProps() {
		const schema = this.schema;
		const invalid = this.issues.length > 0;
		return {
			id: schema.id,
			name: schema.path,
			// Native required checks one input. Boolean false and list items containing empty
			// strings can satisfy their Ridu field; their container rule runs on submit.
			required:
				this.#type !== "checkbox" &&
				this.#type !== "text-list" &&
				this.#type !== "number-list" &&
				schema.required &&
				(!schema.dynamicDefault || this.value !== undefined),
			...fieldControlARIA(schema.id, schema.admin.description !== undefined, invalid),
		};
	}
	set = (value: FieldEditorValue<Type> | null) => {
		this.#write(value);
	};
	assertActive = () => {
		if (this.stale)
			throw new Error(
				"This field editor is stale. Use the editor mounted for the current document, locale and field occurrence."
			);
	};
	destroy = () => {
		if (this.#stale) return;
		this.#stale = true;
		this.#stopPath();
		this.#stopObserve();
		this.#stopLifetime();
	};
	#write(value: unknown) {
		this.assertActive();
		if (this.readOnly) throw new Error(`Field ${this.#path} is read-only.`);
		assertEditorValue(this.#type, value);
		if (value === undefined) throw new Error("Use null to clear an editor value.");
		this.#form.set(this.#path, cloneFormValue(value));
	}
	#resolve() {
		if (this.#stale) return;
		if (
			this.#schema().id !== this.#schemaID ||
			this.#schemaVersion() !== this.#initialSchemaVersion ||
			!this.#form.editorScopeActive ||
			this.#form.editorEpoch !== this.#epoch ||
			this.#form.contentLocale !== this.#locale ||
			JSON.stringify(this.#form.resource) !== this.#resource
		) {
			this.destroy();
			return;
		}
		const path = this.#occurrence.resolve();
		if (path === undefined) {
			this.destroy();
			return;
		}
		if (path !== this.#path) {
			this.#stopPath();
			this.#path = path;
			this.#stopPath = this.#form.register(path);
		}
	}
}

function assertEditorValue(type: FieldEditorType, value: unknown) {
	if (value === null || value === undefined) return;
	if (type === "text-list" || type === "number-list") {
		const element = type === "text-list" ? "string" : "number";
		if (
			!Array.isArray(value) ||
			!Array.from(value).every(
				(item) => typeof item === element && (element !== "number" || Number.isFinite(item))
			)
		)
			throw new Error(
				`Editor for ${type} expected an array of ${element}s with no null or invalid items.`
			);
		return;
	}
	const expected = type === "number" ? "number" : type === "checkbox" ? "boolean" : "string";
	if (typeof value !== expected || (typeof value === "number" && !Number.isFinite(value)))
		throw new Error(`Editor for ${type} expected a ${expected} value or null.`);
}
