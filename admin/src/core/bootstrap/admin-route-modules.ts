// Keep the exports, not a second import-promise cache: the browser already
// deduplicates imports. Route wrappers need these synchronously on first render
// so an already-loaded component does not briefly enter an {#await} placeholder.
const preparedModules = new Map<string, unknown>();

export function preparedAdminModule<Module>(group: string): Module | undefined {
	return preparedModules.get(group) as Module | undefined;
}

export function moduleGroupsForRelativeURL(pathname: string) {
	const segments = pathname.split("/").filter(Boolean);
	if (segments[0] === "collections") {
		if (segments.length === 3 && segments[2] === "upload") return ["bulk-upload"];
		if ((segments.length === 4 || segments.length === 5) && segments[3] === "versions") {
			return ["versions"];
		}
		if (segments.length === 3 && segments[2] !== "trash" && segments[2] !== "upload") {
			return ["document"];
		}
		if (segments.length === 4 && segments[3] === "api") return ["document", "document-api"];
	}
	if (segments[0] === "globals") {
		if ((segments.length === 3 || segments.length === 4) && segments[2] === "versions") {
			return ["versions"];
		}
		if (segments.length === 2) return ["document"];
		if (segments.length === 3 && segments[2] === "api") return ["document", "document-api"];
	}
	if (segments.length === 2 && segments[0] === "account" && segments[1] === "security") {
		return ["date"];
	}
	return [];
}

export async function preloadAdminModuleGroups(groups: readonly string[]) {
	await Promise.all(
		[...new Set(groups)].map(async (group) => {
			if (preparedModules.has(group)) return;
			let module: unknown;
			switch (group) {
				case "bulk-upload":
					module = await import("@admin/features/uploads/bulk-upload-route.svelte");
					break;
				case "document":
					module = await import("@admin/features/documents/document-route.svelte");
					break;
				case "versions":
					module = await import("@admin/features/versions/version-history-route.svelte");
					break;
				case "date":
					module =
						await import("@admin/components/ui/date-value-control/date-value-control.svelte");
					break;
				case "upload-preview":
					module = await import("@admin/features/uploads/upload-control.svelte");
					break;
				case "document-api":
					module = await import("@admin/features/documents/document-api-view.svelte");
					break;
				default:
					return;
			}
			preparedModules.set(group, module);
		})
	);
}
