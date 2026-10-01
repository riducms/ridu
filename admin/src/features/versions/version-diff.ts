import {
	isRecord,
	resolveBlockTypes,
	type SchemaCollection,
	type SchemaField,
} from "@riducms/protocol";
import type { AdminI18n } from "@riducms/translations";
import type { AdminVersion } from "@admin/core/api/admin-client";
import { versionDate } from "@admin/features/versions/version-history";
import { formatDateDisplay } from "@admin/fields/scalar/date-value";

export interface VersionDiffRow {
	path: string;
	label: string;
	before: unknown;
	after: unknown;
	changed: boolean;
	field?: SchemaField;
	locale?: string;
	children?: VersionDiffRow[];
}

export function versionDiffRows(
	collection: SchemaCollection,
	current: AdminVersion,
	comparison: AdminVersion | undefined,
	i18n: AdminI18n,
	locales?: readonly string[]
): VersionDiffRow[] {
	const rows = compareFields(
		collection.fields,
		comparison?.Snapshot,
		current.Snapshot,
		"",
		i18n,
		locales
	);
	for (const name of ["updatedAt", "createdAt", "deletedAt"] as const) {
		if (name in current.Snapshot || (comparison && name in comparison.Snapshot))
			rows.push({
				path: name,
				label: i18n.t(`versions:${name}`),
				before: comparison?.Snapshot[name],
				after: current.Snapshot[name],
				changed: !sameValue(comparison?.Snapshot[name], current.Snapshot[name]),
			});
	}
	rows.push({
		path: "_status",
		label: i18n.t("versions:status"),
		before: comparison?.Status,
		after: current.Status,
		changed: comparison?.Status !== current.Status,
	});
	return rows;
}

function compareFields(
	fields: readonly SchemaField[],
	before: unknown,
	after: unknown,
	prefix: string,
	i18n: AdminI18n,
	locales?: readonly string[]
): VersionDiffRow[] {
	const rows: VersionDiffRow[] = [];
	for (const field of fields) {
		if (field.type === "ui" || field.virtual || field.join || field.admin.hidden) continue;
		const label = i18n.text(field.admin.label || field.name, field.admin.labelTranslations);
		const path = prefix ? `${prefix}.${field.name}` : field.name;
		const left = isRecord(before) ? before[field.name] : undefined;
		const right = isRecord(after) ? after[field.name] : undefined;
		const row = compareField(field, left, right, path, label, i18n, locales);
		const section = field.admin.tab || field.admin.collapsible?.label;
		if (section) {
			const sectionPath = `${prefix}:section:${field.admin.tabGroup?.id ?? field.admin.collapsible?.id ?? ""}:${section}`;
			let group = rows.find((item) => item.path === sectionPath);
			if (!group) {
				group = {
					path: sectionPath,
					label: i18n.text(
						section,
						field.admin.tab
							? field.admin.tabTranslations
							: field.admin.collapsible?.labelTranslations
					),
					before: undefined,
					after: undefined,
					changed: false,
					children: [],
				};
				rows.push(group);
			}
			group.children!.push(row);
			group.changed ||= row.changed;
		} else rows.push(row);
	}
	return rows.map((row) => {
		const child = row.children?.[0];
		return !row.field && row.children?.length === 1 && child?.children && child.label === row.label
			? child
			: row;
	});
}

function compareField(
	field: SchemaField,
	before: unknown,
	after: unknown,
	path: string,
	label: string,
	i18n: AdminI18n,
	locales?: readonly string[]
): VersionDiffRow {
	const row: VersionDiffRow = {
		path,
		label,
		before,
		after,
		field,
		changed: !sameValue(before, after),
	};
	if (field.localized && locales) {
		row.children = locales.map((locale) => ({
			...compareField(
				{ ...field, localized: false },
				isRecord(before) ? before[locale] : undefined,
				isRecord(after) ? after[locale] : undefined,
				`${path}:${locale}`,
				label,
				i18n,
				locales
			),
			locale,
		}));
		row.changed = row.children.some((child) => child.changed);
	} else if (field.type === "group" && field.nested) {
		row.children = compareFields(field.nested.fields, before, after, path, i18n, locales);
		row.changed = row.children.some((child) => child.changed);
	} else if ((field.type === "array" && field.nested) || field.blocks) {
		const left = Array.isArray(before) ? before : [];
		const right = Array.isArray(after) ? after : [];
		const rowKey = (value: unknown, index: number) =>
			isRecord(value) && typeof value._key === "string" ? value._key : String(index);
		const leftKeys = left.map(rowKey);
		const rightKeys = right.map(rowKey);
		const keys = [...new Set([...rightKeys, ...leftKeys])];
		const blocks = resolveBlockTypes(field.blocks);
		row.children = keys.map((key, index) => {
			const oldIndex = leftKeys.indexOf(key);
			const newIndex = rightKeys.indexOf(key);
			const oldValue = left[oldIndex];
			const newValue = right[newIndex];
			const value = newValue ?? oldValue;
			const oldBlock = isRecord(oldValue)
				? blocks.find((block) => block.slug === oldValue.blockType)
				: undefined;
			const block = isRecord(value)
				? blocks.find((block) => block.slug === value.blockType)
				: undefined;
			const childPath = `${path}.${key}`;
			// A type replacement may retain row identity. Keep removed fields from the old block visible.
			const childFields = [
				...new Map(
					[...(oldBlock?.fields ?? []), ...(block?.fields ?? field.nested?.fields ?? [])].map(
						(child) => [child.name, child]
					)
				).values(),
			];
			const labels = block?.labels ?? field.nested?.rowLabels;
			const childLabel = `${labels ? i18n.text(labels.singular, labels.singularTranslations) : i18n.t("versions:row")} ${i18n.formatNumber(index + 1, { minimumIntegerDigits: 2 })}`;
			const children = compareFields(childFields, oldValue, newValue, childPath, i18n, locales);
			if (field.blocks) {
				const oldType = isRecord(oldValue) ? oldValue.blockType : undefined;
				const newType = isRecord(newValue) ? newValue.blockType : undefined;
				children.unshift({
					path: `${childPath}.blockType`,
					label: i18n.t("versions:blockType"),
					before: oldType,
					after: newType,
					changed: !sameValue(oldType, newType),
				});
			}
			if (
				field.blocks &&
				((oldValue !== undefined && !oldBlock) || (newValue !== undefined && !block))
			) {
				children.push({
					path: `${childPath}:content`,
					label: i18n.t("versions:content"),
					before: oldValue,
					after: newValue,
					changed: !sameValue(oldValue, newValue),
				});
			}
			if (oldIndex !== newIndex) {
				children.push({
					path: `${childPath}:position`,
					label: i18n.t("versions:position"),
					before: oldIndex < 0 ? undefined : oldIndex + 1,
					after: newIndex < 0 ? undefined : newIndex + 1,
					changed: true,
				});
			}

			return {
				path: childPath,
				label: childLabel,
				before: oldValue,
				after: newValue,
				changed: !sameValue(oldValue, newValue) || oldIndex !== newIndex,
				children,
			};
		});
	}
	return row;
}

export function formatVersionValue(
	value: unknown,
	i18n: AdminI18n,
	row?: Pick<VersionDiffRow, "path" | "field">
): string {
	if (value === undefined || value === null || value === "") return "";
	if (row?.path === "_status")
		return i18n.t(value === "published" ? "documents:published" : "documents:draft");
	if (row?.field?.type === "date") return formatDateDisplay(value, row.field.date?.format, i18n);
	if (["updatedAt", "createdAt", "deletedAt"].includes(row?.path ?? ""))
		return versionDate(String(value), i18n);
	if (typeof value === "string") return value;
	if (typeof value === "number") return i18n.formatNumber(value);
	if (typeof value === "boolean") return i18n.t(value ? "versions:true" : "versions:false");
	if (row?.field?.plugin?.key === "richtext" || (isRecord(value) && isRecord(value.root))) {
		const text = richText(value);
		if (text) return text;
	}
	return JSON.stringify(value, null, 2);
}

function richText(value: unknown): string {
	if (!isRecord(value)) return "";
	if (typeof value.text === "string") return value.text;
	if (isRecord(value.root)) return richText(value.root);
	if (!Array.isArray(value.children)) return "";
	return value.children.map(richText).join(value.type === "root" ? "\n\n" : "");
}

export function sameValue(left: unknown, right: unknown): boolean {
	if (left === right) return true;
	if (Array.isArray(left) && Array.isArray(right))
		return (
			left.length === right.length && left.every((value, index) => sameValue(value, right[index]))
		);
	if (isRecord(left) && isRecord(right)) {
		const keys = Object.keys(left);
		return (
			keys.length === Object.keys(right).length &&
			keys.every((key) => Object.hasOwn(right, key) && sameValue(left[key], right[key]))
		);
	}
	return false;
}

/** Keep unchanged words readable; bound the LCS matrix for large code and rich-text values. */
export function versionTextDiff(before: string, after: string) {
	const left = before.split(/(\s+|[.,;:!?()[\]{}])/).filter(Boolean);
	const right = after.split(/(\s+|[.,;:!?()[\]{}])/).filter(Boolean);
	let start = 0;
	while (start < left.length && start < right.length && left[start] === right[start]) start++;
	let end = 0;
	while (
		end < left.length - start &&
		end < right.length - start &&
		left[left.length - end - 1] === right[right.length - end - 1]
	)
		end++;
	const a = left.slice(start, left.length - end);
	const b = right.slice(start, right.length - end);
	const oldParts: { text: string; changed: boolean }[] = [];
	const newParts: { text: string; changed: boolean }[] = [];
	const add = (parts: typeof oldParts, text: string, changed: boolean) => {
		if (!text) return;
		const last = parts.at(-1);
		if (last?.changed === changed) last.text += text;
		else parts.push({ text, changed });
	};
	add(oldParts, left.slice(0, start).join(""), false);
	add(newParts, right.slice(0, start).join(""), false);
	if (a.length * b.length > 40_000) {
		add(oldParts, a.join(""), true);
		add(newParts, b.join(""), true);
	} else {
		const lengths = Array.from({ length: a.length + 1 }, () => new Uint32Array(b.length + 1));
		for (let i = a.length - 1; i >= 0; i--)
			for (let j = b.length - 1; j >= 0; j--)
				lengths[i]![j] =
					a[i] === b[j]
						? 1 + lengths[i + 1]![j + 1]!
						: Math.max(lengths[i + 1]![j]!, lengths[i]![j + 1]!);
		let i = 0,
			j = 0;
		while (i < a.length || j < b.length) {
			if (i < a.length && j < b.length && a[i] === b[j]) {
				add(oldParts, a[i++]!, false);
				add(newParts, b[j++]!, false);
			} else if (i < a.length && (j === b.length || lengths[i + 1]![j]! >= lengths[i]![j + 1]!))
				add(oldParts, a[i++]!, true);
			else add(newParts, b[j++]!, true);
		}
	}
	add(oldParts, end ? left.slice(-end).join("") : "", false);
	add(newParts, end ? right.slice(-end).join("") : "", false);
	return { before: oldParts, after: newParts };
}

export function versionReferences(row: VersionDiffRow, value: unknown) {
	const reference = row.field?.relationship ?? row.field?.upload;
	if (!reference) return [];
	return (
		Array.isArray(value) ? value : value === undefined || value === null ? [] : [value]
	).flatMap((item) => {
		const collection =
			isRecord(item) && typeof item.relationTo === "string"
				? item.relationTo
				: reference.collectionSlug;
		const id = isRecord(item) ? item.id : item;
		return collection && (typeof id === "string" || typeof id === "number")
			? [{ collection, id: String(id), key: `${collection}:${id}` }]
			: [];
	});
}
