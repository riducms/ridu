import type { SchemaField, ValidationIssue } from "@riducms/protocol";
import type { FieldAuthoringHost } from "../authoring";
import type { FieldDocumentForm, FieldLiveValidation } from "../form";
import type { AdminI18n } from "../i18n";

/** Field types whose inputs you can replace with a custom component. Lists contain primitive strings or numbers; the list is one field. */
export type FieldEditorType =
	| "text"
	| "textarea"
	| "email"
	| "date"
	| "code"
	| "number"
	| "checkbox"
	| "text-list"
	| "number-list";
/** The value your field component reads and writes. Date and code fields contain strings. */
export type FieldEditorValue<Type extends FieldEditorType> = Type extends "text-list"
	? string[]
	: Type extends "number-list"
		? number[]
		: Type extends "number"
			? number
			: Type extends "checkbox"
				? boolean
				: string;

/**
 * The current field value, validation messages and input controls for your component.
 * Ridu registers it and cleans it up. In arrays/blocks it follows the row's stable
 * `_key` through reorder. Removing the row, closing the editor, or replacing the
 * document, locale, schema or saved/reset values makes it stale. Old connections
 * cannot edit a new row that happens to occupy the same position.
 */
export interface FieldBinding<Type extends FieldEditorType = FieldEditorType> {
	/**
	 * A copy of the resolved field definition, including its current path, label and rules.
	 * `schema.admin.readOnly` reflects field configuration and access, not a pending document save.
	 */
	readonly schema: SchemaField & { type: Type };
	/** Current form value, including unsaved edits; null/undefined mean empty or absent. */
	readonly value: FieldEditorValue<Type> | null | undefined;
	/** Current validation issues, including errors returned by a document save. */
	readonly issues: readonly ValidationIssue[];
	/** Opted-in server feedback, managed by the host; save validation stays independent. */
	readonly liveValidation: FieldLiveValidation;
	/** Whether changes are blocked right now, including during a document save. `set` checks this again when called. */
	readonly readOnly: boolean;
	/** True once the connection expires. Value becomes undefined, issues empty and writes throw. */
	readonly stale: boolean;
	/**
	 * ID, name and accessibility attributes for your input. Spread these onto the
	 * control, and handle `field.readOnly` and value changes separately.
	 * The optional Field wrapper supplies the matching label and error messages.
	 * Native `required` is false for boolean and list controls: a list requires
	 * entries, not nonempty text in every item. Ridu validates the container on save.
	 */
	readonly inputProps: {
		id: string;
		name: string;
		required: boolean;
		"aria-invalid": boolean;
		"aria-describedby": string | undefined;
		"aria-errormessage": string | undefined;
	};
	/**
	 * Change the value in the unsaved form; `null` clears it. This does not save to
	 * the server. Throws if stale, read-only, or given the wrong value type. Lists are copied on reads and writes.
	 * Normal field validation and server authorization still apply on document save.
	 */
	set(value: FieldEditorValue<Type> | null): void;
}

/**
 * Props for a custom field input selected by Go `field.Admin{Editor: field.Component("app:name")}` and
 * registered with `defineFieldEditor` in `admin/src/admin.config.ts`.
 * Use `FieldEditorProps<"text", MyConfig>` for a text editor with decoded settings.
 */
export type FieldEditorProps<Type extends FieldEditorType = FieldEditorType, Config = undefined> = {
	/** Read the current value and update it with `field.set(...)`. */
	field: FieldBinding<Type>;
	/** Interface translations and formatting using the admin's language/preferences. */
	i18n: AdminI18n;
	/**
	 * Read other values in this unsaved document form. These methods return copies
	 * and expire with the editor. Local editors update only their own `field`;
	 * this form interface does not expose `bind` for writing another field.
	 */
	form: Pick<FieldDocumentForm, "contentLocale" | "resource" | "get" | "issuesFor"> &
		Partial<Pick<FieldDocumentForm, "snapshot">>;
	/**
	 * Browse or fetch related documents. These are the local editor's available tools;
	 * a local editor mounted in an embedded form reads and writes that form’s detached payload.
	 * The containing paired plugin owns Apply/Cancel and Go plugin requests.
	 */
	authoring: Pick<
		FieldAuthoringHost,
		"collections" | "documentRevision" | "referenceBrowser" | "findDocument"
	> & { readonly locale: string | undefined };
} & ([Config] extends [undefined]
	? { config?: never }
	: {
			/** Go settings validated by the registration's decoder before rendering. */ config: Config;
		});
