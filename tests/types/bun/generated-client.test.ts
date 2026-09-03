import { describe, expect, it } from "bun:test";

import { createClient } from "../../../testdata/generated/ridu.generated";

describe("generated application client", () => {
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
