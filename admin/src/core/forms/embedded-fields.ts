import { resolveBlockTypes } from "@riducms/protocol";
import type {
	SchemaBlockType,
	SchemaEmbeddedTree,
	SchemaEmbeddedTreeCase,
	SchemaField,
	ValidationIssue,
} from "@riducms/protocol";

export interface EmbeddedOccurrence {
	tree: SchemaEmbeddedTree;
	case: SchemaEmbeddedTreeCase;
	block: SchemaBlockType;
	payload: Record<string, unknown>;
	path: string;
	identity: string | undefined;
}

/** Only descriptor-owned tree edges are visited. Payloads return to ordinary field traversal. */
export function embeddedOccurrences(field: SchemaField, value: unknown, path = field.path) {
	const occurrences: EmbeddedOccurrence[] = [];
	const issues: ValidationIssue[] = [];
	let work = 0;
	const fail = (path: string, message: string, code = "invalid_embedded") =>
		issues.push({ path, message, code });
	if (value === null || value === undefined) return { occurrences, issues };
	for (const tree of field.plugin?.embeddedTrees ?? []) {
		if (tree.version !== 1) {
			fail(path, `Unsupported embedded tree metadata version ${tree.version}`);
			continue;
		}
		let root: unknown = value;
		let rootPath = path;
		for (const segment of tree.root) {
			root = isEmbeddedRecord(root) ? root[segment] : undefined;
			rootPath += `.${segment}`;
		}
		const keys = new Set<string>();
		const visit = (node: unknown, nodePath: string, depth: number) => {
			if (++work > 10_000 || depth > 64) {
				fail(nodePath, "Embedded field traversal budget exceeded", "embedded_budget");
				return;
			}
			if (!isEmbeddedRecord(node)) {
				fail(nodePath, "Embedded tree node must be an object");
				return;
			}
			const branch = tree.cases.find((candidate) => candidate.tagValue === node[tree.tag]);
			if (branch !== undefined) {
				const payload = node[branch.payload];
				const payloadPath = `${nodePath}.${branch.payload}`;
				if (!isEmbeddedRecord(payload)) {
					fail(payloadPath, "Embedded payload must be an object");
				} else {
					const block = resolveBlockTypes(branch).find(
						(candidate) => candidate.slug === payload[branch.discriminator]
					);
					const identity = payload[branch.identity];
					if (identity !== undefined && (typeof identity !== "string" || identity.trim() === ""))
						fail(
							`${payloadPath}.${branch.identity}`,
							"Embedded identity must be a non-empty string",
							"invalid_key"
						);
					if (typeof identity === "string") {
						if (keys.has(identity))
							fail(
								`${payloadPath}.${branch.identity}`,
								"Embedded identity must be unique",
								"duplicate_key"
							);
						keys.add(identity);
					}
					if (block === undefined)
						fail(
							`${payloadPath}.${branch.discriminator}`,
							"Restore the missing embedded schema or migrate the document before saving.",
							"unknown_block_schema"
						);
					else
						occurrences.push({
							tree,
							case: branch,
							block,
							payload,
							path: payloadPath,
							identity: typeof identity === "string" ? identity : undefined,
						});
				}
			}
			const children = node[tree.children];
			if (children === undefined || children === null) return;
			if (!Array.isArray(children)) {
				fail(`${nodePath}.${tree.children}`, "Embedded children must be an array");
				return;
			}
			for (const [index, child] of children.entries()) {
				if (work > 10_000) break;
				visit(child, `${nodePath}.${tree.children}.${index}`, depth + 1);
			}
		};
		if (Array.isArray(root)) {
			for (const [index, node] of root.entries()) {
				if (work > 10_000) break;
				visit(node, `${rootPath}.${index}`, tree.root.length + 1);
			}
		} else visit(root, rootPath, tree.root.length + 1);
	}
	return { occurrences, issues };
}

/** Mutates only schema-owned payloads in an already detached value. */
export function transformEmbeddedPayloads(
	field: SchemaField,
	value: unknown,
	path: string,
	transform: (occurrence: EmbeddedOccurrence) => Record<string, unknown>
) {
	const result = embeddedOccurrences(field, value, path);
	if (result.issues.length > 0)
		throw new Error(`${result.issues[0]?.path}: ${result.issues[0]?.message}`);
	for (const occurrence of result.occurrences) {
		const transformed = { ...transform(occurrence) };
		for (const key of Object.keys(occurrence.payload)) delete occurrence.payload[key];
		Object.assign(occurrence.payload, transformed);
	}
	return value;
}

export function isEmbeddedRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
