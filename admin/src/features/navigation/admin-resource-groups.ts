import type { SchemaCollection } from "@riducms/protocol";

export interface AdminResourceGroup {
	key: string;
	label: string;
	translations?: Readonly<Record<string, string>>;
	collections: readonly SchemaCollection[];
	globals: readonly SchemaCollection[];
}

interface MutableAdminResourceGroup {
	key: string;
	label: string;
	translations?: Readonly<Record<string, string>>;
	collections: SchemaCollection[];
	globals: SchemaCollection[];
}

export function groupAdminResources(
	collections: readonly SchemaCollection[],
	globals: readonly SchemaCollection[],
	fallbackLabels = { collections: "Collections", globals: "Globals" }
): readonly AdminResourceGroup[] {
	const groups = new Map<string, MutableAdminResourceGroup>();

	for (const collection of collections) {
		addResource(
			groups,
			collection.admin.group,
			collection.admin.groupTranslations,
			fallbackLabels.collections,
			"collections",
			collection
		);
	}
	for (const global of globals) {
		addResource(
			groups,
			global.admin.group,
			global.admin.groupTranslations,
			fallbackLabels.globals,
			"globals",
			global
		);
	}

	return [...groups.values()];
}

function addResource(
	groups: Map<string, MutableAdminResourceGroup>,
	configuredGroup: string | undefined,
	translations: Readonly<Record<string, string>> | undefined,
	fallbackGroup: string,
	kind: "collections" | "globals",
	resource: SchemaCollection
) {
	const label = configuredGroup?.trim() || fallbackGroup;
	const key = (configuredGroup?.trim() || kind).toLowerCase();
	let group = groups.get(key);
	if (group === undefined) {
		group = {
			key,
			label,
			...(translations === undefined ? {} : { translations }),
			collections: [],
			globals: [],
		};
		groups.set(key, group);
	}
	group[kind].push(resource);
}
