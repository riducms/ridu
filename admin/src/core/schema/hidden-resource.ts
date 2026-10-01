import type { SchemaManifest } from "@riducms/protocol";

/** Whether the config hides a collection or global, which then has no admin pages of its own. */
export function isHiddenAdminResource(
	manifest: SchemaManifest | undefined,
	kind: "collection" | "global",
	slug: string
): boolean {
	const resources = kind === "collection" ? manifest?.collections : manifest?.globals;
	return resources?.find((resource) => resource.slug === slug)?.admin.hidden === true;
}
