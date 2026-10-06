import type {
	SchemaBlockType,
	SchemaBlocksField,
	SchemaEmbeddedTreeCase,
	SchemaField,
	SchemaManifest,
} from "./generated.js";

type Container = SchemaBlocksField | SchemaEmbeddedTreeCase;
type Resources = Pick<SchemaManifest, "collections" | "globals" | "blocks">;
interface Scope {
	definitions: ReadonlyMap<string, SchemaBlockType>;
	resource: string;
	prefix: string[];
	templateID?: string;
	localized: boolean;
}
const resolvers = new WeakMap<Container, () => SchemaBlockType[]>();
const bound = new WeakSet<Resources>();
const empty: SchemaBlockType[] = [];
/** Fields nest at most this many levels along any placement, as the server enforces. */
const maxFieldDepth = 48;

/**
 * Resolve the block definitions a container selects, as placement views at its placement.
 * Values, permissions and editor state are never cached here.
 */
export function resolveBlockTypes(container: Container | undefined): SchemaBlockType[] {
	if (!container) return empty;
	const resolve = resolvers.get(container);
	if (resolve) return resolve();
	if (container.blockReferences?.length)
		throw new Error("Bind the schema manifest before traversing block references");
	return empty;
}

/**
 * Locate a canonical field path's innermost block definition among bound fields: the
 * definition's slug and the field's path within it. A resource's own field has none.
 * Only the containers along the path are resolved.
 */
export function blockDefinitionPath(
	fields: readonly SchemaField[],
	path: string
): { block: string; path: string } | undefined {
	const segments = path.split(".");
	let current = fields;
	let located: { block: string; start: number } | undefined;
	for (let index = 0; index < segments.length;) {
		const field = current.find((candidate) => candidate.name === segments[index]);
		if (field === undefined) return undefined;
		index += 1;
		if (index === segments.length) break;
		let selected: SchemaBlockType | undefined;
		if (field.blocks) {
			selected = resolveBlockTypes(field.blocks).find((block) => block.slug === segments[index]);
			index += 1;
		} else if (field.plugin?.embeddedTrees?.length) {
			const branch = field.plugin.embeddedTrees
				.find((tree) => tree.key === segments[index])
				?.cases.find((candidate) => candidate.tagValue === segments[index + 1]);
			selected = resolveBlockTypes(branch).find((block) => block.slug === segments[index + 2]);
			index += 3;
		} else if (field.nested) {
			current = field.nested.fields;
			continue;
		} else return undefined;
		if (selected === undefined) return undefined;
		located = { block: selected.slug, start: index };
		current = selected.fields;
	}
	return located === undefined || located.start >= segments.length
		? undefined
		: { block: located.block, path: segments.slice(located.start).join(".") };
}

/** Project a container's placement views lazily, preserving its compact selection. */
export function mapBlockTypes<T extends Container>(
	container: T,
	map: (block: SchemaBlockType) => SchemaBlockType
): T {
	const result = { ...container };
	if (container.blockReferences) result.blockReferences = [...container.blockReferences];
	resolvers.set(
		result,
		memo(() => resolveBlockTypes(container).map(map))
	);
	return result;
}

/**
 * Bind a compact wire manifest without expanding shared definitions into its resources.
 * Each definition is checked once, so binding cost follows definitions, not placements.
 */
export function bindSchemaManifest<T extends Resources>(manifest: T): T {
	if (bound.has(manifest)) return manifest;
	const definitions = new Map<string, SchemaBlockType>();
	for (const [index, block] of (manifest.blocks ?? []).entries()) {
		if (!/^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(block.slug) || definitions.has(block.slug))
			throw new Error(`blocks[${index}].slug: invalid or duplicate block slug ${block.slug}`);
		definitions.set(block.slug, block);
	}
	const depths = new Map<string, number>();
	const stack: string[] = [];
	function depth(fields: SchemaField[]): number {
		let deepest = 0;
		for (const field of fields) {
			let children = 0;
			if (field.nested) children = depth(field.nested.fields);
			if (field.blocks) children = Math.max(children, selection(field.blocks));
			for (const tree of field.plugin?.embeddedTrees ?? [])
				for (const branch of tree.cases) children = Math.max(children, selection(branch));
			deepest = Math.max(deepest, 1 + children);
		}
		return deepest;
	}
	function selection(container: Container): number {
		const refs = container.blockReferences ?? [];
		if (new Set(refs).size !== refs.length) throw new Error("Duplicate block reference");
		return Math.max(0, ...refs.map(visit));
	}
	function visit(slug: string): number {
		const known = depths.get(slug);
		if (known !== undefined) return known;
		if (stack.includes(slug))
			throw new Error(`Block reference cycle: ${[...stack, slug].join(" -> ")}`);
		const block = definitions.get(slug);
		if (!block) throw new Error(`Unknown block reference ${slug}`);
		stack.push(slug);
		const levels = depth(block.fields);
		stack.pop();
		if (levels > maxFieldDepth)
			throw new Error(`Block ${slug}: fields nest ${levels} levels deep; at most ${maxFieldDepth}`);
		depths.set(slug, levels);
		return levels;
	}
	for (const slug of definitions.keys()) visit(slug);
	for (const resource of [...manifest.collections, ...(manifest.globals ?? [])])
		if (depth(resource.fields) > maxFieldDepth)
			throw new Error(`${resource.slug}: fields nest more than ${maxFieldDepth} levels deep`);
	// Placements share static metadata. Freeze that metadata once so a resource
	// consumer cannot alter the definition for another placement. Extension APIs
	// receive detached copies through cloneSchemaField.
	const frozen = new WeakSet<object>();
	function freezeDefinition(value: unknown): void {
		if (typeof value !== "object" || value === null || frozen.has(value)) return;
		frozen.add(value);
		for (const child of Object.values(value)) freezeDefinition(child);
		Object.freeze(value);
	}
	for (const definition of definitions.values()) freezeDefinition(definition);
	for (const resource of [...manifest.collections, ...(manifest.globals ?? [])]) {
		attach(resource.fields, { definitions, resource: resource.id, prefix: [], localized: false });
	}
	bound.add(manifest);
	return manifest;
}

function memo<T>(fn: () => T): () => T {
	let value: T;
	let ready = false;
	return () => {
		if (!ready) {
			value = fn();
			ready = true;
		}
		return value;
	};
}
function fieldID(resource: string, path: string[]): string {
	return [
		resource,
		...path.map((part) =>
			part
				.replace(/([A-Z])/g, "-$1")
				.replace(/[_-]+/g, "-")
				.replace(/^-/, "")
				.toLowerCase()
		),
	].join("-");
}
function bind(container: Container, scope: Scope): void {
	resolvers.set(
		container,
		memo(() =>
			(container.blockReferences ?? []).map((slug) => {
				const template = scope.definitions.get(slug)!;
				const result = { ...template };
				Object.defineProperty(result, "fields", {
					enumerable: true,
					get: memo(() =>
						template.fields.map((field) =>
							place(field, {
								...scope,
								prefix: [...scope.prefix, slug],
								templateID: `block-${slug}`,
							})
						)
					),
				});
				return result;
			})
		)
	);
}
function attach(fields: SchemaField[], scope: Scope): void {
	for (const field of fields) {
		const children = { ...scope, localized: scope.localized || !!field.localized };
		if (field.nested) attach(field.nested.fields, children);
		if (field.blocks) bind(field.blocks, { ...children, prefix: field.path.split(".") });
		for (const tree of field.plugin?.embeddedTrees ?? [])
			for (const branch of tree.cases)
				bind(branch, {
					...children,
					prefix: [...field.path.split("."), tree.key, branch.tagValue],
				});
	}
}
function place(template: SchemaField, scope: Scope): SchemaField {
	const path = [...scope.prefix, ...template.path.split(".")];
	const result = {
		...template,
		path: path.join("."),
		id: fieldID(scope.resource, path),
		localized: !!template.localized && !scope.localized,
	};
	const children = { ...scope, localized: scope.localized || !!template.localized };
	// Static metadata is shared; only presentation identities depend on placement.
	if (template.admin.row || template.admin.tabGroup || template.admin.collapsible) {
		result.admin = { ...template.admin };
		// Rebase like the server: replace the definition's ID prefix with the placement's.
		const from = scope.templateID!;
		const to = fieldID(scope.resource, scope.prefix);
		for (const key of ["row", "tabGroup", "collapsible"] as const) {
			const group = template.admin[key];
			if (group)
				Object.assign(result.admin, {
					[key]: {
						...group,
						id: to + (group.id.startsWith(from) ? group.id.slice(from.length) : group.id),
					},
				});
		}
	}
	if (template.nested) {
		result.nested = { ...template.nested };
		Object.defineProperty(result.nested, "fields", {
			enumerable: true,
			get: memo(() => template.nested!.fields.map((field) => place(field, children))),
		});
	}
	function container<T extends Container>(source: T, prefix: string[]): T {
		const copy = { ...source };
		bind(copy, { ...children, prefix });
		return copy;
	}
	if (template.blocks) result.blocks = container(template.blocks, path);
	if (template.plugin)
		result.plugin = {
			...template.plugin,
			embeddedTrees: (template.plugin.embeddedTrees ?? []).map((tree) => ({
				...tree,
				cases: tree.cases.map((branch) => container(branch, [...path, tree.key, branch.tagValue])),
			})),
		};
	return result;
}

/** Detached schema inspection for extension boundaries, retaining lazy reference bindings. */
export function cloneSchemaField(field: SchemaField): SchemaField {
	const { nested, blocks, plugin, ...metadata } = field;
	const result: SchemaField = structuredClone(metadata);
	function cloneBlock(block: SchemaBlockType): SchemaBlockType {
		const { fields, ...metadata } = block;
		const copy = structuredClone(metadata) as SchemaBlockType;
		Object.defineProperty(copy, "fields", {
			enumerable: true,
			get: memo(() => fields.map(cloneSchemaField)),
		});
		return copy;
	}
	if (nested) {
		const { fields, ...metadata } = nested;
		result.nested = { ...structuredClone(metadata), fields: [] };
		Object.defineProperty(result.nested, "fields", {
			enumerable: true,
			get: memo(() => fields.map(cloneSchemaField)),
		});
	}
	if (blocks) result.blocks = mapBlockTypes(blocks, cloneBlock);
	if (plugin) {
		const { embeddedTrees, ...metadata } = plugin;
		result.plugin = structuredClone(metadata);
		if (embeddedTrees)
			result.plugin.embeddedTrees = embeddedTrees.map((tree) => ({
				...tree,
				root: [...tree.root],
				cases: tree.cases.map((branch) => mapBlockTypes(branch, cloneBlock)),
			}));
	}
	return result;
}
