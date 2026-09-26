import type { AdminDocument, AdminVersion } from "@admin/core/api/admin-client";
import type { AdminI18n } from "@riducms/translations";
import { formatAdminDateTime } from "@admin/core/i18n/format-admin-date-time";
import { listPageSizes, type ListPageSize } from "@admin/features/collections/list-workspace";

export function versionStatus(version: AdminVersion, document: AdminDocument | undefined) {
	const current = version.Revision === document?._revision;
	if (version.Status === "draft") return current ? "currentDraft" : "draft";
	return current && document?._status === "published"
		? "currentlyPublished"
		: "previouslyPublished";
}

export function versionDate(value: string, i18n: AdminI18n) {
	return formatAdminDateTime(value, i18n);
}

export function versionListPage(versions: readonly AdminVersion[], search: URLSearchParams) {
	const limitValue = Number(search.get("limit"));
	const limit: ListPageSize = listPageSizes.find((size) => size === limitValue) ?? 10;
	const totalPages = Math.max(1, Math.ceil(versions.length / limit));
	const requested = Number(search.get("page"));
	const page = Math.min(
		totalPages,
		Number.isSafeInteger(requested) && requested > 0 ? requested : 1
	);
	const sort = search.get("sort") === "updatedAt" ? "updatedAt" : "-updatedAt";
	const ordered = [...versions].sort((a, b) => {
		const difference =
			new Date(a.CreatedAt).valueOf() - new Date(b.CreatedAt).valueOf() || a.Revision - b.Revision;
		return sort === "updatedAt" ? difference : -difference;
	});
	return {
		docs: ordered.slice((page - 1) * limit, page * limit),
		sort,
		pagination: {
			page,
			limit,
			totalDocs: versions.length,
			totalPages,
			hasPrevPage: page > 1,
			hasNextPage: page < totalPages,
		},
	};
}
