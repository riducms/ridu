import { expect, test } from "vitest";
import { resolveAdminConfig } from "@riducms/plugin/admin";
import { preparedCustomView } from "@admin/core/routing/admin-view-selection";
import { SCHEMA_MANIFEST_VERSION, type SchemaCollection } from "@riducms/protocol";

const component = () => ({});
const config = resolveAdminConfig({
	routes: [{ path: "report", component }],
	coreViews: [
		{ key: "lists", surface: "collectionList", component },
		{ key: "posts-list", surface: "collectionList", collection: "posts", component },
		{ key: "create", surface: "collectionCreate", component },
		{ key: "edit", surface: "collectionEdit", component },
		{ key: "global", surface: "global", component },
		{ key: "missing", surface: "notFound", component },
	],
	documentViews: [{ key: "insights", label: "Insights", collection: "posts", component }],
});

test("selects exact owners using router ranking, case-insensitive literals and decoded params", () => {
	for (const [path, key] of [
		["/collections/posts", "posts-list"],
		["/collections/other", "lists"],
		["/COLLECTIONS/posts", "posts-list"],
		["/collections/%70osts", "posts-list"],
		["/collections/posts/CREATE/api", "create"],
		["/collections/posts/a%2Fb/api", "edit"],
		["/collections/posts/trash/api", "edit"],
		["/globals/site/api", "global"],
		["/unknown", "missing"],
		["/report/unknown", "missing"],
	])
		expect(preparedCustomView(config, path!)).toMatchObject({ key });
	expect(preparedCustomView(config, "/REPORT/")).toBe(config.extensions.routes[0]);
	expect(preparedCustomView(config, "/r%65port")).toBe(config.extensions.routes[0]);
	for (const path of [
		"/",
		"/account",
		"/login",
		"/collections/posts/TRASH",
		"/collections/posts/upload",
		"/collections/posts/a/versions",
		"/globals/site/versions/2",
		"/collections/posts/a/insights",
		"/globals/posts/insights",
	])
		expect(preparedCustomView(config, path)).toBeUndefined();
});

test("selects not-found for hidden resources before resource replacement or tab matching", () => {
	const resource: SchemaCollection = {
		id: "posts",
		slug: "posts",
		labels: { singular: "Post", plural: "Posts" },
		admin: { hidden: true },
		capabilities: { auth: false, upload: false, versions: false, trash: false, locking: false },
		fields: [],
	};
	const manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: { name: "Hidden resources" },
		collections: [resource],
		globals: [{ ...resource, id: "site", slug: "site" }],
		plugins: [],
	};
	for (const path of [
		"/Collections/posts",
		"/collections/posts/create",
		"/collections/posts/a/insights",
		"/globals/site/api",
		"/GLOBALS/site/versions/2",
	]) {
		expect(preparedCustomView(config, path, manifest)).toMatchObject({ key: "missing" });
	}
});
