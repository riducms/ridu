import { describe, expect, test } from "bun:test";
import { createAdminI18n, en } from "@riducms/translations";
import {
	versionDate,
	versionListPage,
	versionStatus,
} from "@admin/features/versions/version-history";
import type { AdminVersion } from "@admin/core/api/admin-client";

const versions = Array.from({ length: 26 }, (_, index): AdminVersion => ({
	ID: `doc:${index + 1}`,
	DocumentID: "doc",
	Revision: index + 1,
	Status: index === 25 ? "draft" : "published",
	Snapshot: { id: "doc" },
	CreatedAt: new Date(Date.UTC(2026, 8, index + 1)).toISOString(),
}));

describe("version list", () => {
	test("sorts, pages and normalizes malformed URL controls", () => {
		const view = versionListPage(versions, new URLSearchParams("limit=10&page=2&sort=updatedAt"));
		expect(view.docs.map((version) => version.Revision)).toEqual([
			11, 12, 13, 14, 15, 16, 17, 18, 19, 20,
		]);
		expect(view.pagination).toEqual({
			page: 2,
			limit: 10,
			totalDocs: 26,
			totalPages: 3,
			hasPrevPage: true,
			hasNextPage: true,
		});
		expect(
			versionListPage(versions, new URLSearchParams("limit=-3&page=999")).pagination.page
		).toBe(3);
		expect(versionListPage(versions, new URLSearchParams("page=1.5")).docs[0]?.Revision).toBe(26);
	});

	test("does not label an old publication as live when the current document is a draft", () => {
		const document = { id: "doc", _revision: 26, _status: "draft" as const };
		expect(versionStatus(versions[25]!, document)).toBe("currentDraft");
		expect(versionStatus(versions[24]!, document)).toBe("previouslyPublished");
		expect(versionStatus(versions[24]!, { ...document, _revision: 25, _status: "published" })).toBe(
			"currentlyPublished"
		);
	});

	test("formats the reference date with configured timezone and English ordinals", () => {
		const i18n = createAdminI18n({ languages: [en], language: "en", timeZone: "UTC" });
		expect(versionDate("2026-09-21T08:35:00Z", i18n)).toBe("September 21st 2026, 8:35 AM");
		expect(versionDate("2026-09-12T08:35:00Z", i18n)).toBe("September 12th 2026, 8:35 AM");
	});
});
