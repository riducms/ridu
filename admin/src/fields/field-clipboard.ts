import type { SchemaField } from "@riducms/protocol";

export type FieldClipboardKind = "field" | "row";

export interface FieldClipboardPayload {
	version: 1;
	kind: FieldClipboardKind;
	signature: string;
	value: unknown;
}

const clipboardPrefix = "ridu-field-clipboard:";
const maximumClipboardLength = 1_000_000;
let memoryClipboard: FieldClipboardPayload | undefined;

export function fieldClipboardSignature(field: SchemaField): string {
	return JSON.stringify(fieldShape(field));
}

export function createFieldClipboardPayload(
	field: SchemaField,
	kind: FieldClipboardKind,
	value: unknown
): FieldClipboardPayload {
	return {
		version: 1,
		kind,
		signature: fieldClipboardSignature(field),
		value: cloneJSON(value),
	};
}

export function parseFieldClipboardPayload(encoded: string): FieldClipboardPayload | undefined {
	if (!encoded.startsWith(clipboardPrefix) || encoded.length > maximumClipboardLength)
		return undefined;
	try {
		const candidate = JSON.parse(
			encoded.slice(clipboardPrefix.length)
		) as Partial<FieldClipboardPayload>;
		if (
			candidate.version !== 1 ||
			(candidate.kind !== "field" && candidate.kind !== "row") ||
			typeof candidate.signature !== "string" ||
			!("value" in candidate)
		)
			return undefined;
		return candidate as FieldClipboardPayload;
	} catch {
		return undefined;
	}
}

export function compatibleClipboardValue(
	payload: FieldClipboardPayload | undefined,
	field: SchemaField,
	kind: FieldClipboardKind
) {
	if (
		payload === undefined ||
		payload.kind !== kind ||
		payload.signature !== fieldClipboardSignature(field)
	)
		return undefined;
	return cloneForPaste(payload.value);
}

export async function writeFieldClipboard(payload: FieldClipboardPayload) {
	memoryClipboard = payload;
	try {
		await navigator.clipboard.writeText(clipboardPrefix + JSON.stringify(payload));
	} catch {
		// The in-memory copy keeps same-admin workflows available when a browser
		// denies clipboard permission or the app is served from an insecure origin.
	}
}

export async function readFieldClipboard() {
	try {
		const encoded = await navigator.clipboard.readText();
		const parsed = parseFieldClipboardPayload(encoded);
		if (parsed !== undefined) {
			memoryClipboard = parsed;
			return parsed;
		}
	} catch {
		// Fall through to the last trusted in-app copy.
	}
	return memoryClipboard;
}

function cloneForPaste(value: unknown): unknown {
	if (Array.isArray(value)) return value.map(cloneForPaste);
	if (value === null || typeof value !== "object") return value;
	return Object.fromEntries(
		Object.entries(value).map(([name, nested]) => [
			name,
			name === "_key" ? crypto.randomUUID() : cloneForPaste(nested),
		])
	);
}

function cloneJSON(value: unknown) {
	return value === undefined ? null : JSON.parse(JSON.stringify(value));
}

function fieldShape(field: SchemaField): unknown {
	return {
		type: field.type,
		category: field.category,
		select: field.select?.choices.map((choice) => choice.value),
		relationship: field.relationship && {
			collection: field.relationship.collectionSlug,
			targets: field.relationship.targets?.map((target) => target.collectionSlug),
			hasMany: field.relationship.hasMany === true,
			polymorphic: field.relationship.polymorphic === true,
		},
		upload: field.upload && {
			collection: field.upload.collectionSlug,
			hasMany: field.upload.hasMany === true,
		},
		nested: field.nested && {
			minRows: field.nested.minRows ?? 0,
			maxRows: field.nested.maxRows ?? 0,
			fields: field.nested.fields.map((child) => ({ name: child.name, shape: fieldShape(child) })),
		},
		blocks: field.blocks?.types.map((block) => ({
			key: block.key,
			fields: block.fields.map((child) => ({ name: child.name, shape: fieldShape(child) })),
		})),
		plugin: field.plugin && { key: field.plugin.key, config: field.plugin.config },
	};
}
