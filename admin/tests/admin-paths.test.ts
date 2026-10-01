import { describe, expect, test } from "bun:test";

import {
	adminLoginPath,
	adminLayoutRouteBehavior,
	adminPathSegments,
	adminRedirectFromSearch,
	adminRoutePatterns,
	documentIDFromAdminPath,
	documentPath,
	humanizeAdminPathSegment,
	parseAdminVersionRevision,
	adminPathResource,
} from "@admin/core/routing/admin-paths";

describe("admin authentication redirects", () => {
	test("round-trips safe admin paths with query and hash state", () => {
		const requested = "/collections/posts?draft=true#title";
		expect(adminLoginPath(requested)).toBe(
			`${adminRoutePatterns.login}?redirect=${encodeURIComponent(requested)}`
		);
		expect(adminRedirectFromSearch(`?redirect=${encodeURIComponent(requested)}`)).toBe(requested);
	});

	test("uses the admin home for unsafe or recursive authentication redirects", () => {
		expect(adminLoginPath(adminRoutePatterns.home)).toBe(adminRoutePatterns.login);
		expect(adminRedirectFromSearch("?redirect=https%3A%2F%2Fevil.example")).toBe(
			adminRoutePatterns.home
		);
		expect(adminRedirectFromSearch("?redirect=%2F%2Fevil.example")).toBe(adminRoutePatterns.home);
		expect(adminRedirectFromSearch("?redirect=%2Fcreate-first-user")).toBe(adminRoutePatterns.home);
		expect(adminRedirectFromSearch("?redirect=%2Fforgot-password")).toBe(adminRoutePatterns.home);
	});
});

describe("admin document paths", () => {
	test("decodes document identity exactly once from the browser pathname", () => {
		for (const id of ["folder/item", "%2F", "x%2Fy", "spaces and £"]) {
			const pathname = documentPath("posts", id);
			expect(documentIDFromAdminPath(pathname)).toBe(id);
			expect(documentIDFromAdminPath(pathname.replace("collections", "COLLECTIONS"))).toBe(id);
			expect(documentIDFromAdminPath(pathname.replace("collections", "%63ollections"))).toBe(id);
		}
	});

	test("does not infer an ID outside a collection document route", () => {
		expect(documentIDFromAdminPath("/collections/posts")).toBeUndefined();
		expect(documentIDFromAdminPath("/globals/site-settings")).toBeUndefined();
		expect(documentIDFromAdminPath("/collections/posts/%zz")).toBeUndefined();
	});
});

describe("admin path labels", () => {
	test("decodes valid segments and preserves malformed escape sequences", () => {
		expect(adminPathSegments("/reports/sales%20report")).toEqual(["reports", "sales report"]);
		expect(adminPathSegments("/reports/%E0%A4%A")).toEqual(["reports", "%E0%A4%A"]);
	});

	test("humanizes hyphens and underscores consistently", () => {
		expect(humanizeAdminPathSegment("sales_report-history", "en")).toBe("Sales report history");
	});

	test("accepts only positive safe integer version revisions", () => {
		expect(parseAdminVersionRevision("12")).toBe(12);
		for (const value of [undefined, "", "0", "-1", "1.5", "nope", String(2 ** 53)]) {
			expect(parseAdminVersionRevision(value)).toBeUndefined();
		}
	});
});

describe("admin layout route behavior", () => {
	for (const [pathname, expected] of [
		["/", { ownsViewport: false, waitsForPage: false }],
		["/collections/posts", { ownsViewport: false, waitsForPage: true }],
		["/collections/posts/trash", { ownsViewport: false, waitsForPage: true }],
		["/collections/posts/upload", { ownsViewport: false, waitsForPage: false }],
		["/collections/posts/create", { ownsViewport: true, waitsForPage: true }],
		["/collections/posts/post-1", { ownsViewport: true, waitsForPage: true }],
		["/collections/posts/post-1/api", { ownsViewport: true, waitsForPage: true }],
		["/collections/posts/post-1/versions", { ownsViewport: false, waitsForPage: false }],
		["/globals/site-settings", { ownsViewport: true, waitsForPage: true }],
		["/globals/site-settings/api", { ownsViewport: true, waitsForPage: true }],
		["/globals/site-settings/versions", { ownsViewport: false, waitsForPage: false }],
		["/plugin/reports", { ownsViewport: false, waitsForPage: false }],
	] as const) {
		test(pathname, () => {
			expect(adminLayoutRouteBehavior(pathname)).toEqual(expected);
		});
	}
});

describe("adminPathResource", () => {
	test("names the collection or global a page belongs to", () => {
		expect(adminPathResource("/collections/post-stats")).toEqual({
			kind: "collection",
			slug: "post-stats",
		});
		expect(adminPathResource("/collections/post-stats/abc/versions")).toEqual({
			kind: "collection",
			slug: "post-stats",
		});
		expect(adminPathResource("/globals/sync-state/api")).toEqual({
			kind: "global",
			slug: "sync-state",
		});
		expect(adminPathResource("/collections/a%20b")).toEqual({ kind: "collection", slug: "a b" });
	});

	test("matches the router's case-insensitive sections without changing resource slugs", () => {
		expect(adminPathResource("/Collections/post-stats/abc")).toEqual({
			kind: "collection",
			slug: "post-stats",
		});
		expect(adminPathResource("/GLOBALS/Sync-State/api")).toEqual({
			kind: "global",
			slug: "Sync-State",
		});
	});

	test("ignores pages that belong to no resource", () => {
		expect(adminPathResource("/")).toBeUndefined();
		expect(adminPathResource("/collections")).toBeUndefined();
		expect(adminPathResource("/account/security")).toBeUndefined();
	});
});
