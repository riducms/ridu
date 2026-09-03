import { describe, expect, test } from "bun:test";

import {
	adminLoginPath,
	adminRedirectFromSearch,
	adminRoutePatterns,
	documentIDFromAdminPath,
	documentPath,
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
		}
	});

	test("does not infer an ID outside a collection document route", () => {
		expect(documentIDFromAdminPath("/collections/posts")).toBeUndefined();
		expect(documentIDFromAdminPath("/globals/site-settings")).toBeUndefined();
		expect(documentIDFromAdminPath("/collections/posts/%zz")).toBeUndefined();
	});
});
