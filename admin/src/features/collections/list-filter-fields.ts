import {
	resolveBlockTypes,
	type SchemaBlockType,
	type SchemaCollection,
	type SchemaField,
} from "@riducms/protocol";
import type { AdminI18n } from "@riducms/translations";

/** A filterable field addressed by the dotted path the list planner and REST `where` accept. */
export interface ListFilterField {
	readonly kind: "field";
	readonly path: string;
	readonly field: SchemaField;
	/** Display labels from the collection root, ending with this field's label. */
	readonly trail: readonly string[];
}

/** A group, array, blocks field or block type whose filterable fields form a nested level. */
export interface ListFilterContainer {
	readonly kind: "group" | "array" | "blocks" | "block";
	readonly path: string;
	readonly trail: readonly string[];
}

export type ListFilterEntry = ListFilterField | ListFilterContainer;

export interface ListFilterLevel {
	/** The container path; the collection root is "". */
	readonly path: string;
	readonly trail: readonly string[];
	readonly entries: readonly ListFilterEntry[];
}

export interface ListFilterSearch {
	readonly entries: readonly ListFilterEntry[];
	/** Further matches exist beyond `entries`. */
	readonly truncated: boolean;
}

interface ListFilterSchema {
	readonly fields: readonly SchemaField[];
	/** Runtime-owned document metadata, offered after the collection's root fields. */
	readonly metadata: readonly SchemaField[];
	/** Every collection, which names a polymorphic relationship's target collections. */
	readonly collections?: readonly SchemaCollection[];
	readonly i18n: Pick<AdminI18n, "language" | "text">;
}

/** The first matches shown for a search: enough to scan, few enough to render at once. */
export const listFilterSearchLimit = 50;

/**
 * Filterable fields of one bound collection schema, browsed one level at a time.
 *
 * Block definitions can be placed along millions of paths, so nothing here enumerates
 * placements. A level materializes only its direct children, resolving a path walks only
 * that path, and search consults per-definition summaries before descending, so work
 * follows block definitions and the entries actually shown.
 */
export class ListFilterFields {
	readonly #index: FilterFieldIndex;
	readonly #canRead: (path: string) => boolean;

	constructor(source: ListFilterSchema | FilterFieldIndex, canRead?: (path: string) => boolean) {
		this.#index = source instanceof FilterFieldIndex ? source : new FilterFieldIndex(source);
		this.#canRead = canRead ?? (() => true);
	}

	/** The same schema limited to paths the actor may read, sharing its lookups. */
	readable(canRead: (path: string) => boolean) {
		return new ListFilterFields(this.#index, (path) => this.#canRead(path) && canRead(path));
	}

	/** Changes when any filterable path or field type changes. */
	get signature() {
		return this.#index.signature;
	}

	get empty() {
		return this.level("")?.entries.length === 0;
	}

	/** The first field at the collection root, used for a new condition. */
	get initial() {
		return this.level("")?.entries.find((entry) => entry.kind === "field");
	}

	resolve(path: string) {
		const field = this.#index.resolve(path);
		return field !== undefined && this.#readablePath(path) ? field : undefined;
	}

	/** The singular label of a relationship target collection, or its slug. */
	collectionLabel(slug: string) {
		return this.#index.collectionLabel(slug);
	}

	level(path: string): ListFilterLevel | undefined {
		const level = this.#index.level(path);
		if (level === undefined || !this.#readablePath(path)) return undefined;
		return { ...level, entries: level.entries.filter((entry) => this.#canRead(entry.path)) };
	}

	search(query: string, scope = "", limit = listFilterSearchLimit): ListFilterSearch {
		if (!this.#readablePath(scope)) return { entries: [], truncated: false };
		return this.#index.search(query, scope, limit, this.#canRead);
	}

	#readablePath(path: string) {
		for (let end = path.indexOf("."); end >= 0; end = path.indexOf(".", end + 1))
			if (!this.#canRead(path.slice(0, end))) return false;
		return path === "" || this.#canRead(path);
	}
}

type Node =
	| {
			readonly kind: "fields";
			readonly path: string;
			readonly trail: readonly string[];
			readonly fields: readonly SchemaField[];
	  }
	| {
			readonly kind: "blocks";
			readonly path: string;
			readonly trail: readonly string[];
			readonly types: readonly SchemaBlockType[];
	  };

/** A level's child with the schema it came from. */
type Child =
	| { readonly entry: ListFilterEntry; readonly field: SchemaField; readonly type?: undefined }
	| {
			readonly entry: ListFilterContainer;
			readonly type: SchemaBlockType;
			readonly field?: undefined;
	  };

/** Access-free, immutable lookups over one bound collection schema. */
class FilterFieldIndex {
	readonly #schema: ListFilterSchema;
	readonly #language: string;
	readonly #nodes = new Map<string, Node | null>();
	readonly #levels = new Map<string, ListFilterLevel | null>();
	readonly #resolved = new Map<string, ListFilterField | null>();
	// A definition's fields, labels and query restrictions are shared by every placement,
	// so its summaries are computed once by slug rather than once per placement.
	readonly #definitionOpens = new Map<string, boolean>();
	readonly #definitionHeight = new Map<string, number>();
	#signature?: string;

	constructor(schema: ListFilterSchema) {
		this.#schema = schema;
		// Read during construction so a reactive owner rebuilds the index for a new language.
		this.#language = schema.i18n.language;
	}

	get signature() {
		this.#signature ??= this.#describe();
		return this.#signature;
	}

	collectionLabel(slug: string) {
		const collection = this.#schema.collections?.find((candidate) => candidate.slug === slug);
		return collection === undefined
			? slug
			: this.#schema.i18n.text(collection.labels.singular, collection.labels.singularTranslations);
	}

	resolve(path: string): ListFilterField | undefined {
		let resolved = this.#resolved.get(path);
		if (resolved === undefined) {
			resolved = this.#resolveUncached(path) ?? null;
			this.#resolved.set(path, resolved);
		}
		return resolved ?? undefined;
	}

	level(path: string): ListFilterLevel | undefined {
		let level = this.#levels.get(path);
		if (level === undefined) {
			const node = this.#node(path);
			level = node && {
				path: node.path,
				trail: node.trail,
				entries: this.#children(node).map((child) => child.entry),
			};
			this.#levels.set(path, level ?? null);
		}
		return level ?? undefined;
	}

	/**
	 * Entries whose label contains `query`, shallowest first. Each pass descends only into
	 * containers whose definition summary has a match at exactly the remaining depth, so
	 * every container it opens yields a result and the work is bounded by the limit.
	 */
	search(
		query: string,
		scope: string,
		limit: number,
		canRead: (path: string) => boolean
	): ListFilterSearch {
		const needle = query.trim().toLocaleLowerCase(this.#language);
		const start = this.#node(scope);
		if (needle === "" || start === undefined) return { entries: [], truncated: false };

		const matches = (label: string) => label.toLocaleLowerCase(this.#language).includes(needle);
		// Whether a definition has a match exactly `depth` levels below its block-type entry.
		const definitionReach = new Map<string, boolean>();
		const typeMatchesAt = (type: SchemaBlockType, depth: number): boolean => {
			if (!this.#opensDefinition(type)) return false;
			if (depth === 1) return matches(this.#blockLabel(type));
			const key = `${type.slug} ${depth}`;
			let found = definitionReach.get(key);
			if (found === undefined) {
				found = fieldsMatchAt(type.fields, depth - 1);
				definitionReach.set(key, found);
			}
			return found;
		};
		const fieldMatchesAt = (field: SchemaField, depth: number): boolean => {
			if (isFilterableLeaf(field)) return depth === 1 && matches(this.#label(field));
			if (!this.#opens(field)) return false;
			if (depth === 1) return matches(this.#label(field));
			return field.type === "blocks"
				? resolveBlockTypes(field.blocks).some((type) => typeMatchesAt(type, depth - 1))
				: fieldsMatchAt(field.nested!.fields, depth - 1);
		};
		const fieldsMatchAt = (fields: readonly SchemaField[], depth: number) =>
			fields.some((field) => fieldMatchesAt(field, depth));

		const entries: ListFilterEntry[] = [];
		const collect = (node: Node, depth: number) => {
			for (const child of this.#children(node)) {
				if (entries.length > limit) return;
				if (!canRead(child.entry.path)) continue;
				if (depth === 1) {
					if (matches(child.entry.trail.at(-1)!)) entries.push(child.entry);
					continue;
				}
				// Both summaries count the child itself as depth 1 below this node.
				const below =
					child.type === undefined
						? fieldMatchesAt(child.field, depth)
						: typeMatchesAt(child.type, depth);
				if (below) collect(this.#childNode(child), depth - 1);
			}
		};
		const height = this.#height(start);
		for (let depth = 1; depth <= height && entries.length <= limit; depth++) collect(start, depth);
		return { entries: entries.slice(0, limit), truncated: entries.length > limit };
	}

	#resolveUncached(path: string): ListFilterField | undefined {
		const metadata = this.#schema.metadata.find((field) => field.path === path);
		if (metadata !== undefined)
			return { kind: "field", path, field: metadata, trail: [this.#label(metadata)] };

		const cut = path.lastIndexOf(".");
		const parent = this.#node(cut < 0 ? "" : path.slice(0, cut));
		if (parent?.kind !== "fields" || cut === 0) return undefined;
		const name = path.slice(cut + 1);
		const field = parent.fields.find((candidate) => candidate.name === name);
		if (field === undefined || !isFilterableLeaf(field)) return undefined;
		return { kind: "field", path, field, trail: [...parent.trail, this.#label(field)] };
	}

	#node(path: string): Node | undefined {
		let node = this.#nodes.get(path);
		if (node === undefined) {
			if (path === "") {
				node = { kind: "fields", path, trail: [], fields: this.#schema.fields };
			} else {
				const cut = path.lastIndexOf(".");
				const parent = this.#node(cut < 0 ? "" : path.slice(0, cut));
				const child =
					parent &&
					this.#children(parent).find(
						(candidate) => candidate.entry.kind !== "field" && candidate.entry.path === path
					);
				node = child ? this.#childNode(child) : null;
			}
			this.#nodes.set(path, node);
		}
		return node ?? undefined;
	}

	#childNode(child: Child): Node {
		const { path, trail } = child.entry;
		if (child.type !== undefined) return { kind: "fields", path, trail, fields: child.type.fields };
		return child.field.type === "blocks"
			? { kind: "blocks", path, trail, types: resolveBlockTypes(child.field.blocks) }
			: { kind: "fields", path, trail, fields: child.field.nested?.fields ?? [] };
	}

	#children(node: Node): Child[] {
		const at = (name: string) => (node.path === "" ? name : `${node.path}.${name}`);
		if (node.kind === "blocks")
			return node.types.flatMap((type) =>
				this.#opensDefinition(type)
					? [
							{
								entry: {
									kind: "block" as const,
									path: at(type.slug),
									trail: [...node.trail, this.#blockLabel(type)],
								},
								type,
							},
						]
					: []
			);

		const children = node.fields.flatMap((field): Child[] => {
			const trail = [...node.trail, this.#label(field)];
			if (isFilterableLeaf(field))
				return [{ entry: { kind: "field", path: at(field.name), field, trail }, field }];
			if (!this.#opens(field)) return [];
			const kind = field.type as ListFilterContainer["kind"];
			return [{ entry: { kind, path: at(field.name), trail }, field }];
		});
		if (node.path === "")
			for (const field of this.#schema.metadata)
				children.push({
					entry: { kind: "field", path: field.path, field, trail: [this.#label(field)] },
					field,
				});
		return children;
	}

	/** Whether a container is queryable and leads to at least one filterable field. */
	#opens(field: SchemaField): boolean {
		if (field.queryRestricted) return false;
		if (field.type === "blocks")
			return resolveBlockTypes(field.blocks).some((type) => this.#opensDefinition(type));
		if (field.type !== "group" && field.type !== "array") return false;
		return (field.nested?.fields ?? []).some(
			(child) => isFilterableLeaf(child) || this.#opens(child)
		);
	}

	#opensDefinition(type: SchemaBlockType) {
		let opens = this.#definitionOpens.get(type.slug);
		if (opens === undefined) {
			// Bound manifests reject reference cycles; the placeholder still ends any recursion.
			this.#definitionOpens.set(type.slug, false);
			opens = type.fields.some((child) => isFilterableLeaf(child) || this.#opens(child));
			this.#definitionOpens.set(type.slug, opens);
		}
		return opens;
	}

	/** Levels at and below a node's entries, counting each field and block type. */
	#height(node: Node): number {
		if (node.kind === "blocks")
			return Math.max(0, ...node.types.map((type) => 1 + this.#definitionLevels(type)));
		const metadata = node.path === "" && this.#schema.metadata.length > 0 ? 1 : 0;
		return Math.max(metadata, this.#fieldsHeight(node.fields));
	}

	#fieldsHeight(fields: readonly SchemaField[]): number {
		let height = 0;
		for (const field of fields) {
			let below = 0;
			if (field.type === "blocks")
				for (const type of resolveBlockTypes(field.blocks))
					below = Math.max(below, 1 + this.#definitionLevels(type));
			else if (field.type === "group" || field.type === "array")
				below = this.#fieldsHeight(field.nested?.fields ?? []);
			height = Math.max(height, 1 + below);
		}
		return height;
	}

	#definitionLevels(type: SchemaBlockType) {
		let height = this.#definitionHeight.get(type.slug);
		if (height === undefined) {
			this.#definitionHeight.set(type.slug, 0);
			height = this.#fieldsHeight(type.fields);
			this.#definitionHeight.set(type.slug, height);
		}
		return height;
	}

	#label(field: SchemaField) {
		return this.#schema.i18n.text(field.admin.label, field.admin.labelTranslations);
	}

	#blockLabel(type: SchemaBlockType) {
		return this.#schema.i18n.text(type.labels.singular, type.labels.singularTranslations);
	}

	#describe() {
		// Each definition is described once, keeping this proportional to the registry.
		const definitions: Record<string, unknown> = {};
		const describe = (fields: readonly SchemaField[]): unknown[] =>
			fields.map((field) => [
				field.name,
				field.type,
				field.queryRestricted === true,
				isFilterableLeaf(field),
				field.type === "blocks"
					? resolveBlockTypes(field.blocks).map((type) => {
							if (!(type.slug in definitions)) {
								definitions[type.slug] = null;
								definitions[type.slug] = describe(type.fields);
							}
							return type.slug;
						})
					: describe(field.nested?.fields ?? []),
			]);
		return JSON.stringify([
			describe(this.#schema.fields),
			this.#schema.metadata.map((field) => [field.path, field.type]),
			definitions,
		]);
	}
}

/** Mirrors the list planner: only queryable scalar, relationship and upload leaves filter. */
function isFilterableLeaf(field: SchemaField) {
	return (
		field.type !== "group" &&
		field.type !== "array" &&
		field.type !== "blocks" &&
		field.queryRestricted !== true &&
		field.virtual === undefined &&
		field.join === undefined &&
		field.plugin === undefined &&
		field.type !== "ui" &&
		field.type !== "json" &&
		field.type !== "point" &&
		field.type !== "code"
	);
}
