import type { FieldFormResource } from "@riducms/plugin";

export function generationSnapshotToken(document: Readonly<Record<string, unknown>>): string {
	return JSON.stringify(document);
}

export function generationScopeToken(
	resource: FieldFormResource | undefined,
	locale: string | undefined
): string {
	return JSON.stringify({
		collection: resource?.collection ?? "",
		global: resource?.global === true,
		id: resource?.id ?? "",
		locale: locale ?? "",
	});
}
