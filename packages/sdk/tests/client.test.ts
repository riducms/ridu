import { describe, expect, it } from "bun:test";

import { createClient, RiduError, type ClientOptions, type RiduConfigShape } from "../src";

interface TestConfig extends RiduConfigShape {
	collections: {
		posts: {
			auth: true;
			upload: false;
			versions: true;
			trash: true;
			output: { id: string; title: string; _status?: "draft" | "published" };
			create: { title: string };
			update: { title?: string };
			where: { title?: { equals?: string } };
			select: { title?: boolean };
			populate: Record<never, never>;
		};
		media: {
			auth: false;
			upload: true;
			versions: false;
			trash: false;
			output: { id: string; alt: string; filename: string; focalX?: number; focalY?: number };
			create: { alt: string };
			update: { alt?: string };
			where: { alt?: { equals?: string } };
			select: { alt?: boolean; filename?: boolean };
			populate: Record<never, never>;
		};
	};
	globals: {
		"site-settings": {
			versions: true;
			output: { id: string; siteName: string; _revision: number };
			update: { siteName?: string };
			select: { siteName?: boolean };
			populate: Record<never, never>;
		};
	};
}

describe("Fetch client", () => {
	it("routes plugin requests through the configured client transport", async () => {
		let captured: Request | undefined;
		const controller = new AbortController();
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test/root/..",
			headers: { "x-default": "configured" },
			middleware: [
				async (request, next) => {
					const headers = new Headers(request.headers);
					headers.set("x-middleware", "yes");
					return next(new Request(request, { headers }));
				},
			],
			fetch: async (request) => {
				captured = request as Request;
				return Response.json({ result: "Generated" });
			},
		});

		const result = await client.requestPlugin<{ result: string }>(
			"seo",
			"generate-title",
			{ document: { title: "Draft" } },
			{ signal: controller.signal, headers: { "x-request": "request" } }
		);
		expect(result.result).toBe("Generated");
		expect(captured?.url).toBe("https://cms.example.test/api/plugins/seo/generate-title");
		expect(captured?.signal).toBe(controller.signal);
		expect(captured?.headers.get("x-default")).toBe("configured");
		expect(captured?.headers.get("x-request")).toBe("request");
		expect(captured?.headers.get("x-middleware")).toBe("yes");
		expect(await captured?.json()).toEqual({ document: { title: "Draft" } });
	});

	it("rejects plugin namespace traversal before dispatch", async () => {
		let calls = 0;
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async () => {
				calls++;
				return Response.json({ result: "unexpected" });
			},
		});
		await expect(client.requestPlugin("seo", "../richtext/action", {})).rejects.toBeInstanceOf(
			TypeError
		);
		await expect(client.requestPlugin("../seo", "generate-title", {})).rejects.toBeInstanceOf(
			TypeError
		);
		expect(calls).toBe(0);
	});

	it("exposes a same-origin raw request escape hatch for custom endpoints", async () => {
		let captured: Request | undefined;
		const controller = new AbortController();
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test/nested/base",
			headers: { "x-default": "configured" },
			middleware: [
				async (request, next) =>
					next(
						new Request(request, {
							headers: { ...Object.fromEntries(request.headers), "x-middleware": "yes" },
						})
					),
			],
			fetch: async (request) => {
				captured = request as Request;
				return new Response("accepted", { status: 202, headers: { "content-type": "text/plain" } });
			},
		});

		const response = await client.request(
			"/api/collections/posts/post-1/tracking?notify=true",
			{ method: "POST", body: "raw-body", headers: { "content-type": "text/plain" } },
			{ signal: controller.signal, headers: { "x-request": "request" } }
		);
		expect(response.status).toBe(202);
		expect(await response.text()).toBe("accepted");
		expect(captured?.url).toBe(
			"https://cms.example.test/api/collections/posts/post-1/tracking?notify=true"
		);
		expect(captured?.signal).toBe(controller.signal);
		expect(captured?.headers.get("content-type")).toBe("text/plain");
		expect(captured?.headers.get("x-default")).toBe("configured");
		expect(captured?.headers.get("x-request")).toBe("request");
		expect(captured?.headers.get("x-middleware")).toBe("yes");
		expect(await captured?.text()).toBe("raw-body");
	});

	it("returns raw error responses and rejects origin-changing request paths", async () => {
		let calls = 0;
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async () => {
				calls++;
				return Response.json({ error: "custom" }, { status: 418 });
			},
		});
		const response = await client.request("/api/custom");
		expect(response.status).toBe(418);
		await expect(client.request("https://evil.example.test/api/custom")).rejects.toBeInstanceOf(
			TypeError
		);
		await expect(client.request("//evil.example.test/api/custom")).rejects.toBeInstanceOf(
			TypeError
		);
		await expect(client.request("/api/custom#hidden")).rejects.toBeInstanceOf(TypeError);
		await expect(client.request("/api/custom#")).rejects.toBeInstanceOf(TypeError);
		expect(calls).toBe(1);
	});

	it("uses browser cookie credentials, merged headers, and typed documents", async () => {
		let captured: Request | undefined;
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test/",
			headers: async () => ({ "x-default": "default" }),
			fetch: async (request) => {
				captured = request as Request;
				return Response.json({ doc: { id: "post_1", title: "Hello" } });
			},
		});

		const post = await client.create(
			"posts",
			{ title: "Hello" },
			{
				headers: { "x-request": "request" },
			}
		);
		expect(post.title).toBe("Hello");
		expect(captured?.credentials).toBe("include");
		expect(captured?.headers.get("x-default")).toBe("default");
		expect(captured?.headers.get("x-request")).toBe("request");
		expect(captured?.headers.get("content-type")).toBe("application/json");
		expect(captured?.url).toBe("https://cms.example.test/api/collections/posts");
	});

	it("uses a request-local SvelteKit-style fetch implementation", async () => {
		let calls = 0;
		const requestLocalFetch: NonNullable<ClientOptions["fetch"]> = async (request) => {
			calls++;
			expect(request).toBeInstanceOf(Request);
			return Response.json({ doc: { id: "post_2", title: "Request local" } });
		};
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: requestLocalFetch,
		});

		const post = await client.find("posts", "post_2");
		expect(post.title).toBe("Request local");
		expect(calls).toBe(1);
	});

	it("mints and consumes document-scoped preview capabilities", async () => {
		const requests: Request[] = [];
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			credentials: "omit",
			fetch: async (request) => {
				const input = request as Request;
				requests.push(input);
				if (input.url.endsWith("/api/preview/token/revoke")) {
					return Response.json({ success: true });
				}
				if (input.url.endsWith("/token")) {
					return Response.json({
						previewToken: {
							token: "scoped-secret",
							resource: "collection",
							slug: "posts",
							documentId: "post_2",
							expiresAt: "2030-01-02T03:04:05Z",
						},
					});
				}
				return Response.json({ doc: { id: "post_2", title: "Private draft" } });
			},
		});

		const capability = await client.createPreviewToken("posts", "post_2");
		const draft = await client.preview("posts", "post_2", capability.token, {
			headers: { "x-renderer": "ssr" },
		});
		await client.revokePreviewToken(capability.token, { keepalive: true });
		expect(capability.documentId).toBe("post_2");
		expect(draft.title).toBe("Private draft");
		expect(requests[0]?.method).toBe("POST");
		expect(requests[0]?.url).toBe(
			"https://cms.example.test/api/preview/collections/posts/post_2/token"
		);
		expect(requests[1]?.headers.get("authorization")).toBe("Bearer scoped-secret");
		expect(requests[1]?.headers.get("x-renderer")).toBe("ssr");
		expect(requests[1]?.url).toBe("https://cms.example.test/api/preview/collections/posts/post_2");
		expect(requests[2]?.url).toBe("https://cms.example.test/api/preview/token/revoke");
		expect(await requests[2]?.json()).toEqual({ token: "scoped-secret" });
	});

	it("lets the browser set the multipart boundary for uploads", async () => {
		let captured: Request | undefined;
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				captured = request as Request;
				return Response.json({
					doc: { id: "media_1", alt: "Diagram", filename: "diagram.png" },
				});
			},
		});

		const media = await client.upload("media", new Blob(["image"], { type: "image/png" }), {
			data: { alt: "Diagram" },
			locale: "fr",
		});
		expect(media.filename).toBe("diagram.png");
		expect(captured?.headers.get("content-type")).toStartWith("multipart/form-data; boundary=");
		const form = await captured?.formData();
		expect(form?.get("data")).toBe('{"alt":"Diagram"}');
		expect(form?.get("file")).toBeInstanceOf(Blob);
		expect(new URL(captured?.url ?? "").searchParams.get("locale")).toBe("fr");
	});

	it("creates remote uploads through the SSRF-guarded route", async () => {
		let requestBody: unknown;
		let requestPath = "";
		let requestLocale = "";
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const input = request as Request;
				requestPath = new URL(input.url).pathname;
				requestLocale = new URL(input.url).searchParams.get("locale") ?? "";
				requestBody = await input.json();
				return Response.json({ doc: { id: "media_remote", alt: "Remote" } });
			},
		});
		expect(
			(
				await client.uploadFromURL("media", "https://cdn.example.test/image.png", {
					data: { alt: "Remote" },
					locale: "fr",
				})
			).id
		).toBe("media_remote");
		expect(requestPath).toBe("/api/collections/media/remote-upload");
		expect(requestLocale).toBe("fr");
		expect(requestBody).toEqual({
			url: "https://cdn.example.test/image.png",
			data: { alt: "Remote" },
		});
	});

	it("regenerates upload image sizes around a focal point", async () => {
		let captured: Request | undefined;
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				captured = request as Request;
				return Response.json({ doc: { id: "media_1", focalX: 20, focalY: 80 } });
			},
		});
		const media = await client.updateUploadImage(
			"media",
			"media_1",
			{ focalX: 20, focalY: 80, cropX: 10, cropY: 15, cropWidth: 70, cropHeight: 60 },
			{ revision: 4 }
		);
		expect(media.focalX).toBe(20);
		expect(captured?.method).toBe("PATCH");
		expect(captured?.url).toBe("https://cms.example.test/api/collections/media/media_1/image");
		expect(captured?.headers.get("if-match")).toBe('"4"');
		expect(await captured?.json()).toEqual({
			focalX: 20,
			focalY: 80,
			cropX: 10,
			cropY: 15,
			cropWidth: 70,
			cropHeight: 60,
		});
	});

	it("forwards abort signals", async () => {
		const controller = new AbortController();
		controller.abort();
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				expect((request as Request).signal.aborted).toBe(true);
				throw new DOMException("aborted", "AbortError");
			},
		});
		await expect(
			client.find("posts", "post_1", { signal: controller.signal })
		).rejects.toMatchObject({
			name: "AbortError",
		});
	});

	it("rejects failed operations with structured RiduError", async () => {
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async () =>
				Response.json(
					{
						error: {
							code: "validation",
							status: 422,
							message: "Invalid document",
							requestId: "req_1",
							issues: [{ code: "required", path: "title", message: "Title is required" }],
						},
					},
					{ status: 422 }
				),
		});

		try {
			await client.create("posts", { title: "" });
			expect.unreachable();
		} catch (error) {
			expect(error).toBeInstanceOf(RiduError);
			if (error instanceof RiduError) {
				expect(error.code).toBe("validation");
				expect(error.requestId).toBe("req_1");
				expect(error.issues[0]?.path).toBe("title");
			}
		}
	});

	it("serializes typed list controls and runs middleware", async () => {
		let captured: Request | undefined;
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			middleware: [
				async (request, next) => {
					const headers = new Headers(request.headers);
					headers.set("x-middleware", "yes");
					return next(new Request(request, { headers }));
				},
			],
			fetch: async (request) => {
				captured = request as Request;
				return Response.json({
					docs: [],
					pagination: {
						page: 1,
						limit: 10,
						totalDocs: 0,
						totalPages: 0,
						hasNextPage: false,
						hasPrevPage: false,
					},
				});
			},
		});
		await client.list("posts", {
			page: 1,
			limit: 10,
			depth: 2,
			where: { title: { equals: "Hello" } },
			select: { title: true },
			sort: ["title", "-id"],
			trash: true,
		});
		expect(captured?.headers.get("x-middleware")).toBe("yes");
		const url = new URL(captured?.url ?? "");
		expect(url.searchParams.get("where")).toBe('{"title":{"equals":"Hello"}}');
		expect(url.searchParams.get("depth")).toBe("2");
		expect(url.searchParams.getAll("sort")).toEqual(["title", "-id"]);
		expect(url.searchParams.get("trash")).toBe("true");
	});

	it("serializes depth for individual document reads", async () => {
		let captured: Request | undefined;
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				captured = request as Request;
				return Response.json({ doc: { id: "post_1", title: "Hello" } });
			},
		});

		await client.find("posts", "post_1", { depth: 3 });

		const url = new URL(captured?.url ?? "");
		expect(url.pathname).toBe("/api/collections/posts/post_1");
		expect(url.searchParams.get("depth")).toBe("3");
	});

	it("uses explicit restore and permanent-delete endpoints", async () => {
		const calls: string[] = [];
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const current = request as Request;
				calls.push(`${current.method} ${new URL(current.url).pathname}`);
				if (current.method === "POST") {
					return Response.json({ doc: { id: "post_1", title: "Restored" } });
				}
				return Response.json({ id: "post_1", deleted: true });
			},
		});
		expect((await client.restoreDeleted("posts", "post_1")).title).toBe("Restored");
		expect((await client.deletePermanent("posts", "post_1")).deleted).toBe(true);
		expect(calls).toEqual([
			"POST /api/collections/posts/post_1/restore-deleted",
			"DELETE /api/collections/posts/post_1/permanent",
		]);
	});

	it("serializes locale and fallback controls on reads and writes", async () => {
		const calls: string[] = [];
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const current = request as Request;
				const url = new URL(current.url);
				calls.push(`${current.method} ${url.pathname}${url.search}`);
				if (url.pathname.endsWith("/versions")) return Response.json({ versions: [] });
				if (url.pathname.endsWith("/selection")) {
					return Response.json({ items: [], totalDocs: 0 });
				}
				if (url.pathname === "/api/collections/posts") {
					if (current.method === "GET") {
						return Response.json({
							docs: [],
							pagination: {
								page: 1,
								limit: 10,
								totalDocs: 0,
								totalPages: 0,
								hasNextPage: false,
								hasPrevPage: false,
							},
						});
					}
					return Response.json({ doc: { id: "post_1", title: "Bonjour" } });
				}
				return Response.json({ doc: { id: "post_1", title: "Bonjour" } });
			},
		});

		await client.list("posts", { locale: "fr", fallbackLocale: ["en", "de"] });
		await client.find("posts", "post_1", { locale: "all" });
		await client.create(
			"posts",
			{ title: "Bonjour" },
			{
				locale: "fr",
				fallbackLocale: false,
				draft: true,
			}
		);
		await client.update("posts", "post_1", { title: "Salut" }, { locale: "fr" });
		await client.duplicate("posts", "post_1", {}, { locale: "fr" });
		await client.copyLocale("posts", "post_1", { from: "en", to: "fr" }, { revision: 2 });
		await client.global("site-settings", { locale: "ar", fallbackLocale: "en" });
		await client.updateGlobal("site-settings", { siteName: "Réglages" }, { locale: "fr" });
		await client.copyGlobalLocale("site-settings", { from: "en", to: "fr" });
		await client.publishGlobal("site-settings", { locale: "fr" });
		await client.versions("posts", "post_1", { locale: "fr" });
		await client.globalVersions("site-settings", { locale: "all" });
		await client.resolveFilteredSelection("posts", { locale: "fr" });

		expect(calls).toEqual([
			"GET /api/collections/posts?locale=fr&fallback-locale=en%2Cde",
			"GET /api/collections/posts/post_1?locale=all",
			"POST /api/collections/posts?locale=fr&fallback-locale=false&draft=true",
			"PATCH /api/collections/posts/post_1?locale=fr",
			"POST /api/collections/posts/post_1/duplicate?locale=fr",
			"POST /api/collections/posts/post_1/copy-locale",
			"GET /api/globals/site-settings?locale=ar&fallback-locale=en",
			"PATCH /api/globals/site-settings?locale=fr",
			"POST /api/globals/site-settings/copy-locale",
			"POST /api/globals/site-settings/publish?locale=fr",
			"GET /api/collections/posts/post_1/versions?locale=fr",
			"GET /api/globals/site-settings/versions?locale=all",
			"POST /api/access/collections/posts/selection?locale=fr",
		]);
	});

	it("gets individual versions and manages scheduled publishes", async () => {
		const calls: Array<{ method: string; path: string; revision: string | null }> = [];
		const scheduled = {
			id: "publish_1",
			documentId: "post_1",
			expectedRevision: 3,
			runAt: "2030-01-02T03:04:05.000Z",
			attempts: 0,
			createdAt: "2030-01-01T00:00:00.000Z",
		};
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const current = request as Request;
				const url = new URL(current.url);
				const path = url.pathname;
				calls.push({ method: current.method, path, revision: current.headers.get("if-match") });
				if (path.endsWith("/versions/2")) {
					return Response.json({
						version: {
							ID: "version_2",
							DocumentID: "post_1",
							Revision: 2,
							Status: "draft",
							Snapshot: { id: "post_1", title: "Earlier" },
							CreatedAt: scheduled.createdAt,
						},
					});
				}
				if (path.includes("/restore/")) {
					calls.at(-1)!.path += url.search;
					return Response.json({ doc: { id: "post_1", title: "Earlier", _status: "draft" } });
				}
				if (current.method === "POST") return Response.json({ scheduledPublish: scheduled });
				if (current.method === "DELETE") return Response.json({ id: scheduled.id, deleted: true });
				return Response.json({ scheduledPublishes: [scheduled] });
			},
		});

		expect((await client.version("posts", "post_1", 2)).Revision).toBe(2);
		expect((await client.restore("posts", "post_1", 2, { revision: 3, draft: true }))._status).toBe(
			"draft"
		);
		expect(
			(await client.schedulePublish("posts", "post_1", new Date(scheduled.runAt), { revision: 3 }))
				.id
		).toBe("publish_1");
		expect(await client.scheduledPublishes("posts", "post_1")).toHaveLength(1);
		expect((await client.cancelScheduledPublish("posts", "post_1", "publish_1")).deleted).toBe(
			true
		);
		expect(calls).toEqual([
			{ method: "GET", path: "/api/collections/posts/post_1/versions/2", revision: null },
			{
				method: "POST",
				path: "/api/collections/posts/post_1/restore/2?draft=true",
				revision: '"3"',
			},
			{ method: "POST", path: "/api/collections/posts/post_1/schedule", revision: '"3"' },
			{ method: "GET", path: "/api/collections/posts/post_1/schedule", revision: null },
			{
				method: "DELETE",
				path: "/api/collections/posts/post_1/schedule/publish_1",
				revision: null,
			},
		]);
	});

	it("uses typed duplicate and bulk mutation endpoints", async () => {
		const calls: Array<{ method: string; path: string; body: unknown }> = [];
		let localizedBulk = "";
		let emptyTrashQuery = "";
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const current = request as Request;
				const text = await current.clone().text();
				calls.push({
					method: current.method,
					path: new URL(current.url).pathname,
					body: text === "" ? undefined : JSON.parse(text),
				});
				if (new URL(current.url).pathname.endsWith("/bulk") && localizedBulk === "") {
					localizedBulk = new URL(current.url).searchParams.get("locale") ?? "";
				}
				if (current.method === "DELETE" && new URL(current.url).pathname.endsWith("/posts")) {
					emptyTrashQuery = new URL(current.url).search;
				}
				if (new URL(current.url).pathname.endsWith("/duplicate")) {
					return Response.json({ doc: { id: "post_2", title: "Copy" } });
				}
				return Response.json({ docs: [{ id: "post_1", title: "Changed" }] });
			},
		});
		expect((await client.duplicate("posts", "post_1", { title: "Copy" })).id).toBe("post_2");
		expect(
			(await client.bulkUpdate("posts", ["post_1"], { title: "Changed" }, { locale: "fr" }))[0]
				?.title
		).toBe("Changed");
		await client.bulkPublish("posts", ["post_1"]);
		await client.bulkDelete("posts", ["post_1"]);
		await client.bulkRestoreDeleted("posts", ["post_1"]);
		await client.bulkDeletePermanent("posts", ["post_1"]);
		await client.emptyTrash("posts");
		expect(localizedBulk).toBe("fr");
		expect(emptyTrashQuery).toBe("?trash=true");
		expect(calls).toEqual([
			{
				method: "POST",
				path: "/api/collections/posts/post_1/duplicate",
				body: { title: "Copy" },
			},
			{
				method: "POST",
				path: "/api/collections/posts/bulk",
				body: { action: "update", ids: ["post_1"], data: { title: "Changed" } },
			},
			{
				method: "POST",
				path: "/api/collections/posts/bulk",
				body: { action: "publish", ids: ["post_1"] },
			},
			{
				method: "POST",
				path: "/api/collections/posts/bulk",
				body: { action: "delete", ids: ["post_1"] },
			},
			{
				method: "POST",
				path: "/api/collections/posts/bulk",
				body: { action: "restoreDeleted", ids: ["post_1"] },
			},
			{
				method: "POST",
				path: "/api/collections/posts/bulk",
				body: { action: "deletePermanent", ids: ["post_1"] },
			},
			{
				method: "DELETE",
				path: "/api/collections/posts",
				body: undefined,
			},
		]);
	});

	it("sends inverse join deltas through one atomic endpoint", async () => {
		let captured: Request | undefined;
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				captured = request as Request;
				return Response.json({
					doc: { id: "category/1", title: "Category" },
					added: 1,
					removed: 1,
				});
			},
		});
		const result = await client.mutateJoin(
			"posts",
			"category/1",
			"related.posts",
			{
				additions: ["post_2"],
				removals: ["post_1"],
			},
			{ locale: "fr" }
		);
		expect(result).toEqual({
			doc: { id: "category/1", title: "Category" },
			added: 1,
			removed: 1,
		});
		expect(captured?.method).toBe("PATCH");
		expect(captured?.url).toBe(
			"https://cms.example.test/api/collections/posts/category%2F1/joins/related.posts?locale=fr"
		);
		expect(await captured?.json()).toEqual({
			additions: ["post_2"],
			removals: ["post_1"],
		});
	});

	it("uses the stable auth session endpoints", async () => {
		const paths: string[] = [];
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const input = request as Request;
				const url = new URL(input.url);
				paths.push(`${input.method} ${url.pathname}${url.search}`);
				if (url.pathname.endsWith("/bootstrap")) {
					return Response.json({ available: true });
				}
				if (url.pathname.endsWith("/logout") || url.pathname.endsWith("/logout-all")) {
					return Response.json({ loggedOut: true });
				}
				if (url.pathname.endsWith("/sessions") && input.method === "GET") {
					return Response.json({
						sessions: [
							{
								id: "session_1",
								createdAt: "2026-08-05T09:00:00Z",
								lastSeenAt: "2026-08-05T10:00:00Z",
								expiresAt: "2026-08-05T11:00:00Z",
								current: true,
							},
						],
					});
				}
				if (url.pathname.endsWith("/sessions/session_1")) {
					return Response.json({ id: "session_1", deleted: true });
				}
				return Response.json({
					session: {
						id: "session_1",
						collection: "posts",
						user: { id: "post_1", title: "Session user" },
						expiresAt: "2026-08-05T10:00:00Z",
					},
				});
			},
		});
		expect((await client.authBootstrap("posts")).available).toBe(true);
		const login = await client.login("posts", {
			email: "ada@example.test",
			password: "secret-pass",
		});
		expect(login.user.title).toBe("Session user");
		expect(login.collection).toBe("posts");
		expect((await client.session()).expiresAt).toBe("2026-08-05T10:00:00Z");
		expect((await client.refreshSession()).expiresAt).toBe("2026-08-05T10:00:00Z");
		expect((await client.logout()).loggedOut).toBe(true);
		expect((await client.sessions())[0]?.current).toBe(true);
		expect((await client.revokeSession("session_1")).deleted).toBe(true);
		expect((await client.logoutAll()).loggedOut).toBe(true);
		expect(paths).toEqual([
			"GET /api/auth/posts/bootstrap",
			"POST /api/auth/posts/login",
			"GET /api/auth/me",
			"POST /api/auth/refresh",
			"POST /api/auth/logout",
			"GET /api/auth/sessions",
			"DELETE /api/auth/sessions/session_1",
			"POST /api/auth/logout-all",
		]);
	});

	it("creates auth users without mixing credentials into document data", async () => {
		let captured: Request | undefined;
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				captured = request as Request;
				return Response.json({ doc: { id: "post_auth", title: "New account" } });
			},
		});

		const created = await client.createAuthUser(
			"posts",
			{ title: "New account" },
			"correct-horse",
			{
				locale: "fr",
				fallbackLocale: false,
			}
		);
		expect(created.id).toBe("post_auth");
		expect(captured?.url).toBe(
			"https://cms.example.test/api/auth/posts/create-user?locale=fr&fallback-locale=false"
		);
		expect(await captured?.json()).toEqual({
			data: { title: "New account" },
			password: "correct-horse",
		});
	});

	it("exposes recovery, verification, and API key endpoints", async () => {
		const paths: string[] = [];
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const input = request as Request;
				const url = new URL(input.url);
				paths.push(`${input.method} ${url.pathname}`);
				if (url.pathname === "/api/auth/api-keys" && input.method === "POST") {
					return Response.json({
						apiKey: {
							id: "key_1",
							name: "Deploy",
							key: "ridu_key_secret",
							createdAt: "2026-08-13T10:00:00Z",
						},
					});
				}
				if (url.pathname === "/api/auth/api-keys" && input.method === "GET") {
					return Response.json({
						apiKeys: [
							{
								id: "key_1",
								name: "Deploy",
								createdAt: "2026-08-13T10:00:00Z",
								lastUsedAt: "2026-08-13T10:05:00Z",
							},
						],
					});
				}
				if (url.pathname.endsWith("/key_1")) {
					return Response.json({ id: "key_1", deleted: true });
				}
				return Response.json({ success: true });
			},
		});
		expect((await client.requestPasswordReset("posts", "ada@example.test")).success).toBe(true);
		expect((await client.resetPassword("posts", "reset-token", "changed-secret")).success).toBe(
			true
		);
		expect((await client.requestVerification("posts", "ada@example.test")).success).toBe(true);
		expect((await client.verifyEmail("posts", "verify-token")).success).toBe(true);
		expect((await client.changePassword("old-secret", "new-secret")).success).toBe(true);
		expect((await client.createAPIKey({ name: "Deploy" })).key).toBe("ridu_key_secret");
		expect((await client.apiKeys())[0]?.lastUsedAt).toBe("2026-08-13T10:05:00Z");
		expect((await client.revokeAPIKey("key_1")).deleted).toBe(true);
		expect(paths).toEqual([
			"POST /api/auth/posts/forgot-password",
			"POST /api/auth/posts/reset-password",
			"POST /api/auth/posts/request-verification",
			"POST /api/auth/posts/verify",
			"POST /api/auth/change-password",
			"POST /api/auth/api-keys",
			"GET /api/auth/api-keys",
			"DELETE /api/auth/api-keys/key_1",
		]);
	});

	it("uses singleton global routes for reads, updates, drafts, and restores", async () => {
		const paths: string[] = [];
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const url = new URL((request as Request).url);
				paths.push(`${(request as Request).method} ${url.pathname}${url.search}`);
				if (url.pathname.endsWith("/versions")) return Response.json({ versions: [] });
				return Response.json({
					doc: { id: "site-settings", siteName: "Ridu", _revision: 2 },
				});
			},
		});
		expect((await client.global("site-settings", { depth: 2 })).siteName).toBe("Ridu");
		expect(
			(await client.updateGlobal("site-settings", { siteName: "Ridu" }, { revision: 1 }))._revision
		).toBe(2);
		expect(await client.globalVersions("site-settings")).toEqual([]);
		await client.publishGlobal("site-settings", { revision: 2 });
		await client.unpublishGlobal("site-settings", { revision: 3 });
		await client.restoreGlobal("site-settings", 1, { revision: 4, draft: true });
		expect(paths).toEqual([
			"GET /api/globals/site-settings?depth=2",
			"PATCH /api/globals/site-settings",
			"GET /api/globals/site-settings/versions",
			"POST /api/globals/site-settings/publish",
			"POST /api/globals/site-settings/unpublish",
			"POST /api/globals/site-settings/restore/1?draft=true",
		]);
	});

	it("sends edited values through collection and global publish endpoints", async () => {
		const requests: Array<{ path: string; body: unknown; revision: string | null }> = [];
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const current = request as Request;
				requests.push({
					path: new URL(current.url).pathname,
					body: JSON.parse(await current.text()),
					revision: current.headers.get("if-match"),
				});
				return Response.json({ doc: { id: "post_1", title: "Published", _revision: 3 } });
			},
		});

		await client.publishChanges("posts", "post_1", { title: "Published post" }, { revision: 1 });
		await client.publishGlobalChanges(
			"site-settings",
			{ siteName: "Published site" },
			{ revision: 2 }
		);
		expect(requests).toEqual([
			{
				path: "/api/collections/posts/post_1/publish",
				body: { title: "Published post" },
				revision: '"1"',
			},
			{
				path: "/api/globals/site-settings/publish",
				body: { siteName: "Published site" },
				revision: '"2"',
			},
		]);
	});

	it("persists opaque user preferences through the stable preference routes", async () => {
		const requests: string[] = [];
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const input = request as Request;
				const url = new URL(input.url);
				requests.push(`${input.method} ${url.pathname}`);
				if (input.method === "DELETE" && url.pathname === "/api/preferences")
					return Response.json({ success: true });
				if (input.method === "DELETE")
					return Response.json({ id: "collection:posts:presets", deleted: true });
				if (input.method === "PUT") {
					return Response.json(JSON.parse(await input.text()));
				}
				return Response.json({ value: [{ name: "Editorial", view: "hierarchy" }] });
			},
		});
		const key = "collection:posts:presets";
		expect(await client.preference<{ name: string; view: string }[]>(key)).toEqual([
			{ name: "Editorial", view: "hierarchy" },
		]);
		expect(await client.setPreference(key, [{ name: "Editorial" }])).toEqual([
			{ name: "Editorial" },
		]);
		expect((await client.deletePreference(key)).deleted).toBe(true);
		expect((await client.resetPreferences()).success).toBe(true);
		expect(requests).toEqual([
			"GET /api/preferences/collection%3Aposts%3Apresets",
			"PUT /api/preferences/collection%3Aposts%3Apresets",
			"DELETE /api/preferences/collection%3Aposts%3Apresets",
			"DELETE /api/preferences",
		]);
	});

	it("force-unlocks auth accounts through the collection-scoped route", async () => {
		let requested = "";
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const input = request as Request;
				requested = `${input.method} ${new URL(input.url).pathname}`;
				return Response.json({ success: true });
			},
		});
		expect((await client.forceUnlock("posts", "user_1")).success).toBe(true);
		expect(requested).toBe("POST /api/auth/posts/user_1/unlock");
	});

	it("acquires, takes over, inspects, and releases document locks", async () => {
		const requests: Array<{ method: string; path: string; body: unknown }> = [];
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const input = request as Request;
				requests.push({
					method: input.method,
					path: new URL(input.url).pathname,
					body: input.method === "POST" ? await input.clone().json() : undefined,
				});
				if (input.method === "DELETE") return Response.json({ id: "post_1", deleted: true });
				return Response.json({
					lock: {
						documentId: "post_1",
						ownerId: "user_1",
						ownerLabel: "Editor",
						createdAt: "2026-08-14T00:00:00Z",
						updatedAt: "2026-08-14T00:00:00Z",
						expiresAt: "2026-08-14T00:02:00Z",
					},
					owned: true,
					acquired: input.method === "POST",
					canTakeOver: false,
				});
			},
		});
		expect((await client.documentLock("posts", "post_1")).owned).toBe(true);
		expect((await client.acquireDocumentLock("posts", "post_1", true)).acquired).toBe(true);
		expect((await client.releaseDocumentLock("posts", "post_1", { keepalive: true })).deleted).toBe(
			true
		);
		expect(requests).toEqual([
			{
				method: "GET",
				path: "/api/collections/posts/post_1/lock",
				body: undefined,
			},
			{
				method: "POST",
				path: "/api/collections/posts/post_1/lock",
				body: { takeover: true },
			},
			{
				method: "DELETE",
				path: "/api/collections/posts/post_1/lock",
				body: undefined,
			},
		]);
	});

	it("evaluates collection and global access without exposing access rules", async () => {
		const requests: Array<{ path: string; body: unknown }> = [];
		let accessLocale = "";
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				const input = request as Request;
				if (requests.length === 0)
					accessLocale = new URL(input.url).searchParams.get("locale") ?? "";
				requests.push({ path: new URL(input.url).pathname, body: await input.clone().json() });
				return Response.json({
					operations: {
						admin: false,
						create: true,
						read: true,
						readVersions: false,
						update: true,
						delete: false,
						duplicate: true,
						publish: true,
						unpublish: true,
						restoreDeleted: false,
						deletePermanent: false,
						selectAll: true,
					},
					fields: { secret: { read: false, create: false, update: false } },
				});
			},
		});
		expect(
			(
				await client.collectionAccess("posts", {
					id: "post_1",
					data: { title: "Next" },
					locale: "fr",
				})
			).fields.secret?.read
		).toBe(false);
		expect((await client.collectionAccess("posts")).operations.admin).toBe(false);
		expect((await client.globalAccess("site-settings")).operations.update).toBe(true);
		expect(accessLocale).toBe("fr");
		expect(requests).toEqual([
			{
				path: "/api/access/collections/posts",
				body: { id: "post_1", data: { title: "Next" } },
			},
			{ path: "/api/access/collections/posts", body: {} },
			{ path: "/api/access/globals/site-settings", body: {} },
		]);
	});

	it("resolves one typed filtered selection and forwards cancellation", async () => {
		let captured: Request | undefined;
		const access = {
			operations: {
				admin: false,
				create: true,
				read: true,
				readVersions: false,
				update: true,
				delete: false,
				duplicate: true,
				publish: true,
				unpublish: true,
				restoreDeleted: false,
				deletePermanent: false,
				selectAll: true,
			},
			fields: { secret: { read: false, create: false, update: false } },
		};
		const client = createClient<TestConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (request) => {
				captured = request as Request;
				return Response.json({
					items: [
						{ id: "post_1", access },
						{
							id: "post_2",
							access: { ...access, operations: { ...access.operations, update: false } },
						},
					],
					totalDocs: 2,
				});
			},
		});
		const request = new AbortController();
		const selection = await client.resolveFilteredSelection("posts", {
			where: { title: { equals: "Selected" } },
			trash: true,
			signal: request.signal,
		});
		expect(selection.items.map((item) => item.id)).toEqual(["post_1", "post_2"]);
		expect(selection.items[1]?.access.operations.update).toBe(false);
		expect(new URL(captured?.url ?? "").pathname).toBe("/api/access/collections/posts/selection");
		expect(await captured?.clone().json()).toEqual({
			where: { title: { equals: "Selected" } },
			trash: true,
		});
		request.abort();
		expect(captured?.signal.aborted).toBe(true);
	});

	it("rejects malformed filtered selections", async () => {
		const access = {
			operations: {
				admin: false,
				create: true,
				read: true,
				readVersions: false,
				update: true,
				delete: false,
				duplicate: true,
				publish: true,
				unpublish: true,
				restoreDeleted: false,
				deletePermanent: false,
				selectAll: true,
			},
			fields: {},
		};
		const malformed = [
			[
				{ id: "post_2", access },
				{ id: "post_1", access },
			],
			[{ id: "", access }],
			Array.from({ length: 101 }, (_, index) => ({
				id: `post_${String(index).padStart(3, "0")}`,
				access,
			})),
		];
		for (const items of malformed) {
			const client = createClient<TestConfig>({
				baseURL: "https://cms.example.test",
				fetch: async () => Response.json({ items, totalDocs: items.length }),
			});
			await expect(client.resolveFilteredSelection("posts")).rejects.toBeInstanceOf(RiduError);
		}
	});
});
