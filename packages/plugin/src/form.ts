import type { ValidationIssue } from "@riducms/protocol";

/** Identifies the collection document or global currently open in the form. */
export interface FieldFormResource {
	/** The collection slug, or the global slug when `global` is true. */
	collection: string;
	/** The saved document ID. A new document does not have one yet. */
	id?: string;
	/** True when the form edits a global instead of a collection document. */
	global?: boolean;
}

/**
 * Read the current document form, including edits that have not been saved.
 * Returned objects are copies: mutating them does not change the form.
 * In field editors these reads throw once the editor's connection to the form expires.
 */
export interface FieldDocumentForm {
	/** The content locale being edited, such as `"fr"`; separate from the admin UI language. */
	readonly contentLocale?: string;
	/** Which collection document or global is being edited, when supplied by this form. */
	readonly resource?: FieldFormResource;
	/**
	 * Read a copy of a value by its current document-root path, such as `"title"`
	 * or `"reviews.0.note"`. Returns undefined for a missing value. A numbered path
	 * reads whichever row is at that position now; it does not remember row identity.
	 * Check the returned value's type before using it.
	 */
	get(path: string): unknown;
	/** Current validation issues at or below a document-root path, including server issues. */
	issuesFor(path: string): readonly ValidationIssue[];
	/**
	 * Copy all current form values, including unsaved edits. The result is a snapshot:
	 * it does not update when the user types, and changing it does not update the form.
	 * This is form data, not a fresh document fetched from the server.
	 */
	snapshot(): Record<string, unknown>;
}

/** Host-owned advisory feedback. A checked result is not a promise that Save will succeed. */
export interface FieldLiveValidation {
	/** Idle until edited; skipped means the current input could not be checked. */
	readonly status: "idle" | "pending" | "checked" | "skipped" | "failed";
	/** Retry a failed check using current form values. Ridu owns cancellation and requests. */
	retry(): void;
}
