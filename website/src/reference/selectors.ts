import { referenceModules } from './generated';
import { tokenizeReferenceSignature } from './signature';
import type { ReferenceModule, ReferenceSymbol } from './types';

export interface ReferenceSymbolEntry {
	module: ReferenceModule;
	symbol: ReferenceSymbol;
}

export interface ReferenceSymbolDisplayName {
	qualifier: string;
	label: string;
}

export const allReferenceSymbols: ReferenceSymbolEntry[] = referenceModules.flatMap((module) =>
	module.symbols.map((symbol) => ({ module, symbol }))
);

const referenceEntriesByHref = new Map(
	allReferenceSymbols.map((entry) => [
		`/reference/${entry.module.slug}/${entry.symbol.slug}/`,
		entry
	])
);
const referenceEntryBySymbol = new Map(
	allReferenceSymbols.map((entry) => [entry.symbol, entry] as const)
);

/** Resolve an explicit or unambiguous public reference type to its canonical route. */
export function referenceTypeHref(symbol: ReferenceSymbol, typeName: string): string | undefined {
	const explicit = Object.hasOwn(symbol.typeLinks, typeName)
		? symbol.typeLinks[typeName]
		: undefined;
	if (explicit) return explicit;
	if (typeName === symbol.name || typeName.split('.').at(-1) === symbol.name.split('.').at(-1)) {
		return undefined;
	}
	if (genericParameterNames(symbol).has(typeName)) return undefined;

	const current = referenceEntryBySymbol.get(symbol);
	const separator = typeName.lastIndexOf('.');
	if (separator > 0) {
		const qualifier = typeName.slice(0, separator);
		const name = typeName.slice(separator + 1);
		const matchingModules = referenceModules.filter((module) => {
			const packageBase = module.packageName.slice(module.packageName.lastIndexOf('/') + 1);
			return [module.name, module.slug, packageBase].includes(qualifier);
		});
		const matches = matchingModules.flatMap((module) =>
			module.symbols
				.filter(
					(candidate) =>
						['type', 'interface', 'class'].includes(candidate.kind) && candidate.name === name
				)
				.map((candidate) => ({ module, symbol: candidate }))
		);
		if (matches.length === 1 && matches[0].symbol !== symbol) {
			return `/reference/${matches[0].module.slug}/${matches[0].symbol.slug}/`;
		}
		return undefined;
	}

	const local = current?.module.symbols.find(
		(candidate) =>
			candidate !== symbol &&
			['type', 'interface', 'class'].includes(candidate.kind) &&
			candidate.name === typeName
	);
	if (local && current) return `/reference/${current.module.slug}/${local.slug}/`;
	return undefined;
}

function genericParameterNames(symbol: ReferenceSymbol): Set<string> {
	const names = new Set<string>();
	const declarationName = symbol.name
		.split('.')
		.at(-1)
		?.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
	if (!declarationName) return names;
	const parameters = symbol.signature.match(
		new RegExp(`${declarationName}\\s*(?:<([^>]+)>|\\[([^\\]]+)\\])`)
	);
	const source = parameters?.[1] ?? parameters?.[2];
	if (!source) return names;
	for (const parameter of source.split(',')) {
		const name = parameter.trim().match(/^([A-Za-z_$][\w$]*)/)?.[1];
		if (name) names.add(name);
	}
	return names;
}

/** Add safe inferred links without overriding hand-authored cross-package choices. */
export function resolvedReferenceTypeLinks(symbol: ReferenceSymbol): Record<string, string> {
	const links = Object.assign(Object.create(null) as Record<string, string>, symbol.typeLinks);
	const declarations = [
		symbol.signature,
		...symbol.parameters.map((parameter) => parameter.type),
		...(symbol.returns ? [symbol.returns.type] : [])
	];
	for (const declaration of declarations) {
		for (const match of declaration.matchAll(/[A-Za-z_$@][\w$@]*(?:\.[A-Za-z_$][\w$]*)*/g)) {
			const typeName = match[0];
			const target = referenceTypeHref(symbol, typeName);
			if (target) links[typeName] = target;
		}
	}
	return links;
}

export function findReferenceModule(slug: string): ReferenceModule | undefined {
	return referenceModules.find((module) => module.slug === slug);
}

export function findReferenceSymbol(
	moduleSlug: string,
	symbolSlug: string
): ReferenceSymbol | undefined {
	return findReferenceModule(moduleSlug)?.symbols.find((symbol) => symbol.slug === symbolSlug);
}

export function referenceSymbolDisplayName(
	module: ReferenceModule,
	symbol: ReferenceSymbol
): ReferenceSymbolDisplayName {
	const memberSeparator = symbol.name.lastIndexOf('.');
	if (memberSeparator > 0) {
		return {
			qualifier: symbol.name.slice(0, memberSeparator + 1),
			label: symbol.name.slice(memberSeparator + 1)
		};
	}

	if (module.language === 'go') {
		return {
			qualifier: `${module.packageName.slice(module.packageName.lastIndexOf('/') + 1)}.`,
			label: symbol.name
		};
	}

	return { qualifier: '', label: symbol.name };
}

export function referencedTypeEntries(symbol: ReferenceSymbol): ReferenceSymbolEntry[] {
	const seen = new Set<string>();
	const entries: ReferenceSymbolEntry[] = [];
	const declarations = [
		symbol.signature,
		...symbol.parameters.map((parameter) => parameter.type),
		...(symbol.returns ? [symbol.returns.type] : [])
	];

	const typeLinks = resolvedReferenceTypeLinks(symbol);
	for (const declaration of declarations) {
		const tokens = tokenizeReferenceSignature(declaration, {
			name: symbol.name,
			parameters: symbol.parameters,
			typeLinks
		});

		for (const token of tokens) {
			if (!token.href || seen.has(token.href)) continue;
			seen.add(token.href);

			const entry = referenceEntriesByHref.get(token.href);
			if (
				!entry ||
				entry.symbol === symbol ||
				!['type', 'interface', 'class'].includes(entry.symbol.kind)
			) {
				continue;
			}

			entries.push(entry);
		}
	}

	return entries;
}

/** Bun-style inlining belongs on callables and aliases, not already-complete ordinary type pages. */
export function shouldInlineReferencedTypes(symbol: ReferenceSymbol): boolean {
	return (
		['function', 'method', 'command'].includes(symbol.kind) ||
		(symbol.kind === 'type' &&
			(/\bfunc\s*\(/.test(symbol.signature) ||
				/=\s*\(/.test(symbol.signature) ||
				/^type\s+[^=]+?=/.test(symbol.signature)))
	);
}

/** Apply the rendered inlining policy, including canonical-only facade aliases. */
export function inlineReferencedTypeEntries(symbol: ReferenceSymbol): ReferenceSymbolEntry[] {
	if (!shouldInlineReferencedTypes(symbol)) return [];
	const entries = referencedTypeEntries(symbol);
	const aliasTarget = symbol.signature.match(/^type\s+[^=]+?=\s*([A-Za-z0-9_./@]+)/)?.[1];
	if (!aliasTarget) return entries;

	const targetHref = resolvedReferenceTypeLinks(symbol)[aliasTarget];
	const target = targetHref
		? entries.find(
				(entry) => `/reference/${entry.module.slug}/${entry.symbol.slug}/` === targetHref
			)
		: undefined;
	if (!target) return [];
	const localMembers = symbol.parameters.map((parameter) => parameter.name);
	const canonicalMembers = target.symbol.parameters.map((parameter) => parameter.name);
	const hasCanonicalParity =
		localMembers.length > 0 &&
		localMembers.length === canonicalMembers.length &&
		localMembers.every((name, index) => name === canonicalMembers[index]);
	return hasCanonicalParity ? [] : [target];
}

/** Exact method pages belonging to a documented receiver/interface type. */
export function receiverMethodEntries(
	module: ReferenceModule,
	symbol: ReferenceSymbol
): ReferenceSymbolEntry[] {
	if (!['type', 'interface', 'class'].includes(symbol.kind)) return [];
	const methodEntries = (ownerModule: ReferenceModule, owner: ReferenceSymbol) => {
		const prefix = `${owner.name}.`;
		return ownerModule.symbols.flatMap((candidate) => {
			const suffix = candidate.name.slice(prefix.length);
			return candidate.kind === 'method' &&
				candidate.name.startsWith(prefix) &&
				/^[A-Za-z_$][\w$]*$/.test(suffix)
				? [{ module: ownerModule, symbol: candidate }]
				: [];
		});
	};
	const local = methodEntries(module, symbol);
	if (local.length > 0) return local;

	const aliasTarget = symbol.signature.match(/^type\s+[^=]+?=\s*([A-Za-z0-9_./@]+)/)?.[1];
	const targetHref =
		aliasTarget && Object.hasOwn(symbol.typeLinks, aliasTarget)
			? symbol.typeLinks[aliasTarget]
			: undefined;
	const target = targetHref ? referenceEntriesByHref.get(targetHref) : undefined;
	return target ? methodEntries(target.module, target.symbol) : [];
}

export function groupReferenceModules(
	modules: readonly ReferenceModule[] = referenceModules
): Map<string, ReferenceModule[]> {
	return modules.reduce<Map<string, ReferenceModule[]>>((groups, module) => {
		const items = groups.get(module.group) ?? [];
		items.push(module);
		groups.set(module.group, items);
		return groups;
	}, new Map());
}

export function groupReferenceSymbols(module: ReferenceModule): Map<string, ReferenceSymbol[]> {
	const discoveredGroups = module.symbols.reduce<Map<string, ReferenceSymbol[]>>(
		(groups, symbol) => {
			const items = groups.get(symbol.group) ?? [];
			items.push(symbol);
			groups.set(symbol.group, items);
			return groups;
		},
		new Map()
	);

	return new Map(
		module.groupOrder.map((group) => [group, discoveredGroups.get(group) ?? []] as const)
	);
}

export function referenceGroupID(group: string): string {
	return group.toLowerCase().replaceAll(/[^a-z0-9]+/g, '-');
}

export const riduSymbols = findReferenceModule('ridu')?.symbols ?? [];
