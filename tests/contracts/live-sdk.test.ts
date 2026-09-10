import { describe, expect, it } from "bun:test";

import { createClient } from "../../testdata/generated/ridu.generated";

const liveURL = Bun.env.RIDU_LIVE_URL;
const live = liveURL ? describe : describe.skip;

live("generated SDK against the real Go REST server", () => {
	it("matches the create/list/find/update/delete behavior", async () => {
		const client = createClient({ baseURL: liveURL ?? "http://127.0.0.1" });
		const author = await client.create("authors", { name: "Ada" });
		const created = await client.create("posts", {
			title: "Live contract",
			author: author.id,
			seo: { description: "Through Fetch" },
		});
		expect(created.status).toBe("draft");
		expect((await client.find("posts", created.id)).title).toBe("Live contract");

		const page = await client.list("posts", {
			where: { title: { equals: "Live contract" } },
			page: 1,
			limit: 5,
		});
		expect(page.pagination.totalDocs).toBe(1);
		expect(page.docs[0]?.id).toBe(created.id);

		const updated = await client.update("posts", created.id, { status: "published" });
		expect(updated.status).toBe("published");
		expect((await client.delete("posts", created.id)).deleted).toBe(true);
	});

	it("rejects secret predicates, counts and ordering through generated methods", async () => {
		const client = createClient({ baseURL: liveURL ?? "http://127.0.0.1" });
		const created = await client.create("posts", {
			title: "Private query",
			seo: { description: "secret-sapphire" },
		});
		expect(created.seo?.description).toBeUndefined();
		try {
			for (const fragment of ["sapphire", "missing"]) {
				const where = { "seo.description": { contains: fragment } };
				await expect(client.list("posts", { where })).rejects.toMatchObject({
					code: "access_denied",
					status: 403,
				});
				await expect(client.count("posts", { where })).rejects.toMatchObject({
					code: "access_denied",
					status: 403,
				});
			}
			for (const sort of ["seo.description", "-seo.description"] as const) {
				await expect(client.list("posts", { sort: [sort] })).rejects.toMatchObject({
					code: "access_denied",
					status: 403,
				});
			}
			expect(
				(await client.count("posts", { where: { title: { equals: "Private query" } } })).totalDocs
			).toBe(1);
		} finally {
			await client.delete("posts", created.id);
		}
	});

	it("dispatches root and collection custom endpoints through the raw SDK transport", async () => {
		const client = createClient({ baseURL: liveURL ?? "http://127.0.0.1" });
		const root = await client.request("/api/custom/hello%20world", { method: "POST" });
		expect(root.status).toBe(202);
		expect(await root.json()).toEqual({ method: "POST", value: "hello world" });

		const collection = await client.request("/api/collections/posts/custom-summary");
		expect(collection.status).toBe(200);
		expect(await collection.json()).toEqual({ collection: "posts" });

		const manifest = await client.schema();
		expect(manifest.version).toBe(1);
		expect(manifest.application.endpoints?.[0]).toMatchObject({
			method: "POST",
			path: "/custom/:value",
		});
		expect(manifest.collections.find(({ slug }) => slug === "posts")?.endpoints?.[0]).toMatchObject(
			{
				method: "GET",
				path: "/custom-summary",
			}
		);
	});

	it("carries an authenticated session into a raw custom endpoint", async () => {
		const client = createClient({ baseURL: liveURL ?? "http://127.0.0.1" });
		const login = await client.request("/api/auth/users/login", {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify({ email: "live@riducms.test", password: "live-password" }),
		});
		expect(login.status).toBe(200);
		const cookie = login.headers.get("set-cookie")?.split(";", 1)[0];
		expect(cookie).toStartWith("ridu_session=");

		const identity = await client.request("/api/whoami", {
			headers: { cookie: cookie ?? "" },
		});
		expect(identity.status).toBe(200);
		expect(await identity.json()).toMatchObject({ collection: "users" });
	});
});
