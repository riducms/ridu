import { describe, expect, it } from "bun:test";
import { readFile } from "node:fs/promises";
import ts from "typescript";

import { createClient } from "../../../testdata/generated/ridu.generated";

describe("generated application client", () => {
	it("contains no explicit any keywords in its public contract", async () => {
		const source = await readFile(
			new URL("../../../testdata/generated/ridu.generated.ts", import.meta.url),
			"utf8"
		);
		expect(explicitAnyLocations(source)).toEqual([]);

		const compactAny = "export interface Unsafe { value:any; nested:Record<string,any> }";
		expect(explicitAnyLocations(compactAny)).toHaveLength(2);
		expect(
			explicitAnyLocations(
				'export interface Company { company: string; label: "any" } // any in a comment'
			)
		).toEqual([]);
	});

	it("binds its exact config to the Fetch SDK", async () => {
		const client = createClient({
			baseURL: "https://cms.example.test",
			fetch: async () =>
				Response.json({
					doc: {
						id: "post_1",
						createdAt: "2026-08-04T10:00:00Z",
						updatedAt: "2026-08-04T10:00:00Z",
						title: "Bun fixture",
						status: "draft",
						author: null,
						seo: null,
					},
				}),
		});

		const post = await client.create("posts", { title: "Bun fixture" });
		expect(post.id).toBe("post_1");
		expect(post.title).toBe("Bun fixture");
	});
});

function explicitAnyLocations(source: string) {
	const file = ts.createSourceFile(
		"ridu.generated.ts",
		source,
		ts.ScriptTarget.Latest,
		true,
		ts.ScriptKind.TS
	);
	const locations: string[] = [];
	function visit(node: ts.Node) {
		if (node.kind === ts.SyntaxKind.AnyKeyword) {
			const location = file.getLineAndCharacterOfPosition(node.getStart(file));
			locations.push(`${location.line + 1}:${location.character + 1}`);
		}
		ts.forEachChild(node, visit);
	}
	visit(file);
	return locations;
}
