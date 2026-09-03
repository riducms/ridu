import registryFile from './module-registry.json';
import type { ReferenceLanguage } from '../types';

export interface GoReferenceSource {
	kind: 'go';
	pattern: string;
}

export interface TypeScriptReferenceEntrypoint {
	/** Public package or export-map specifier used in declaration IDs and exact aliases. */
	specifier: string;
	path: string;
	tsconfig: string;
	/** The surface is an alternate export path for declarations already present in this module. */
	aliasesOnly?: boolean;
}

export interface TypeScriptReferenceSource {
	kind: 'typescript';
	packageJSON: string;
	entrypoints: TypeScriptReferenceEntrypoint[];
}

export interface CLIReferenceSource {
	kind: 'cli';
	helpSnapshots: string[];
	records: string;
}

export type ReferenceModuleSource =
	GoReferenceSource | TypeScriptReferenceSource | CLIReferenceSource;

export interface ReferenceModuleRegistration {
	id: string;
	name: string;
	slug: string;
	packageName: string;
	language: ReferenceLanguage;
	group: string;
	summary: string;
	groupOrder: string[];
	sources: ReferenceModuleSource[];
	records?: string;
	symbolOrder: string[];
}

interface ReferenceModuleRegistryFile {
	schemaVersion: 1;
	modules: ReferenceModuleRegistration[];
}

const registry = registryFile as ReferenceModuleRegistryFile;
if (registry.schemaVersion !== 1) throw new Error('Unsupported reference module registry version');

const moduleIDs = new Set<string>();
const moduleSlugs = new Set<string>();
for (const module of registry.modules) {
	if (moduleIDs.has(module.id)) throw new Error(`duplicate reference module ID ${module.id}`);
	if (moduleSlugs.has(module.slug))
		throw new Error(`duplicate reference module slug ${module.slug}`);
	moduleIDs.add(module.id);
	moduleSlugs.add(module.slug);
}

/** Reviewed registry for every public Go package, npm export map, Svelte surface, and CLI record. */
export const referenceModuleRegistry: readonly ReferenceModuleRegistration[] = registry.modules;
