import type { SchemaField } from "@riducms/protocol";

export type FormValues = Record<string, unknown>;

export interface DetachedDraftValue {
	fieldId: string;
	label: string;
	path: string;
	reason: "removed" | "incompatible" | "removed-block";
	value: unknown;
}

export interface FormSchemaState {
	values: FormValues;
	original: FormValues;
}

export interface FormSchemaReconciliation extends FormSchemaState {
	detached: DetachedDraftValue[];
}

export interface FormDraftRecovery extends FormSchemaReconciliation {
	restoredFields: number;
}

export interface FormSchemaReconciliationOptions {
	initializeDefaults?: boolean;
}

export function localizationSource(
	path: string,
	values: FormValues,
	original: FormValues,
	sources: Readonly<Record<string, string>>
) {
	return sources[localizationProvenancePath(path, values, original)];
}

export function shouldSubmitLocalizedPath(
	path: string,
	values: FormValues,
	original: FormValues,
	locale: string | undefined,
	sources: Readonly<Record<string, string>>
) {
	const provenancePath = localizationProvenancePath(path, values, original);
	const source = sources[provenancePath];
	return (
		source === undefined ||
		source === locale ||
		!deepEqual(readPath(values, path), readPath(original, provenancePath))
	);
}

export function submissionFormValues(
	fields: readonly SchemaField[],
	input: FormValues,
	include: (path: string, canonicalPath: string) => boolean = () => true
): FormValues {
	return submissionRecord(fields, input, "", include);
}

export function initialFormValues(fields: readonly SchemaField[]): FormValues {
	const values: FormValues = {};
	for (const field of fields) {
		const defaultValue = initialFieldValue(field);
		if (defaultValue.present) values[field.name] = cloneFormValue(defaultValue.value);
	}
	return values;
}

export function documentFormValues(
	fields: readonly SchemaField[],
	document: Readonly<FormValues>
): FormValues {
	const values: FormValues = {};
	for (const field of fields) {
		if (Object.hasOwn(document, field.name)) {
			values[field.name] = cloneFormValue(document[field.name]);
		}
	}
	return values;
}

function submissionRecord(
	fields: readonly SchemaField[],
	input: unknown,
	prefix: string,
	include: (path: string, canonicalPath: string) => boolean
): FormValues {
	const values = isRecord(input) ? input : {};
	const submitted: FormValues = {};
	for (const field of fields) {
		const path = joinPath(prefix, field.name);
		if (
			field.category === "presentation" ||
			!include(path, field.path) ||
			!Object.hasOwn(values, field.name)
		)
			continue;
		const value = values[field.name];
		if (field.type === "group") {
			submitted[field.name] = submissionRecord(field.nested?.fields ?? [], value, path, include);
		} else if (field.type === "array" && Array.isArray(value)) {
			submitted[field.name] = value.map((row, index) =>
				submissionRecord(field.nested?.fields ?? [], row, `${path}.${index}`, include)
			);
		} else if (field.type === "blocks" && Array.isArray(value)) {
			submitted[field.name] = value.map((row, index) => {
				if (!isRecord(row) || typeof row.blockType !== "string") return cloneFormValue(row);
				const block = field.blocks?.types.find((candidate) => candidate.key === row.blockType);
				return submissionRecord(block?.fields ?? [], row, `${path}.${index}`, include);
			});
		} else {
			submitted[field.name] = cloneFormValue(value);
		}
	}
	copyReservedValue(values, submitted, "_key");
	copyReservedValue(values, submitted, "blockType");
	return submitted;
}

function localizationProvenancePath(path: string, values: FormValues, originalValues: FormValues) {
	const segments = path.split(".");
	const resolved: string[] = [];
	let current: unknown = values;
	let original: unknown = originalValues;
	for (const segment of segments) {
		if (Array.isArray(current) && /^\d+$/.test(segment)) {
			const row = current[Number(segment)];
			const key = isRecord(row) ? row._key : undefined;
			const originalIndex =
				typeof key === "string" && Array.isArray(original)
					? original.findIndex((candidate) => isRecord(candidate) && candidate._key === key)
					: Number(segment);
			const index = originalIndex >= 0 ? originalIndex : Number(segment);
			resolved.push(String(index));
			current = row;
			original = Array.isArray(original) ? original[index] : undefined;
			continue;
		}
		resolved.push(segment);
		current = Array.isArray(current)
			? current[Number(segment)]
			: isRecord(current)
				? current[segment]
				: undefined;
		original = Array.isArray(original)
			? original[Number(segment)]
			: isRecord(original)
				? original[segment]
				: undefined;
	}
	return resolved.join(".");
}

function readPath(values: unknown, path: string) {
	let current = values;
	for (const segment of path.split(".")) {
		current = Array.isArray(current)
			? current[Number(segment)]
			: isRecord(current)
				? current[segment]
				: undefined;
	}
	return current;
}

export function reconcileFormSchema(
	state: FormSchemaState,
	previousFields: readonly SchemaField[],
	nextFields: readonly SchemaField[],
	options: FormSchemaReconciliationOptions = {}
): FormSchemaReconciliation {
	const detached: DetachedDraftValue[] = [];
	const reconciled = reconcileRecord(
		previousFields,
		nextFields,
		state.values,
		state.original,
		detached,
		"",
		options.initializeDefaults === true
	);
	return { ...reconciled, detached };
}

export function recoverFormDraft(
	latest: FormSchemaState,
	draft: FormSchemaState,
	previousFields: readonly SchemaField[],
	nextFields: readonly SchemaField[]
): FormDraftRecovery {
	const reconciled = reconcileFormSchema(draft, previousFields, nextFields);
	const values = cloneFormValues(latest.values);
	let restoredFields = 0;

	for (const field of nextFields) {
		const local = reconciled.values[field.name];
		const original = reconciled.original[field.name];
		if (deepEqual(local, original)) continue;
		values[field.name] = cloneFormValue(local);
		restoredFields += 1;
	}

	return {
		values,
		original: cloneFormValues(latest.original),
		detached: reconciled.detached,
		restoredFields,
	};
}

function reconcileRecord(
	previousFields: readonly SchemaField[],
	nextFields: readonly SchemaField[],
	currentInput: unknown,
	originalInput: unknown,
	detached: DetachedDraftValue[],
	parentPath: string,
	initializeDefaults: boolean
) {
	const current = isRecord(currentInput) ? currentInput : {};
	const original = isRecord(originalInput) ? originalInput : {};
	const values: FormValues = {};
	const originals: FormValues = {};
	const previousByID = new Map(previousFields.map((field) => [field.id, field]));
	const nextIDs = new Set(nextFields.map((field) => field.id));

	for (const next of nextFields) {
		const previous = previousByID.get(next.id);
		if (previous === undefined) {
			if (initializeDefaults) {
				const defaultValue = initialFieldValue(next);
				if (defaultValue.present) {
					values[next.name] = cloneFormValue(defaultValue.value);
					originals[next.name] = cloneFormValue(defaultValue.value);
				}
			}
			continue;
		}

		const currentPresent = Object.hasOwn(current, previous.name);
		const originalPresent = Object.hasOwn(original, previous.name);
		const currentValue = current[previous.name];
		const originalValue = original[previous.name];
		if (!compatibleFields(previous, next)) {
			if (currentPresent && !deepEqual(currentValue, originalValue)) {
				detached.push(detachedValue(previous, currentValue, "incompatible", parentPath));
			}
			if (initializeDefaults) {
				const defaultValue = initialFieldValue(next);
				if (defaultValue.present) {
					values[next.name] = cloneFormValue(defaultValue.value);
					originals[next.name] = cloneFormValue(defaultValue.value);
				}
			}
			continue;
		}

		const reconciled = reconcileFieldValue(
			previous,
			next,
			currentPresent,
			currentValue,
			originalPresent,
			originalValue,
			detached,
			parentPath,
			initializeDefaults
		);
		if (reconciled.currentPresent) values[next.name] = reconciled.current;
		if (reconciled.originalPresent) originals[next.name] = reconciled.original;
	}

	for (const previous of previousFields) {
		if (nextIDs.has(previous.id) || !Object.hasOwn(current, previous.name)) continue;
		const currentValue = current[previous.name];
		if (!deepEqual(currentValue, original[previous.name])) {
			detached.push(detachedValue(previous, currentValue, "removed", parentPath));
		}
	}

	copyReservedValue(current, values, "_key");
	copyReservedValue(original, originals, "_key");
	copyReservedValue(current, values, "blockType");
	copyReservedValue(original, originals, "blockType");
	return { values, original: originals };
}

function reconcileFieldValue(
	previous: SchemaField,
	next: SchemaField,
	currentPresent: boolean,
	currentValue: unknown,
	originalPresent: boolean,
	originalValue: unknown,
	detached: DetachedDraftValue[],
	parentPath: string,
	initializeDefaults: boolean
) {
	if (initializeDefaults && !currentPresent && !originalPresent) {
		const initial = initialFieldValue(next);
		if (initial.present) {
			return {
				currentPresent: true,
				current: cloneFormValue(initial.value),
				originalPresent: true,
				original: cloneFormValue(initial.value),
			};
		}
	}

	if (previous.type === "group" && next.type === "group") {
		if (!currentPresent && !originalPresent) return absentFieldValue();
		const reconciled = reconcileRecord(
			previous.nested?.fields ?? [],
			next.nested?.fields ?? [],
			currentValue,
			originalValue,
			detached,
			joinPath(parentPath, next.name),
			initializeDefaults
		);
		return {
			currentPresent,
			current: reconciled.values,
			originalPresent,
			original: reconciled.original,
		};
	}

	if (previous.type === "array" && next.type === "array") {
		return reconcileArrayValue(
			previous,
			next,
			currentPresent,
			currentValue,
			originalPresent,
			originalValue,
			detached,
			parentPath,
			initializeDefaults
		);
	}

	if (previous.type === "blocks" && next.type === "blocks") {
		return reconcileBlocksValue(
			previous,
			next,
			currentPresent,
			currentValue,
			originalPresent,
			originalValue,
			detached,
			parentPath,
			initializeDefaults
		);
	}

	return {
		currentPresent,
		current: cloneFormValue(currentValue),
		originalPresent,
		original: cloneFormValue(originalValue),
	};
}

function reconcileArrayValue(
	previous: SchemaField,
	next: SchemaField,
	currentPresent: boolean,
	currentValue: unknown,
	originalPresent: boolean,
	originalValue: unknown,
	detached: DetachedDraftValue[],
	parentPath: string,
	initializeDefaults: boolean
) {
	if (!Array.isArray(currentValue) && !Array.isArray(originalValue)) {
		return {
			currentPresent,
			current: cloneFormValue(currentValue),
			originalPresent,
			original: cloneFormValue(originalValue),
		};
	}
	const currentRows = Array.isArray(currentValue) ? currentValue : [];
	const originalRows = Array.isArray(originalValue) ? originalValue : [];
	const previousChildren = previous.nested?.fields ?? [];
	const nextChildren = next.nested?.fields ?? [];
	const rowPath = joinPath(parentPath, next.name);

	return {
		currentPresent,
		current: currentRows.map((row, index) => {
			const originalRow = matchingRow(row, originalRows, index);
			return reconcileRecord(
				previousChildren,
				nextChildren,
				row,
				originalRow,
				detached,
				`${rowPath}.${index}`,
				initializeDefaults
			).values;
		}),
		originalPresent,
		original: originalRows.map(
			(row, index) =>
				reconcileRecord(
					previousChildren,
					nextChildren,
					row,
					row,
					[],
					`${rowPath}.${index}`,
					initializeDefaults
				).values
		),
	};
}

function reconcileBlocksValue(
	previous: SchemaField,
	next: SchemaField,
	currentPresent: boolean,
	currentValue: unknown,
	originalPresent: boolean,
	originalValue: unknown,
	detached: DetachedDraftValue[],
	parentPath: string,
	initializeDefaults: boolean
) {
	if (!Array.isArray(currentValue) && !Array.isArray(originalValue)) {
		return {
			currentPresent,
			current: cloneFormValue(currentValue),
			originalPresent,
			original: cloneFormValue(originalValue),
		};
	}
	const currentRows = Array.isArray(currentValue) ? currentValue : [];
	const originalRows = Array.isArray(originalValue) ? originalValue : [];
	const previousBlocks = new Map((previous.blocks?.types ?? []).map((block) => [block.key, block]));
	const nextBlocks = new Map((next.blocks?.types ?? []).map((block) => [block.key, block]));
	const rowPath = joinPath(parentPath, next.name);
	const keptCurrent: FormValues[] = [];

	for (const [index, row] of currentRows.entries()) {
		if (!isRecord(row)) continue;
		const blockType = String(row.blockType ?? "");
		const previousBlock = previousBlocks.get(blockType);
		const nextBlock = nextBlocks.get(blockType);
		const originalRow = matchingRow(row, originalRows, index);
		if (previousBlock === undefined || nextBlock === undefined) {
			if (!deepEqual(row, originalRow)) {
				detached.push({
					fieldId: previous.id,
					label: previous.admin.label,
					path: `${rowPath}.${index}`,
					reason: "removed-block",
					value: cloneFormValue(row),
				});
			}
			continue;
		}
		keptCurrent.push(
			reconcileRecord(
				previousBlock.fields,
				nextBlock.fields,
				row,
				originalRow,
				detached,
				`${rowPath}.${index}`,
				initializeDefaults
			).values
		);
	}

	const keptOriginal = originalRows.flatMap((row, index) => {
		if (!isRecord(row)) return [];
		const blockType = String(row.blockType ?? "");
		const previousBlock = previousBlocks.get(blockType);
		const nextBlock = nextBlocks.get(blockType);
		if (previousBlock === undefined || nextBlock === undefined) return [];
		return [
			reconcileRecord(
				previousBlock.fields,
				nextBlock.fields,
				row,
				row,
				[],
				`${rowPath}.${index}`,
				initializeDefaults
			).values,
		];
	});

	return {
		currentPresent,
		current: keptCurrent,
		originalPresent,
		original: keptOriginal,
	};
}

function matchingRow(row: unknown, candidates: unknown[], fallbackIndex: number) {
	if (isRecord(row) && typeof row._key === "string") {
		const matched = candidates.find(
			(candidate) => isRecord(candidate) && candidate._key === row._key
		);
		if (matched !== undefined) return matched;
	}
	return candidates[fallbackIndex];
}

function compatibleFields(previous: SchemaField, next: SchemaField) {
	if (previous.type !== next.type) return false;
	if (previous.type === "select") {
		return Boolean(previous.select?.hasMany) === Boolean(next.select?.hasMany);
	}
	if (previous.type === "plugin") return previous.plugin?.key === next.plugin?.key;
	return true;
}

function fieldDefault(field: SchemaField) {
	if (field.type === "select" && field.select?.hasMany === true) {
		if ((field.select.defaultValues?.length ?? 0) === 0) return { present: false };
		return { present: true, value: field.select.defaultValues };
	}
	if (field.default === undefined) return { present: false };
	if (field.type === "number") return { present: true, value: Number(field.default) };
	if (field.type === "checkbox") return { present: true, value: field.default === "true" };
	return { present: true, value: field.default };
}

function initialFieldValue(field: SchemaField): { present: boolean; value?: unknown } {
	const direct = fieldDefault(field);
	if (direct.present) return direct;
	if (field.type === "group") {
		const nested = initialFormValues(field.nested?.fields ?? []);
		return Object.keys(nested).length === 0 ? { present: false } : { present: true, value: nested };
	}
	if (field.type === "array" && (field.nested?.minRows ?? 0) > 0) {
		return {
			present: true,
			value: Array.from({ length: field.nested?.minRows ?? 0 }, () => ({
				...initialFormValues(field.nested?.fields ?? []),
				_key: crypto.randomUUID(),
			})),
		};
	}
	return { present: false };
}

function detachedValue(
	field: SchemaField,
	value: unknown,
	reason: DetachedDraftValue["reason"],
	parentPath: string
) {
	return {
		fieldId: field.id,
		label: field.admin.label,
		path: joinPath(parentPath, field.name),
		reason,
		value: cloneFormValue(value),
	};
}

function absentFieldValue() {
	return {
		currentPresent: false,
		current: undefined,
		originalPresent: false,
		original: undefined,
	};
}

function copyReservedValue(source: FormValues, target: FormValues, name: string) {
	if (Object.hasOwn(source, name)) target[name] = cloneFormValue(source[name]);
}

function joinPath(parent: string, child: string) {
	return parent === "" ? child : `${parent}.${child}`;
}

function deepEqual(left: unknown, right: unknown) {
	return JSON.stringify(left) === JSON.stringify(right);
}

function isRecord(value: unknown): value is FormValues {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function cloneFormValues(values: FormValues): FormValues {
	return cloneFormValue(values) as FormValues;
}

export function cloneFormValue(value: unknown): unknown {
	if (Array.isArray(value)) return value.map(cloneFormValue);
	if (isRecord(value)) {
		return Object.fromEntries(
			Object.entries(value).map(([name, child]) => [name, cloneFormValue(child)])
		);
	}
	return value;
}
