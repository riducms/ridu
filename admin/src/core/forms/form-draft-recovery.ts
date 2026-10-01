import { bindSchemaManifest, isRecord, resolveBlockTypes } from "@riducms/protocol";
import type { SchemaBlockType, SchemaCollection, SchemaField } from "@riducms/protocol";
import { embeddedOccurrences } from "@admin/core/forms/embedded-fields";

import { cloneFormValues, type FormValues } from "@admin/core/forms/form-schema";

const recoveryVersion = 3;
const storagePrefix = "ridu:form-recovery:";

export interface FormDraftCheckpoint {
	version: typeof recoveryVersion;
	collection: SchemaCollection;
	blocks: SchemaBlockType[];
	documentID?: string;
	locale?: string;
	values: FormValues;
	original: FormValues;
	base: FormDraftBase;
	createdAt: string;
}

export interface FormDraftBase {
	revision?: number;
	updatedAt?: string;
}

export function formDraftBase(
	document: Readonly<Record<string, unknown>> | undefined
): FormDraftBase {
	return {
		...(typeof document?._revision === "number" && document._revision > 0
			? { revision: document._revision }
			: {}),
		...(typeof document?.updatedAt === "string" ? { updatedAt: document.updatedAt } : {}),
	};
}

export function sameFormDraftBase(
	checkpoint: FormDraftCheckpoint,
	document: Readonly<Record<string, unknown>>
): boolean {
	const latest = formDraftBase(document);
	if (checkpoint.base.revision !== undefined || latest.revision !== undefined)
		return checkpoint.base.revision === latest.revision;
	// Every served document has updatedAt; a checkpoint without a base is never current.
	return checkpoint.base.updatedAt !== undefined && checkpoint.base.updatedAt === latest.updatedAt;
}

/** Resolve a checkpoint field to the same current row or declared plugin payload.
 * Permissions for another occurrence at the same numeric index are never reused. */
export function formDraftAccessPath(
	fields: readonly SchemaField[],
	draft: FormValues,
	current: FormValues,
	path: string,
	draftPrefix = "",
	currentPrefix = ""
): string | undefined {
	for (const field of fields) {
		const draftPath = draftPrefix === "" ? field.name : `${draftPrefix}.${field.name}`;
		const currentPath = currentPrefix === "" ? field.name : `${currentPrefix}.${field.name}`;
		if (path === draftPath) return currentPath;
		if (!path.startsWith(`${draftPath}.`)) continue;
		const value = draft[field.name];
		const latest = current[field.name];
		if (field.type === "group" && isRecord(value))
			return formDraftAccessPath(
				field.nested?.fields ?? [],
				value,
				isRecord(latest) ? latest : {},
				path,
				draftPath,
				currentPath
			);
		if ((field.type === "array" || field.type === "blocks") && Array.isArray(value)) {
			const index = Number(path.slice(draftPath.length + 1).split(".")[0]);
			const row = value[index];
			if (!isRecord(row) || typeof row._key !== "string" || !Array.isArray(latest)) return;
			const matches = latest.flatMap((candidate, index) =>
				isRecord(candidate) &&
				candidate._key === row._key &&
				(field.type !== "blocks" || candidate.blockType === row.blockType)
					? [{ row: candidate, index }]
					: []
			);
			if (matches.length !== 1) return;
			const match = matches[0]!;
			const children =
				field.type === "array"
					? field.nested?.fields
					: resolveBlockTypes(field.blocks).find((block) => block.slug === row.blockType)?.fields;
			return formDraftAccessPath(
				children ?? [],
				row,
				match.row,
				path,
				`${draftPath}.${index}`,
				`${currentPath}.${match.index}`
			);
		}
		if (field.type === "plugin") {
			const occurrence = embeddedOccurrences(field, value, draftPath).occurrences.find(
				(candidate) => path.startsWith(`${candidate.path}.`)
			);
			if (occurrence?.identity === undefined) return;
			const matches = embeddedOccurrences(field, latest, currentPath).occurrences.filter(
				(candidate) =>
					candidate.identity === occurrence.identity &&
					candidate.tree.key === occurrence.tree.key &&
					candidate.case.tagValue === occurrence.case.tagValue &&
					candidate.block.slug === occurrence.block.slug
			);
			if (matches.length !== 1) return;
			return formDraftAccessPath(
				occurrence.block.fields,
				occurrence.payload,
				matches[0]!.payload,
				path,
				occurrence.path,
				matches[0]!.path
			);
		}
	}
}

export function saveFormDraft(
	collection: SchemaCollection,
	documentID: string | undefined,
	values: FormValues,
	original: FormValues,
	base: FormDraftBase = {},
	locale?: string,
	blocks: SchemaBlockType[] = []
): boolean {
	try {
		const checkpoint: FormDraftCheckpoint = {
			version: recoveryVersion,
			collection,
			blocks,
			...(documentID === undefined ? {} : { documentID }),
			...(locale === undefined ? {} : { locale }),
			values: cloneFormValues(values),
			original: cloneFormValues(original),
			base,
			createdAt: new Date().toISOString(),
		};
		sessionStorage.setItem(
			storageKey(collection.id, documentID, locale),
			JSON.stringify(checkpoint)
		);
		return true;
	} catch {
		return false;
	}
}

/** Read a valid recovery checkpoint without consuming it. Startup uses this to
 * finish data-dependent access before the owning form adopts the draft. */
export function peekFormDraft(
	collectionID: string,
	documentID: string | undefined,
	locale?: string
): FormDraftCheckpoint | undefined {
	try {
		const encoded = sessionStorage.getItem(storageKey(collectionID, documentID, locale));
		if (encoded === null) return undefined;
		const decoded: unknown = JSON.parse(encoded);
		if (!isCheckpoint(decoded, collectionID, documentID, locale)) return undefined;
		bindSchemaManifest({ collections: [decoded.collection], globals: [], blocks: decoded.blocks });
		return decoded;
	} catch {
		return undefined;
	}
}

export function clearFormDraft(
	collectionID: string,
	documentID: string | undefined,
	locale?: string
): void {
	try {
		sessionStorage.removeItem(storageKey(collectionID, documentID, locale));
	} catch {
		// Recovery is best-effort when browser storage is unavailable.
	}
}

function storageKey(collectionID: string, documentID: string | undefined, locale?: string) {
	return `${storagePrefix}${JSON.stringify([collectionID, documentID ?? null, locale ?? null])}`;
}

function isCheckpoint(
	value: unknown,
	collectionID: string,
	documentID: string | undefined,
	locale: string | undefined
): value is FormDraftCheckpoint {
	if (!isRecord(value) || value.version !== recoveryVersion) return false;
	if (!isRecord(value.collection) || value.collection.id !== collectionID) return false;
	if (!Array.isArray(value.collection.fields)) return false;
	if (!Array.isArray(value.blocks)) return false;
	if (value.documentID !== documentID) return false;
	if (value.locale !== locale) return false;
	if (!isRecord(value.base)) return false;
	if (
		value.base.revision !== undefined &&
		(typeof value.base.revision !== "number" ||
			!Number.isSafeInteger(value.base.revision) ||
			value.base.revision < 0)
	)
		return false;
	if (value.base.updatedAt !== undefined && typeof value.base.updatedAt !== "string") return false;
	return isRecord(value.values) && isRecord(value.original) && typeof value.createdAt === "string";
}
