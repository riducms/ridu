import { readdir, readFile } from "node:fs/promises";
import { relative, resolve } from "node:path";
import { parse } from "svelte/compiler";

const repositoryRoot = resolve(import.meta.dir, "..");
const roots = [resolve(repositoryRoot, "admin/src"), resolve(repositoryRoot, "plugins")];
const prohibitedInputTypes = new Set([
	"checkbox",
	"date",
	"datetime-local",
	"radio",
	"range",
	"time",
]);
const violations: string[] = [];

for (const root of roots) {
	for (const file of await svelteFiles(root)) await checkFile(file);
}

if (violations.length > 0) {
	console.error("Admin UI policy violations:");
	for (const violation of violations) console.error(`  ${violation}`);
	process.exitCode = 1;
} else {
	console.log("Admin UI policy passed.");
}

async function svelteFiles(directory: string): Promise<string[]> {
	const entries = await readdir(directory, { withFileTypes: true });
	const files: string[] = [];
	for (const entry of entries) {
		const path = resolve(directory, entry.name);
		if (entry.isDirectory()) files.push(...(await svelteFiles(path)));
		else if (entry.isFile() && path.endsWith(".svelte")) files.push(path);
	}
	return files;
}

async function checkFile(path: string) {
	const source = await readFile(path, "utf8");
	const legacyIndex = source.indexOf("{@const");
	if (legacyIndex >= 0) {
		violations.push(`${location(path, source, legacyIndex)} legacy {@const}; use {const} or {let}`);
	}

	const ast = parse(source);
	visit(ast, (node) => {
		if (node.type !== "Element" || typeof node.name !== "string") return;
		if (hasException(node, source)) return;

		if (node.name === "select") {
			violations.push(`${location(path, source, node.start)} raw <select>; use an owned Select`);
			return;
		}

		if (node.name !== "input") return;
		const type = nativeInputType(node, source);
		if (type !== undefined && prohibitedInputTypes.has(type)) {
			violations.push(
				`${location(path, source, node.start)} raw <input type=\"${type}\">; use an owned Ridu control`
			);
		}
	});
}

type Node = {
	type?: string;
	name?: string;
	start?: number;
	end?: number;
	attributes?: unknown;
};

function visit(
	value: unknown,
	inspect: (node: Required<Pick<Node, "type" | "start">> & Node) => void
) {
	const seen = new Set<object>();
	const walk = (candidate: unknown) => {
		if (candidate === null || typeof candidate !== "object") return;
		if (seen.has(candidate)) return;
		seen.add(candidate);
		if (Array.isArray(candidate)) {
			for (const item of candidate) walk(item);
			return;
		}
		const node = candidate as Node;
		if (typeof node.type === "string" && typeof node.start === "number") {
			inspect(node as Required<Pick<Node, "type" | "start">> & Node);
		}
		for (const child of Object.values(node)) walk(child);
	};
	walk(value);
}

function hasException(node: Node, source: string) {
	if (typeof node.start !== "number" || !Array.isArray(node.attributes)) return false;
	if (
		node.attributes.some((attribute) => attributeName(attribute) === "data-ridu-native-exception")
	) {
		return true;
	}
	const before = source.slice(Math.max(0, node.start - 240), node.start);
	return /<!--\s*ridu-ui-policy:\s*allow-native-control\s*-->\s*$/.test(before);
}

function nativeInputType(node: Node, source: string) {
	if (!Array.isArray(node.attributes)) return undefined;
	const attribute = node.attributes.find((candidate) => attributeName(candidate) === "type");
	if (attribute === undefined) return undefined;
	const rendered = attributeSource(attribute, source);
	const match = rendered.match(/^type\s*=\s*["']([^"']+)["']$/);
	return match?.[1];
}

function attributeName(attribute: unknown) {
	if (attribute === null || typeof attribute !== "object") return undefined;
	const name = (attribute as { name?: unknown }).name;
	return typeof name === "string" ? name : undefined;
}

function attributeSource(attribute: unknown, source: string) {
	if (attribute === null || typeof attribute !== "object") return "";
	const { start, end } = attribute as { start?: unknown; end?: unknown };
	return typeof start === "number" && typeof end === "number" ? source.slice(start, end) : "";
}

function location(path: string, source: string, index: number) {
	const before = source.slice(0, index);
	const line = before.split("\n").length;
	const column = index - before.lastIndexOf("\n");
	return `${relative(repositoryRoot, path)}:${line}:${column}`;
}
