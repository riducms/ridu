import { resolveBlockTypes } from "@riducms/protocol";
import { transformEmbeddedPayloads } from "@admin/core/forms/embedded-fields";
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
	return cloneFieldForPaste(field, payload.value, kind);
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

/** Clone owned repeating rows and declared payloads; arbitrary JSON retains its identity semantics. */
export function cloneFieldForPaste(
	field: SchemaField,
	value: unknown,
	kind: FieldClipboardKind = "field"
): unknown {
	function row(input: unknown): unknown {
		if (input === null || typeof input !== "object" || Array.isArray(input))
			return cloneJSON(input);
		const source = input as Record<string, unknown>;
		const children =
			field.type === "blocks"
				? (resolveBlockTypes(field.blocks).find((block) => block.slug === source.blockType)
						?.fields ?? [])
				: (field.nested?.fields ?? []);
		return { ...record(children, source), _key: crypto.randomUUID() };
	}
	if (kind === "row") return row(value);
	if ((field.type === "array" || field.type === "blocks") && Array.isArray(value))
		return value.map(row);
	if (
		field.type === "group" &&
		value !== null &&
		typeof value === "object" &&
		!Array.isArray(value)
	) {
		return record(field.nested?.fields ?? [], value as Record<string, unknown>);
	}
	if (field.type === "plugin")
		return transformEmbeddedPayloads(field, cloneJSON(value), field.path, (occurrence) => ({
			...record(occurrence.block.fields, occurrence.payload),
			[occurrence.case.identity]: crypto.randomUUID(),
		}));
	return cloneJSON(value);
}

function record(fields: readonly SchemaField[], source: Record<string, unknown>) {
	const copy = cloneJSON(source) as Record<string, unknown>;
	for (const field of fields) {
		if (Object.hasOwn(source, field.name))
			copy[field.name] = cloneFieldForPaste(field, source[field.name]);
	}
	return copy;
}

function cloneJSON(value: unknown) {
	return value === undefined ? null : JSON.parse(JSON.stringify(value));
}

function fieldShape(field: SchemaField): unknown {
	return {
		type: field.type,
		category: field.category,
		select: field.select?.options.map((option) => option.value),
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
		blocks: field.blocks && {
			minRows: field.blocks.minRows ?? 0,
			maxRows: field.blocks.maxRows ?? 0,
			types: resolveBlockTypes(field.blocks).map((block) => ({
				key: block.slug,
				fields: block.fields.map((child) => ({ name: child.name, shape: fieldShape(child) })),
			})),
		},
		plugin: field.plugin && {
			key: field.plugin.key,
			config: field.plugin.config,
			embeddedTrees: field.plugin.embeddedTrees?.map((tree) => ({
				version: tree.version,
				key: tree.key,
				root: tree.root,
				children: tree.children,
				tag: tree.tag,
				cases: tree.cases.map((branch) => ({
					tagValue: branch.tagValue,
					payload: branch.payload,
					discriminator: branch.discriminator,
					identity: branch.identity,
					types: resolveBlockTypes(branch).map((block) => ({
						key: block.slug,
						fields: block.fields.map((child) => ({ name: child.name, shape: fieldShape(child) })),
					})),
				})),
			})),
		},
	};
}
