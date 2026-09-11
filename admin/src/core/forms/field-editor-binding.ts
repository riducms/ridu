import { FieldBindingLifetime } from "@admin/core/forms/field-binding-lifetime";
import type {
	FieldBinding,
	FieldEditorProps,
	FieldEditorType,
	FieldEditorValue,
} from "@riducms/plugin/editor";
import type { SchemaField } from "@riducms/protocol";
import { fieldControlARIA } from "@riducms/ui";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import { cloneFormValue } from "@admin/core/forms/form-schema";

/** The host owns this capability until unmount or an invalidating form transition. */
export class FieldEditorBinding<Type extends FieldEditorType> implements FieldBinding<Type> {
	#form: FormController;
	#lifetime: FieldBindingLifetime;
	#type: Type;
	#apply: ((value: FieldEditorValue<Type> | null) => void) | undefined;
	readonly form: FieldEditorProps["form"];

	constructor(
		form: FormController,
		schema: () => SchemaField,
		type: Type,
		schemaVersion: () => unknown = () => form.editorEpoch,
		apply?: (value: FieldEditorValue<Type> | null) => void
	) {
		this.#form = form;
		this.#apply = apply;
		this.#type = type;
		this.#lifetime = new FieldBindingLifetime(form, schema, schemaVersion);
		const binding = this;
		this.form = {
			get contentLocale() {
				binding.assertActive();
				return form.contentLocale;
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
		return { ...this.#lifetime.schema, type: this.#type };
	}
	get stale() {
		return this.#lifetime.stale;
	}
	get value(): FieldEditorValue<Type> | null | undefined {
		if (this.stale) return undefined;
		const value = this.#form.get(this.#lifetime.path);
		assertEditorValue(this.#type, value);
		return cloneFormValue(value) as FieldEditorValue<Type> | null | undefined;
	}
	get issues() {
		return this.stale
			? []
			: this.#form.issuesFor(this.#lifetime.path).map((issue) => ({ ...issue }));
	}
	get liveValidation() {
		if (this.stale) return { status: "idle" as const, retry: () => {} };
		const feedback = this.#form.liveValidation.forField(this.#lifetime.path);
		return {
			status: feedback.status,
			retry: () => {
				this.assertActive();
				if (!this.readOnly) this.#form.liveValidation.flush(this.#lifetime.path);
			},
		};
	}

	get visible() {
		return this.#lifetime.visible;
	}
	get readOnly() {
		return this.#lifetime.readOnly;
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
	destroy = () => this.#lifetime.destroy();
	#write(value: unknown) {
		this.assertActive();
		if (this.readOnly) throw new Error(`Field ${this.#lifetime.path} is read-only.`);
		assertEditorValue(this.#type, value);
		if (value === undefined) throw new Error("Use null to clear an editor value.");
		const detached = cloneFormValue(value) as FieldEditorValue<Type> | null;
		this.assertActive();
		if (this.readOnly) throw new Error(`Field ${this.#lifetime.path} is read-only.`);
		if (this.#apply !== undefined) this.#apply(detached);
		else this.#form.set(this.#lifetime.path, detached);
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
