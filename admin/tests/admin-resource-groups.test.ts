import { describe, expect, test } from "bun:test";

import type { SchemaCollection } from "@riducms/protocol";

import { groupAdminResources } from "@admin/features/navigation/admin-resource-groups";

function resource(slug: string, group?: string): SchemaCollection {
	return {
		id: `collection-${slug}`,
		slug,
		labels: { singular: slug, plural: slug },
		admin: group === undefined ? {} : { group },
		capabilities: {
			auth: false,
			upload: false,
			versions: false,
			trash: false,
			locking: false,
		},
		fields: [],
	};
}

describe("admin resource groups", () => {
	test("preserves manifest order and combines collections and globals with the same group", () => {
		const groups = groupAdminResources(
			[resource("posts", "Editorial"), resource("media", "Editorial"), resource("users", "Team")],
			[resource("homepage", "Editorial"), resource("settings", "Settings")]
		);

		expect(groups.map((group) => group.label)).toEqual(["Editorial", "Team", "Settings"]);
		expect(groups[0]?.collections.map((item) => item.slug)).toEqual(["posts", "media"]);
		expect(groups[0]?.globals.map((item) => item.slug)).toEqual(["homepage"]);
	});

	test("keeps ungrouped collections and globals in distinct familiar defaults", () => {
		const groups = groupAdminResources([resource("posts")], [resource("settings")]);

		expect(groups.map((group) => group.label)).toEqual(["Collections", "Globals"]);
		expect(groups[0]?.collections).toHaveLength(1);
		expect(groups[1]?.globals).toHaveLength(1);
	});

	test("trims configured groups and matches them without case sensitivity", () => {
		const groups = groupAdminResources(
			[resource("posts", " Editorial ")],
			[resource("homepage", "editorial")]
		);

		expect(groups).toHaveLength(1);
		expect(groups[0]?.label).toBe("Editorial");
	});
});
