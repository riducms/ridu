import { isRecord, resolveBlockTypes } from "@riducms/protocol";
import type { AccessCapabilitiesEnvelope, SchemaField, ValidationIssue } from "@riducms/protocol";
import { embeddedOccurrences } from "@admin/core/forms/embedded-fields";

export interface FieldValueLocation {
	schema?: SchemaField;
	path: string;
	token: string;
	value: unknown;
}

/** Go and JavaScript may spell the same JSON string differently (notably U+2028/2029). */
export function canonicalIssueTarget(target: string): string | undefined {
	try {
		const segments: unknown = JSON.parse(target);
		return Array.isArray(segments) && segments.every((segment) => typeof segment === "string")
			? JSON.stringify(segments)
			: undefined;
	} catch {
		return undefined;
	}
}

/** Issues describe the submitted snapshot. Follow stable occurrences, never current indexes. */
export function correlateFormIssues(
	fields: readonly SchemaField[],
	before: Record<string, unknown>,
	after: Record<string, unknown>,
	issues: readonly ValidationIssue[]
) {
	return correlate(indexFieldValues(fields, before), indexFieldValues(fields, after), issues);
}

export function correlateEmbeddedIssues(
	field: SchemaField,
	before: unknown,
	after: unknown,
	issues: readonly ValidationIssue[]
) {
	const relevant = issues.filter(
		(issue) => issue.path === field.path || issue.path.startsWith(`${field.path}.`)
	);
	if (relevant.length === 0) return [];
	return correlate(
		indexFieldValues([field], { [field.name]: before }),
		indexFieldValues([field], { [field.name]: after }),
		relevant,
		false
	);
}

export function indexFieldValues(fields: readonly SchemaField[], values: Record<string, unknown>) {
	const locations: FieldValueLocation[] = [];
	const add = (path: string, token: readonly string[], value: unknown, schema?: SchemaField) =>
		locations.push({
			path,
			token: JSON.stringify(token),
			value,
			...(schema === undefined ? {} : { schema }),
		});
	const record = (
		children: readonly SchemaField[],
		value: unknown,
		path: string,
		token: readonly string[]
	) => {
		if (!isRecord(value)) return;
		for (const child of children)
			visit(child, value[child.name], `${path}.${child.name}`, [...token, child.id]);
	};
	const visit = (field: SchemaField, value: unknown, path: string, token: readonly string[]) => {
		add(path, token, value, field);
		if (field.type === "group") record(field.nested?.fields ?? [], value, path, token);
		if ((field.type === "array" || field.type === "blocks") && Array.isArray(value)) {
			const keys = new Map<string, number>();
			for (const row of value) {
				if (isRecord(row) && typeof row._key === "string")
					keys.set(row._key, (keys.get(row._key) ?? 0) + 1);
			}
			for (const [i, row] of value.entries()) {
				if (!isRecord(row) || typeof row._key !== "string" || keys.get(row._key) !== 1) continue;
				const block = resolveBlockTypes(field.blocks).find(
					(candidate) => candidate.slug === row.blockType
				);
				const next = [...token, row._key, block?.slug ?? ""];
				add(`${path}.${i}`, next, row);
				record(block?.fields ?? field.nested?.fields ?? [], row, `${path}.${i}`, next);
			}
		}
		if (field.plugin?.embeddedTrees !== undefined) {
			const result = embeddedOccurrences(field, value, path);
			if (result.issues.length > 0) return;
			for (const occurrence of result.occurrences) {
				if (occurrence.identity === undefined) continue;
				const next = [
					...token,
					occurrence.tree.key,
					occurrence.case.tagValue,
					occurrence.block.slug,
					occurrence.identity,
				];
				add(occurrence.path, next, occurrence.payload);
				record(occurrence.block.fields, occurrence.payload, occurrence.path, next);
			}
		}
	};
	for (const field of fields) visit(field, values[field.name], field.path, [field.id]);
	return locations;
}

// An index below a schema-owned location has no stable identity of its own.
// After the container changes, neither issues nor access may follow that index.
export function canRebaseIndexedSuffix(suffix: string, before: unknown, after: unknown) {
	return !/\.\d+(?:\.|$)/.test(suffix) || JSON.stringify(before) === JSON.stringify(after);
}

/** Evaluated occurrence permissions follow schema identities, never replacement indexes. */
export function rebaseFieldAccess(
	before: readonly FieldValueLocation[],
	after: readonly FieldValueLocation[],
	access: AccessCapabilitiesEnvelope["fields"]
) {
	const previous = new Map(before.map((location) => [location.path, location]));
	const current = new Map(after.map((location) => [location.token, location]));
	const fields: AccessCapabilitiesEnvelope["fields"] = {};
	for (const [path, capability] of Object.entries(access)) {
		let prefix = path;
		while (!previous.has(prefix) && prefix.includes("."))
			prefix = prefix.slice(0, prefix.lastIndexOf("."));
		const source = previous.get(prefix);
		if (!source) {
			fields[path] = capability;
			continue;
		}
		const target = current.get(source.token);
		if (!target) continue;
		const suffix = path.slice(source.path.length);
		if (!canRebaseIndexedSuffix(suffix, source.value, target.value)) continue;
		fields[target.path + suffix] = capability;
	}
	return fields;
}

function correlate(
	before: FieldValueLocation[],
	after: FieldValueLocation[],
	issues: readonly ValidationIssue[],
	useTargets = true
) {
	const previous = new Map(before.map((location) => [location.path, location]));
	const previousTargets = new Map(before.map((location) => [location.token, location]));
	const current = new Map(after.map((location) => [location.token, location]));
	return issues.flatMap((issue) => {
		if (useTargets && issue.target !== undefined) {
			// The server may transform/reorder its final candidate before validation. Its
			// display path then describes that candidate, not our submitted snapshot.
			const token = canonicalIssueTarget(issue.target);
			if (token === undefined) return [];
			const source = previousTargets.get(token);
			const target = current.get(token);
			if (!source || !target || JSON.stringify(source.value) !== JSON.stringify(target.value))
				return [];
			return [{ ...issue, path: target.path }];
		}
		let path = issue.path;
		while (!previous.has(path) && path.includes(".")) path = path.slice(0, path.lastIndexOf("."));
		const source = previous.get(path);
		if (source === undefined) return [issue];
		const target = current.get(source.token);
		if (target === undefined) return [];
		const suffix = issue.path.slice(source.path.length);
		if (!canRebaseIndexedSuffix(suffix, source.value, target.value)) return [];
		if (JSON.stringify(at(source.value, suffix)) !== JSON.stringify(at(target.value, suffix)))
			return [];
		return [{ ...issue, path: target.path + suffix }];
	});
}

function at(value: unknown, suffix: string) {
	for (const segment of suffix.split(".").filter(Boolean)) {
		if (Array.isArray(value)) value = value[Number(segment)];
		else if (isRecord(value)) value = value[segment];
		else return undefined;
	}
	return value;
}
