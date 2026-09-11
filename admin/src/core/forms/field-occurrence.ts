import { resolveBlockTypes } from "@riducms/protocol";
import type { SchemaField } from "@riducms/protocol";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import { scopeRepeatedRowField } from "@admin/fields/nested/scoped-field";

type ResolvePath = () => string | undefined;
export interface FieldOccurrence {
	schema: SchemaField;
	resolve: ResolvePath;
	ancestors: readonly { schema: SchemaField; resolve: ResolvePath }[];
}

/** Capture schema-owned identities once. Resolution never trusts a retained numeric index. */
export function captureFieldOccurrence(form: FormController, target: string): FieldOccurrence {
	const find = (
		fields: readonly SchemaField[],
		parent: ResolvePath,
		instance: string,
		ancestors: FieldOccurrence["ancestors"]
	): FieldOccurrence | undefined => {
		for (const field of fields) {
			const path = () => {
				const prefix = parent();
				return prefix === undefined ? undefined : prefix ? `${prefix}.${field.name}` : field.name;
			};
			const current = path();
			if (current === undefined || (target !== current && !target.startsWith(`${current}.`)))
				continue;
			const parents = [...ancestors, { schema: field, resolve: path }];
			const schema = scopeRepeatedRowField(field, parent() ?? "", instance);
			// Root fields do not have a leading dot.
			if (target === current)
				return { schema: { ...schema, path: current }, resolve: path, ancestors };
			if (field.type === "group") {
				// An absent optional object may initialize through its visible inputs.
				// Once present, removing it revokes the child occurrence immediately.
				let initialized = record(form.get(current));
				const objectPath = () => {
					const current = path();
					if (current === undefined) return;
					if (record(form.get(current))) initialized = true;
					else if (initialized) return;
					return current;
				};
				const result = find(field.nested?.fields ?? [], objectPath, instance, parents);
				if (result) return result;
			}
			if (field.type === "array" || field.type === "blocks") {
				const rows = form.get(current);
				const index = Number(target.slice(current.length + 1).split(".")[0]);
				if (!Array.isArray(rows) || !Number.isSafeInteger(index)) continue;
				const row: unknown = rows[index];
				if (!record(row) || typeof row._key !== "string" || !row._key)
					throw new Error(`Cannot bind ${target}: rows require stable _key values.`);
				const key = row._key,
					variant = row.blockType;
				const rowPath = () => {
					const base = path();
					if (base === undefined) return;
					return form.rowPath(base, key, variant);
				};
				const children =
					field.type === "array"
						? field.nested?.fields
						: resolveBlockTypes(field.blocks).find((block) => block.slug === variant)?.fields;
				const result = find(children ?? [], rowPath, `${instance}-${key}`, parents);
				if (result) return result;
			}
			if (field.plugin?.embeddedTrees !== undefined) {
				const initial = form
					.embeddedFields({ ...field, path: current })
					.occurrences.find((item) => target.startsWith(`${item.path}.`));
				if (!initial || initial.identity === undefined)
					throw new Error(`Cannot bind ${target}: embedded payloads require stable identities.`);
				const { identity, tree, block, case: branch } = initial;
				const payloadPath = () => {
					const base = path();
					if (base === undefined) return;
					const match = form.embeddedOccurrence({ ...field, path: base }, tree.key, identity);
					if (
						match === undefined ||
						match?.block.slug !== block.slug ||
						match.case.tagValue !== branch.tagValue
					)
						return;
					return match.path;
				};
				const result = find(
					block.fields,
					payloadPath,
					`${instance}-${tree.key}-${identity}`,
					parents
				);
				if (result) return result;
			}
		}
	};
	const found = find(form.schemaFields, () => "", "binding", []);
	if (!found || found.resolve() === undefined)
		throw new Error(`Cannot bind ${target}: no unique live schema field occurrence exists.`);
	return found;
}
function record(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
