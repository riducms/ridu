import { cloneSchemaField, type SchemaField } from "@riducms/protocol";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import { captureFieldOccurrence, type FieldOccurrence } from "@admin/core/forms/field-occurrence";
import { evaluateFieldCondition } from "@admin/core/forms/field-condition";
import { fieldAccessPath } from "@admin/fields/nested/scoped-field";

/** Private owner shared by local and paired adapters. Revocation is synchronous and permanent. */
export class FieldBindingLifetime {
	#revoked = false;
	#source: SchemaField;
	#snapshot: SchemaField;
	#path: string;
	#occurrence: FieldOccurrence;
	#epoch: number;
	#version: unknown;
	#resource: string;
	#locale: string | undefined;
	#stopPath: () => void;
	#stopObserve: () => void;
	#stopLifetime: () => void;
	#cleanup = new Set<() => void>();

	constructor(
		private form: FormController,
		private getSchema: () => SchemaField,
		private schemaVersion: () => unknown = () => form.editorEpoch
	) {
		this.#source = getSchema();
		this.#snapshot = cloneSchemaField(this.#source);
		this.#path = this.#source.path;
		this.#occurrence = captureFieldOccurrence(form, this.#path);
		this.#epoch = form.editorEpoch;
		this.#version = schemaVersion();
		this.#resource = JSON.stringify(form.resource);
		this.#locale = form.contentLocale;
		this.#stopPath = form.register(this.#path);
		this.#stopObserve = form.observe(this.#path.split(".")[0]!, () => this.#resolve());
		this.#stopLifetime = form.registerEditorLifetime(this.destroy);
	}

	get stale() {
		this.#resolve();
		return this.#revoked;
	}
	get path() {
		this.#resolve();
		return this.#path;
	}
	/** A revoked local host can render cached metadata once before its subtree unmounts. */
	get schema(): SchemaField {
		this.#resolve();
		return { ...cloneSchemaField(this.#revoked ? this.#snapshot : this.#source), path: this.#path };
	}
	get visible() {
		if (this.stale) return false;
		return this.#selections().every(({ schema, resolve }) => {
			const path = resolve();
			return path !== undefined && this.#visible(schema, path);
		});
	}
	/** Permission/schema restrictions exclude the temporary pending-save lock. */
	get schemaReadOnly() {
		if (this.stale) return true;
		return this.#selections().some(({ schema, resolve }) => {
			const path = resolve();
			return (
				path === undefined ||
				schema.admin.readOnly === true ||
				!this.#visible(schema, path) ||
				!this.form.canWrite(path, fieldAccessPath(schema))
			);
		});
	}
	get readOnly() {
		return this.schemaReadOnly || this.form.editingBlocked;
	}
	#selections() {
		return [...this.#occurrence.ancestors, { schema: this.#source, resolve: () => this.#path }];
	}
	#visible(schema: SchemaField, path: string) {
		return (
			schema.admin.hidden !== true &&
			this.form.canRead(path, fieldAccessPath(schema)) &&
			(schema.admin.condition === undefined ||
				evaluateFieldCondition(schema.admin.condition, path, (other) => this.form.get(other)))
		);
	}
	onDestroy(cleanup: () => void) {
		if (this.stale) {
			cleanup();
			return () => {};
		}
		this.#cleanup.add(cleanup);
		return () => {
			this.#cleanup.delete(cleanup);
		};
	}
	destroy = () => {
		if (this.#revoked) return;
		this.#revoked = true;
		// Never call component-owned getters during or after revocation.
		this.#snapshot = cloneSchemaField(this.#source);
		this.#stopPath();
		this.#stopObserve();
		this.#stopLifetime();
		const callbacks = [...this.#cleanup];
		this.#cleanup.clear();
		// Dispose every child even if one cleanup fails.
		const failures: unknown[] = [];
		for (const cleanup of callbacks) {
			try {
				cleanup();
			} catch (error) {
				failures.push(error);
			}
		}
		if (failures.length) throw new AggregateError(failures, "Field binding cleanup failed.");
	};
	#resolve() {
		if (this.#revoked) return;
		if (
			this.form.editorEpoch !== this.#epoch ||
			this.form.contentLocale !== this.#locale ||
			JSON.stringify(this.form.resource) !== this.#resource ||
			!this.form.editorScopeActive
		) {
			this.destroy();
			return;
		}
		if (this.#revoked) return;
		const version = this.schemaVersion();
		if (this.#revoked) return;
		if (version !== this.#version) {
			this.destroy();
			return;
		}
		const schema = this.getSchema();
		if (this.#revoked) return;
		if (schema.id !== this.#source.id) {
			this.destroy();
			return;
		}
		this.#source = schema;
		const path = this.#occurrence.resolve();
		if (path === undefined) {
			this.destroy();
			return;
		}
		if (path !== this.#path) {
			this.#stopPath();
			this.#path = path;
			this.#stopPath = this.form.register(path);
		}
	}
}
