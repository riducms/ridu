import type { AdminI18n } from "@riducms/plugin";
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
		const blockFields = field.blocks.types.flatMap((block) => block.fields);
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
			} else if (requireMissing && field.required) {
				issues.push(requiredIssue(field, path, i18n));
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
			case "json":
			case "plugin":
				// Plugin validators and internal populated-document checks remain server-authoritative.
				break;
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
	if (!field.select?.choices.some((choice) => choice.value === value)) {
		issues.push(
			issue(
				"invalid_choice",
				path,
				translate(
					i18n,
					"errors:allowedChoice",
					{ label: localizedFieldLabel(field, i18n) },
					`${localizedFieldLabel(field, i18n)} is not an allowed choice`
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

	const choices = new Set((field.select?.choices ?? []).map((choice) => choice.value));
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
		if (!choices.has(candidate)) {
			issues.push(
				issue(
					"invalid_choice",
					candidatePath,
					translate(
						i18n,
						"errors:allowedChoice",
						{ label: localizedFieldLabel(field, i18n) },
						`${localizedFieldLabel(field, i18n)} is not an allowed choice`
					)
				)
			);
		}
		if (seen.has(candidate)) {
			issues.push(
				issue(
					"duplicate_choice",
					candidatePath,
					translate(
						i18n,
						"errors:duplicateChoice",
						{ label: localizedFieldLabel(field, i18n) },
						`${localizedFieldLabel(field, i18n)} must not contain duplicate choices`
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
		const block = field.blocks.types.find((candidate) => candidate.key === row.blockType);
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
