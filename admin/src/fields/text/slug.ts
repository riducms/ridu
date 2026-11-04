import { normalizeSlug } from "@riducms/protocol";

export function slugFollowsSource(slug: unknown, source: unknown) {
	return String(slug ?? "") === normalizeSlug(String(source ?? ""));
}
