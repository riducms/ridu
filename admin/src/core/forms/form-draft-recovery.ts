import type { SchemaCollection } from "@riducms/protocol";

import { cloneFormValues, type FormValues } from "@admin/core/forms/form-schema";

const recoveryVersion = 1;
const storagePrefix = "ridu:form-recovery:";

export interface FormDraftCheckpoint {
	version: typeof recoveryVersion;
	collection: SchemaCollection;
	documentID?: string;
	values: FormValues;
	original: FormValues;
	createdAt: string;
}

export function saveFormDraft(
	collection: SchemaCollection,
	documentID: string | undefined,
	values: FormValues,
	original: FormValues
): boolean {
	try {
		const checkpoint: FormDraftCheckpoint = {
			version: recoveryVersion,
			collection,
			...(documentID === undefined ? {} : { documentID }),
			values: cloneFormValues(values),
			original: cloneFormValues(original),
			createdAt: new Date().toISOString(),
		};
		sessionStorage.setItem(storageKey(collection.id, documentID), JSON.stringify(checkpoint));
		return true;
	} catch {
		return false;
	}
}

export function takeFormDraft(
	collectionID: string,
	documentID: string | undefined
): FormDraftCheckpoint | undefined {
	const key = storageKey(collectionID, documentID);
	try {
		const encoded = sessionStorage.getItem(key);
		if (encoded === null) return undefined;
		sessionStorage.removeItem(key);
		const decoded: unknown = JSON.parse(encoded);
		return isCheckpoint(decoded, collectionID, documentID) ? decoded : undefined;
	} catch {
		return undefined;
	}
}

export function clearFormDraft(collectionID: string, documentID: string | undefined): void {
	try {
		sessionStorage.removeItem(storageKey(collectionID, documentID));
	} catch {
		// Recovery is best-effort when browser storage is unavailable.
	}
}

function storageKey(collectionID: string, documentID: string | undefined) {
	return `${storagePrefix}${collectionID}:${documentID ?? "new"}`;
}

function isCheckpoint(
	value: unknown,
	collectionID: string,
	documentID: string | undefined
): value is FormDraftCheckpoint {
	if (!isRecord(value) || value.version !== recoveryVersion) return false;
	if (!isRecord(value.collection) || value.collection.id !== collectionID) return false;
	if (value.documentID !== documentID) return false;
	return isRecord(value.values) && isRecord(value.original) && typeof value.createdAt === "string";
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
