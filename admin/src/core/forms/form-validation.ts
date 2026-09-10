import { resolveBlockTypes } from "@riducms/protocol";
import { embeddedOccurrences } from "@admin/core/forms/embedded-fields";
import type { AdminI18n } from "@riducms/plugin";
import { createAdminI18n } from "@riducms/translations";
import type { SchemaField, ValidationIssue } from "@riducms/protocol";

import { initialFormValues, type FormValues } from "@admin/core/forms/form-schema";

export interface FormValidationOptions {
	requireMissing: boolean;
	include?: (path: string, canonicalPath: string) => boolean;
	i18n?: AdminI18n;
}

export function validateFormValues(
	fields: readonly SchemaField[],
	values: FormValues,
	options: FormValidationOptions
): ValidationIssue[] {
	const issues: ValidationIssue[] = [];
	validateFields(
		fields,
		values,
		"",
		options.requireMissing,
		issues,
		options.include ?? (() => true),
		options.i18n
	);
	return issues;
}

export function invalidFieldLabels(
	fields: readonly SchemaField[],
	issues: readonly ValidationIssue[],
	i18n?: AdminI18n
) {
	const labels = new Set<string>();
	for (const issue of issues) {
		labels.add(fieldLabelForPath(fields, issue.path, i18n) ?? issue.path.replaceAll(".", " → "));
	}
	return [...labels];
}

function fieldLabelForPath(fields: readonly SchemaField[], path: string, i18n?: AdminI18n) {
	return resolveFieldLabel(fields, path.split("."), [], i18n);
}

function resolveFieldLabel(
	fields: readonly SchemaField[],
	segments: readonly string[],
	parents: readonly string[],
	i18n?: AdminI18n
): string | undefined {
	const [name, ...remaining] = segments;
	if (name === undefined) return parents.length > 0 ? parents.join(" → ") : undefined;
	const field = fields.find((candidate) => candidate.name === name);
	if (field === undefined) return parents.length > 0 ? parents.join(" → ") : undefined;

	const labels = [...parents, localizedFieldLabel(field, i18n)];
	if (remaining.length === 0) return labels.join(" → ");
	if (field.type === "group" && field.nested !== undefined) {
		return resolveFieldLabel(field.nested.fields, remaining, labels, i18n);
	}
	if (field.type === "array" && field.nested !== undefined) {
		const [index, ...childSegments] = remaining;
		const rowLabels = isIndex(index)
			? [
					...labels,
					translate(
						i18n,
						"fields:rowNumber",
						{ number: Number(index) + 1 },
						`Row ${Number(index) + 1}`
					),
				]
			: labels;
		return resolveFieldLabel(
			field.nested.fields,
			isIndex(index) ? childSegments : remaining,
			rowLabels,
			i18n
		);
	}
	if (field.type === "blocks" && field.blocks !== undefined) {
		const [index, ...childSegments] = remaining;
		const blockLabels = isIndex(index)
			? [
					...labels,
					translate(
						i18n,
						"fields:blockNumber",
						{ number: Number(index) + 1 },
						`Block ${Number(index) + 1}`
					),
				]
			: labels;
		const blockFields = resolveBlockTypes(field.blocks).flatMap((block) => block.fields);
		return resolveFieldLabel(
			blockFields,
			isIndex(index) ? childSegments : remaining,
			blockLabels,
			i18n
		);
	}
	return labels.join(" → ");
}

function isIndex(value: string | undefined) {
	return value !== undefined && /^\d+$/.test(value);
}

function validateFields(
	fields: readonly SchemaField[],
	values: FormValues,
	prefix: string,
	requireMissing: boolean,
	issues: ValidationIssue[],
	include: (path: string, canonicalPath: string) => boolean,
	i18n?: AdminI18n
) {
	for (const field of fields) {
		if (field.category === "presentation") continue;
		const path = joinPath(prefix, field.name);
		if (!include(path, field.path)) continue;
		const exists = Object.hasOwn(values, field.name);
		const value = values[field.name];
		if (!exists) {
			const defaults = requireMissing ? initialFormValues([field]) : {};
			if (Object.hasOwn(defaults, field.name)) {
				validateFields([field], defaults, prefix, true, issues, include, i18n);
			} else if (requireMissing && field.required && !field.dynamicDefault) {
				issues.push(requiredIssue(field, path, i18n));
			} else if (
				requireMissing &&
				!field.dynamicDefault &&
				(field.type === "text-list" || field.type === "number-list")
			) {
				validatePrimitiveList(field, [], path, issues, i18n);
			} else if (requireMissing && field.type === "array" && (field.nested?.minRows ?? 0) > 0) {
				issues.push(
					issue(
						"min_rows",
						path,
						translate(
							i18n,
							"errors:minRows",
							{ label: localizedFieldLabel(field, i18n), count: field.nested?.minRows ?? 0 },
							`${localizedFieldLabel(field, i18n)} must contain at least ${field.nested?.minRows} rows`
						)
					)
				);
			}
			continue;
		}
		if (value === null || value === undefined) {
			if (field.required) issues.push(requiredIssue(field, path, i18n));
			else if (field.type === "text-list" || field.type === "number-list")
				validatePrimitiveList(field, [], path, issues, i18n);
			else if (field.type === "array" && (field.nested?.minRows ?? 0) > 0) {
				issues.push(
					issue(
						"min_rows",
						path,
						translate(
							i18n,
							"errors:minRows",
							{ label: localizedFieldLabel(field, i18n), count: field.nested?.minRows ?? 0 },
							`${localizedFieldLabel(field, i18n)} must contain at least ${field.nested?.minRows} rows`
						)
					)
				);
			}
			continue;
		}

		switch (field.type) {
			case "text-list":
			case "number-list":
				validatePrimitiveList(field, value, path, issues, i18n);
				break;
			case "text":
			case "code":
			case "textarea":
				validateString(field, value, path, issues, i18n);
				break;
			case "email":
			case "date":
				validateOptionalString(field, value, path, issues, i18n);
				break;
			case "number":
				if (typeof value !== "number" || !Number.isFinite(value)) {
					issues.push(
						issue(
							"invalid_number",
							path,
							translate(
								i18n,
								"errors:finiteNumber",
								{ label: localizedFieldLabel(field, i18n) },
								`${localizedFieldLabel(field, i18n)} must be a finite number`
							)
						)
					);
				}
				break;
			case "checkbox":
				if (typeof value !== "boolean") {
					issues.push(
						issue(
							"invalid_type",
							path,
							translate(
								i18n,
								"errors:boolean",
								{ label: localizedFieldLabel(field, i18n) },
								`${localizedFieldLabel(field, i18n)} must be a boolean`
							)
						)
					);
				}
				break;
			case "select":
			case "radio":
				validateSelect(field, value, path, issues, i18n);
				break;
			case "point":
				validatePoint(field, value, path, issues, i18n);
				break;
			case "relationship":
				validateReference(field, value, path, false, issues, i18n);
				break;
			case "upload":
				validateReference(field, value, path, true, issues, i18n);
				break;
			case "group":
				if (!isRecord(value) || field.nested === undefined) {
					issues.push(
						issue(
							"invalid_type",
							path,
							translate(
								i18n,
								"errors:object",
								{ label: localizedFieldLabel(field, i18n) },
								`${localizedFieldLabel(field, i18n)} must be an object`
							)
						)
					);
				} else {
					validateFields(field.nested.fields, value, path, true, issues, include, i18n);
				}
				break;
			case "array":
				validateRows(field, value, path, issues, include, i18n);
				break;
			case "blocks":
				validateBlocks(field, value, path, issues, include, i18n);
				break;
			case "plugin": {
				const embedded = embeddedOccurrences(field, value, path);
				issues.push(...embedded.issues);
				for (const occurrence of embedded.occurrences)
					validateFields(
						occurrence.block.fields,
						occurrence.payload,
						occurrence.path,
						true,
						issues,
						include,
						i18n
					);
				break;
			}
			case "json":
				// Plugin envelope validators and populated-document checks remain server-authoritative.
				break;
		}
	}
}

function validatePrimitiveList(
	field: SchemaField,
	value: unknown,
	path: string,
	issues: ValidationIssue[],
	i18n: AdminI18n = createAdminI18n()
) {
	const label = localizedFieldLabel(field, i18n);
	const report = (
		code: string,
		key: Parameters<AdminI18n["t"]>[0],
		variables: Readonly<Record<string, string | number>> = {}
	) => issues.push(issue(code, path, i18n.t(key, { label, ...variables })));
	if (!Array.isArray(value)) {
		report("invalid_type", "errors:array");
		return;
	}
	const minimum = Math.max(field.required ? 1 : 0, field.list?.minRows ?? 0);
	if (value.length < minimum)
		report(field.required && value.length === 0 ? "required" : "min_rows", "errors:listMinItems", {
			count: minimum,
		});
	if (field.list?.maxRows && value.length > field.list.maxRows)
		report("max_rows", "errors:listMaxItems", { count: field.list.maxRows });
	for (const [index, item] of value.entries()) {
		const number = index + 1;
		if (field.type === "text-list") {
			if (typeof item !== "string") {
				report("invalid_type", "errors:listItemString", { number });
				continue;
			}
			const length = Array.from(item).length;
			if (length < (field.text?.minLength ?? 0))
				report("min_length", "errors:listItemMinLength", { number, count: field.text!.minLength! });
			if (field.text?.maxLength !== undefined && length > field.text.maxLength)
				report("max_length", "errors:listItemMaxLength", { number, count: field.text.maxLength });
		} else if (typeof item !== "number" || !Number.isFinite(item))
			report("invalid_number", "errors:listItemNumber", { number });
		else {
			if (field.number?.min !== undefined && item < field.number.min)
				report("min_value", "errors:listItemMin", { number, min: field.number.min });
			if (field.number?.max !== undefined && item > field.number.max)
				report("max_value", "errors:listItemMax", { number, max: field.number.max });
		}
	}
}

function validateString(
	field: SchemaField,
	value: unknown,
	path: string,
	issues: ValidationIssue[],
	i18n?: AdminI18n
) {
	if (typeof value !== "string") {
		issues.push(
			issue(
				"invalid_type",
				path,
				translate(
					i18n,
					"errors:string",
					{ label: localizedFieldLabel(field, i18n) },
					`${localizedFieldLabel(field, i18n)} must be a string`
				)
			)
		);
	} else if (field.required && value.length === 0) {
		issues.push(requiredIssue(field, path, i18n));
	}
}

function validateOptionalString(
	field: SchemaField,
	value: unknown,
	path: string,
	issues: ValidationIssue[],
	i18n?: AdminI18n
) {
	if (typeof value !== "string") {
		issues.push(
			issue(
				"invalid_type",
				path,
				translate(
					i18n,
					"errors:string",
					{ label: localizedFieldLabel(field, i18n) },
					`${localizedFieldLabel(field, i18n)} must be a string`
				)
			)
		);
	} else if (field.required && value.length === 0) {
		issues.push(requiredIssue(field, path, i18n));
	}
}

function validateSelect(
	field: SchemaField,
	value: unknown,
	path: string,
	issues: ValidationIssue[],
	i18n?: AdminI18n
) {
	if (field.type === "select" && field.select?.hasMany === true) {
		validateSelectMany(field, value, path, issues, i18n);
		return;
	}
	if (typeof value !== "string") {
		issues.push(
			issue(
				"invalid_type",
				path,
				translate(
					i18n,
					"errors:string",
					{ label: localizedFieldLabel(field, i18n) },
					`${localizedFieldLabel(field, i18n)} must be a string`
				)
			)
		);
		return;
	}
	if (value === "") {
		if (field.required) issues.push(requiredIssue(field, path, i18n));
		return;
	}
	if (!field.select?.options.some((option) => option.value === value)) {
		issues.push(
			issue(
				"invalid_option",
				path,
				translate(
					i18n,
					"errors:allowedOption",
					{ label: localizedFieldLabel(field, i18n) },
					`${localizedFieldLabel(field, i18n)} is not an allowed option`
				)
			)
		);
	}
}

function validateSelectMany(
	field: SchemaField,
	value: unknown,
	path: string,
	issues: ValidationIssue[],
	i18n?: AdminI18n
) {
	if (!Array.isArray(value)) {
		issues.push(
			issue(
				"invalid_type",
				path,
				translate(
					i18n,
					"errors:array",
					{ label: localizedFieldLabel(field, i18n) },
					`${localizedFieldLabel(field, i18n)} must be an array`
				)
			)
		);
		return;
	}
	if (value.length === 0) {
		if (field.required) issues.push(requiredIssue(field, path, i18n));
		return;
	}

	const options = new Set((field.select?.options ?? []).map((option) => option.value));
	const seen = new Set<string>();
	for (const [index, candidate] of value.entries()) {
		const candidatePath = `${path}.${index}`;
		if (typeof candidate !== "string") {
			issues.push(
				issue(
					"invalid_type",
					candidatePath,
					translate(
						i18n,
						"errors:string",
						{ label: localizedFieldLabel(field, i18n) },
						`${localizedFieldLabel(field, i18n)} must be a string`
					)
				)
			);
			continue;
		}
		if (!options.has(candidate)) {
			issues.push(
				issue(
					"invalid_option",
					candidatePath,
					translate(
						i18n,
						"errors:allowedOption",
						{ label: localizedFieldLabel(field, i18n) },
						`${localizedFieldLabel(field, i18n)} is not an allowed option`
					)
				)
			);
		}
		if (seen.has(candidate)) {
			issues.push(
				issue(
					"duplicate_option",
					candidatePath,
					translate(
						i18n,
						"errors:duplicateOption",
						{ label: localizedFieldLabel(field, i18n) },
						`${localizedFieldLabel(field, i18n)} must not contain duplicate options`
					)
				)
			);
		} else {
			seen.add(candidate);
		}
	}
}

function validatePoint(
	field: SchemaField,
	value: unknown,
	path: string,
	issues: ValidationIssue[],
	i18n?: AdminI18n
) {
	if (
		!Array.isArray(value) ||
		value.length !== 2 ||
		!value.every((coordinate) => typeof coordinate === "number" && Number.isFinite(coordinate)) ||
		value[0] < -180 ||
		value[0] > 180 ||
		value[1] < -90 ||
		value[1] > 90
	) {
		issues.push(
			issue(
				"invalid_point",
				path,
				translate(
					i18n,
					"errors:validPoint",
					{ label: localizedFieldLabel(field, i18n) },
					`${localizedFieldLabel(field, i18n)} must be a valid [longitude, latitude] pair`
				)
			)
		);
	}
}

function validateReference(
	field: SchemaField,
	value: unknown,
	path: string,
	upload: boolean,
	issues: ValidationIssue[],
	i18n?: AdminI18n
) {
	const contract = upload ? field.upload : field.relationship;
	const hasMany = contract?.hasMany === true;
	const kind = upload ? "upload" : "relationship";
	if (hasMany) {
		if (!Array.isArray(value)) {
			issues.push(
				issue(
					"invalid_type",
					path,
					translate(
						i18n,
						"errors:array",
						{ label: localizedFieldLabel(field, i18n) },
						`${localizedFieldLabel(field, i18n)} must be an array`
					)
				)
			);
			return;
		}
		if (field.required && value.length === 0) {
			issues.push(
				issue(
					"required",
					path,
					translate(
						i18n,
						"errors:oneReference",
						{
							label: localizedFieldLabel(field, i18n),
							kind: translate(i18n, upload ? "fields:upload" : "fields:relationship", {}, kind),
						},
						`${localizedFieldLabel(field, i18n)} must contain at least one ${kind}`
					)
				)
			);
		}
		for (const [index, item] of value.entries()) {
			validateReferenceValue(field, item, `${path}.${index}`, upload, issues, i18n);
		}
		return;
	}
	if (value === "") {
		if (field.required) issues.push(requiredIssue(field, path, i18n));
		return;
	}
	validateReferenceValue(field, value, path, upload, issues, i18n);
}

function validateReferenceValue(
	field: SchemaField,
	value: unknown,
	path: string,
	upload: boolean,
	issues: ValidationIssue[],
	i18n?: AdminI18n
) {
	if (upload || field.relationship?.polymorphic !== true) {
		if (typeof value !== "string" || value === "") {
			issues.push(
				issue(
					upload ? "invalid_upload" : "invalid_relationship",
					path,
					translate(
						i18n,
						"errors:referenceID",
						{
							kind: translate(
								i18n,
								upload ? "fields:upload" : "fields:relationship",
								{},
								upload ? "upload" : "relationship"
							),
						},
						`${upload ? "upload" : "relationship"} reference must be a non-empty ID string`
					)
				)
			);
		}
		return;
	}
	if (
		!isRecord(value) ||
		typeof value.relationTo !== "string" ||
		typeof value.id !== "string" ||
		value.id === "" ||
		!field.relationship.targets?.some((target) => target.collectionSlug === value.relationTo)
	) {
		issues.push(
			issue(
				"invalid_relationship",
				path,
				translate(
					i18n,
					"errors:polymorphicReference",
					{},
					"polymorphic reference requires an allowed relationTo and non-empty ID"
				)
			)
		);
	}
}

function validateRows(
	field: SchemaField,
	value: unknown,
	path: string,
	issues: ValidationIssue[],
	include: (path: string, canonicalPath: string) => boolean,
	i18n?: AdminI18n
) {
	if (!Array.isArray(value) || field.nested === undefined) {
		issues.push(
			issue(
				"invalid_type",
				path,
				translate(
					i18n,
					"errors:array",
					{ label: localizedFieldLabel(field, i18n) },
					`${localizedFieldLabel(field, i18n)} must be an array`
				)
			)
		);
		return;
	}
	if (field.required && value.length === 0) {
		issues.push(
			issue(
				"required",
				path,
				translate(
					i18n,
					"errors:oneRow",
					{ label: localizedFieldLabel(field, i18n) },
					`${localizedFieldLabel(field, i18n)} must contain at least one row`
				)
			)
		);
	}
	if (value.length < (field.nested.minRows ?? 0) && !(field.required && value.length === 0)) {
		issues.push(
			issue(
				"min_rows",
				path,
				translate(
					i18n,
					"errors:minRows",
					{ label: localizedFieldLabel(field, i18n), count: field.nested.minRows ?? 0 },
					`${localizedFieldLabel(field, i18n)} must contain at least ${field.nested.minRows} rows`
				)
			)
		);
	}
	if ((field.nested.maxRows ?? 0) > 0 && value.length > (field.nested.maxRows ?? 0)) {
		issues.push(
			issue(
				"max_rows",
				path,
				translate(
					i18n,
					"errors:maxRows",
					{ label: localizedFieldLabel(field, i18n), count: field.nested.maxRows ?? 0 },
					`${localizedFieldLabel(field, i18n)} must contain no more than ${field.nested.maxRows} rows`
				)
			)
		);
	}
	for (const [index, row] of value.entries()) {
		const rowPath = `${path}.${index}`;
		if (!isRecord(row)) {
			issues.push(
				issue(
					"invalid_type",
					rowPath,
					translate(i18n, "errors:rowObject", {}, "array row must be an object")
				)
			);
			continue;
		}
		validateFields(field.nested.fields, row, rowPath, true, issues, include, i18n);
	}
}

function validateBlocks(
	field: SchemaField,
	value: unknown,
	path: string,
	issues: ValidationIssue[],
	include: (path: string, canonicalPath: string) => boolean,
	i18n?: AdminI18n
) {
	if (!Array.isArray(value) || field.blocks === undefined) {
		issues.push(
			issue(
				"invalid_type",
				path,
				translate(
					i18n,
					"errors:array",
					{ label: localizedFieldLabel(field, i18n) },
					`${localizedFieldLabel(field, i18n)} must be an array`
				)
			)
		);
		return;
	}
	if (field.required && value.length === 0) {
		issues.push(
			issue(
				"required",
				path,
				translate(
					i18n,
					"errors:oneBlock",
					{ label: localizedFieldLabel(field, i18n) },
					`${localizedFieldLabel(field, i18n)} must contain at least one block`
				)
			)
		);
	}
	const minRows = field.blocks.minRows ?? 0;
	const maxRows = field.blocks.maxRows ?? 0;
	if (value.length < minRows && !(field.required && value.length === 0)) {
		issues.push(
			issue(
				"min_rows",
				path,
				translate(
					i18n,
					"errors:minRows",
					{ label: localizedFieldLabel(field, i18n), count: minRows },
					`${localizedFieldLabel(field, i18n)} must contain at least ${minRows} rows`
				)
			)
		);
	}
	if (maxRows > 0 && value.length > maxRows) {
		issues.push(
			issue(
				"max_rows",
				path,
				translate(
					i18n,
					"errors:maxRows",
					{ label: localizedFieldLabel(field, i18n), count: maxRows },
					`${localizedFieldLabel(field, i18n)} must contain no more than ${maxRows} rows`
				)
			)
		);
	}

	for (const [index, row] of value.entries()) {
		const rowPath = `${path}.${index}`;
		if (!isRecord(row)) {
			issues.push(
				issue(
					"invalid_type",
					rowPath,
					translate(i18n, "errors:blockObject", {}, "block must be an object")
				)
			);
			continue;
		}
		const block = resolveBlockTypes(field.blocks).find(
			(candidate) => candidate.slug === row.blockType
		);
		if (block === undefined) {
			issues.push(
				issue(
					"invalid_block",
					`${rowPath}.blockType`,
					translate(i18n, "errors:blockType", {}, "block type is not allowed")
				)
			);
			continue;
		}
		validateFields(block.fields, row, rowPath, true, issues, include, i18n);
	}
}

function requiredIssue(field: SchemaField, path: string, i18n?: AdminI18n) {
	return issue(
		"required",
		path,
		translate(
			i18n,
			"errors:fieldRequired",
			{ label: localizedFieldLabel(field, i18n) },
			`${localizedFieldLabel(field, i18n)} is required`
		)
	);
}

function issue(code: string, path: string, message: string): ValidationIssue {
	return { code, path, message };
}

function joinPath(prefix: string, name: string) {
	return prefix === "" ? name : `${prefix}.${name}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function translate(
	i18n: AdminI18n | undefined,
	key: Parameters<AdminI18n["t"]>[0],
	variables: Readonly<Record<string, string | number>>,
	fallback: string
) {
	return i18n?.t(key, variables) ?? fallback;
}

function localizedFieldLabel(field: SchemaField, i18n?: AdminI18n) {
	return i18n?.text(field.admin.label, field.admin.labelTranslations) ?? field.admin.label;
}

/** Unknown stored variants require schema recovery even if a UI edit removed the row. */
export function unknownBlockIssues(
	fields: readonly SchemaField[],
	values: FormValues,
	i18n?: AdminI18n,
	prefix = ""
): ValidationIssue[] {
	const issues: ValidationIssue[] = [];
	for (const field of fields) {
		const value = values[field.name];
		const path = joinPath(prefix, field.name);
		if (field.type === "group" && isRecord(value))
			issues.push(...unknownBlockIssues(field.nested?.fields ?? [], value, i18n, path));
		if (field.type === "plugin") {
			const embedded = embeddedOccurrences(field, value, path);
			issues.push(...embedded.issues);
			for (const occurrence of embedded.occurrences)
				issues.push(
					...unknownBlockIssues(occurrence.block.fields, occurrence.payload, i18n, occurrence.path)
				);
		}
		if (!Array.isArray(value)) continue;
		if (field.type === "array") {
			value.forEach((row, index) => {
				if (isRecord(row))
					issues.push(
						...unknownBlockIssues(field.nested?.fields ?? [], row, i18n, `${path}.${index}`)
					);
			});
		} else if (field.type === "blocks") {
			value.forEach((row, index) => {
				const block = isRecord(row)
					? resolveBlockTypes(field.blocks).find((candidate) => candidate.slug === row.blockType)
					: undefined;
				if (block === undefined)
					issues.push(
						issue(
							"unknown_block_schema",
							`${path}.${index}.blockType`,
							translate(
								i18n,
								"errors:unknownBlockRecovery",
								{},
								"Restore the missing block schema or migrate the document before saving."
							)
						)
					);
				else
					issues.push(
						...unknownBlockIssues(block.fields, row as FormValues, i18n, `${path}.${index}`)
					);
			});
		}
	}
	return issues;
}
