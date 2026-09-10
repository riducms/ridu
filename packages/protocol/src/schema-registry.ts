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

/** Resolve definitions at this placement. Values, permissions and editor state are never cached here. */
export function resolveBlockTypes(container: Container | undefined): SchemaBlockType[] {
	if (!container) return empty;
	const resolve = resolvers.get(container);
	if (resolve) return resolve();
	if (container.blockReferences?.length)
		throw new Error("Bind the schema manifest before traversing block references");
	return container.types ?? empty;
}

/** Preserve compact reference ownership when projecting schema metadata for an occurrence. */
export function mapBlockTypes<T extends Container>(
	container: T,
	map: (block: SchemaBlockType) => SchemaBlockType
): T {
	const result = { ...container };
	if (container.blockReferences) result.blockReferences = [...container.blockReferences];
	delete result.types;
	resolvers.set(
		result,
		memo(() => resolveBlockTypes(container).map(map))
	);
	// Inline projections remain ordinary schema values for consumers that serialize them.
	if (!result.blockReferences?.length)
		Object.defineProperty(result, "types", { enumerable: true, get: resolvers.get(result)! });
	return result;
}

/** Bind a compact wire manifest without expanding shared definitions into its resources. */
export function bindSchemaManifest<T extends Resources>(manifest: T): T {
	if (bound.has(manifest)) return manifest;
	const definitions = new Map<string, SchemaBlockType>();
	for (const [index, block] of (manifest.blocks ?? []).entries()) {
		if (!/^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(block.slug) || definitions.has(block.slug))
			throw new Error(`blocks[${index}].slug: invalid or duplicate block slug ${block.slug}`);
		definitions.set(block.slug, block);
	}
	type Cost = { nodes: number; height: number };
	const visited = new Map<string, Cost>();
	const stack: string[] = [];
	let work = 0;
	function inspect(fields: SchemaField[], depth: number): Cost {
		if (depth + stack.length > 48 || (work += fields.length) > 100_000)
			throw new Error("Block schema exceeds traversal bounds");
		const total = { nodes: 0, height: 0 };
		for (const field of fields) {
			const children: Cost[] = [];
			if (field.nested) children.push(inspect(field.nested.fields, depth + 1));
			if (field.blocks) children.push(inspectContainer(field.blocks, depth + 1));
			for (const tree of field.plugin?.embeddedTrees ?? [])
				for (const branch of tree.cases) children.push(inspectContainer(branch, depth + 1));
			total.nodes += 1;
			let height = 1;
			for (const child of children) {
				total.nodes += child.nodes;
				height = Math.max(height, 1 + child.height);
			}
			total.height = Math.max(total.height, height);
			if (total.nodes > 100_000 || total.height > 48)
				throw new Error(`${field.path}: resolved block graph exceeds traversal bounds`);
		}
		return total;
	}
	function inspectContainer(container: Container, depth: number): Cost {
		const refs = container.blockReferences ?? [];
		if (refs.length && container.types?.length)
			throw new Error("Use either inline blocks or blockReferences");
		if (new Set(refs).size !== refs.length) throw new Error("Duplicate block reference");
		const children = refs.map(visit);
		for (const block of container.types ?? []) children.push(inspect(block.fields, depth));
		return children.reduce(
			(total, child) => ({
				nodes: total.nodes + child.nodes,
				height: Math.max(total.height, child.height),
			}),
			{ nodes: 0, height: 0 }
		);
	}
	function visit(slug: string): Cost {
		if (stack.includes(slug))
			throw new Error(`Block reference cycle: ${[...stack, slug].join(" -> ")}`);
		const block = definitions.get(slug);
		if (!block) throw new Error(`Unknown block reference ${slug}`);
		const previous = visited.get(slug);
		if (previous) return previous;
		stack.push(slug);
		const cost = inspect(block.fields, 0);
		stack.pop();
		visited.set(slug, cost);
		return cost;
	}
	for (const slug of definitions.keys()) visit(slug);
	let nodes = 0;
	for (const resource of [...manifest.collections, ...(manifest.globals ?? [])]) {
		nodes += inspect(resource.fields, 0).nodes;
		if (nodes > 100_000) throw new Error("Resolved block graph exceeds traversal bounds");
	}
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
		if (field.blocks) {
			if (field.blocks.blockReferences?.length)
				bind(field.blocks, { ...children, prefix: field.path.split(".") });
			else for (const block of field.blocks.types ?? []) attach(block.fields, children);
		}
		for (const tree of field.plugin?.embeddedTrees ?? [])
			for (const branch of tree.cases) {
				if (branch.blockReferences?.length)
					bind(branch, {
						...children,
						prefix: [...field.path.split("."), tree.key, branch.tagValue],
					});
				else for (const block of branch.types ?? []) attach(block.fields, children);
			}
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
		for (const key of ["row", "tabGroup", "collapsible"] as const) {
			const group = template.admin[key];
			if (group)
				Object.assign(result.admin, {
					[key]: {
						...group,
						id: fieldID(scope.resource, scope.prefix) + group.id.slice(scope.templateID!.length),
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
		if (source.blockReferences?.length) {
			const copy = { ...source };
			bind(copy, { ...children, prefix });
			return copy;
		}
		return mapBlockTypes(source, (block) => {
			const copy = { ...block };
			Object.defineProperty(copy, "fields", {
				enumerable: true,
				get: memo(() => block.fields.map((field) => place(field, children))),
			});
			return copy;
		});
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
