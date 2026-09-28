import { describe, expect, it } from "bun:test";

import {
	createClient,
	memoryTokenStore,
	RiduError,
	type RiduConfigShape,
	type SessionTokenStore,
} from "../src";

interface Account {
	id: string;
	email: string;
	displayName: string;
}

interface TransportConfig extends RiduConfigShape {
	collections: {
		users: {
			auth: true;
			upload: false;
			versions: false;
			drafts: false;
			trash: false;
			output: Account;
			create: { email: string; displayName: string };
			update: { displayName?: string };
			where: Record<string, unknown>;
			select: Record<string, boolean>;
			populate: Record<never, never>;
		};
		assets: {
			auth: false;
			upload: true;
			versions: false;
			drafts: false;
			trash: false;
			output: { id: string; url?: string };
			create: Record<string, unknown>;
			update: Record<string, unknown>;
			where: Record<string, unknown>;
			select: Record<string, boolean>;
			populate: Record<never, never>;
		};
	};
}

type Session = { id: string; collection: string; user: unknown; expiresAt: string };

/** A store with the documented contract: every event names its token; stale events are ignored. */
class MemoryStore implements SessionTokenStore<Session> {
	token: string | null = null;
	session: Session | null = null;
	readonly events: string[] = [];

	issued(token: string, session: Session) {
		this.token = token;
		this.session = session;
		this.events.push(`issued:${token}`);
	}

	verified(token: string, session: Session) {
		this.events.push(`verified:${token}`);
		if (this.token === token) this.session = session;
	}

	cleared(token: string) {
		this.events.push(`cleared:${token}`);
		if (this.token !== token) return;
		this.token = null;
		this.session = null;
	}
}

const session = (id = "session_1") => ({
	id,
	collection: "users",
	user: { id: "user_1", email: "ada@example.test", displayName: "Ada" },
	expiresAt: "2030-01-01T00:00:00Z",
});

const invalidCredential = () =>
	Response.json(
		{
			error: {
				code: "invalid_credential",
				status: 401,
				message: "the Authorization credential is invalid or expired",
				issues: [],
			},
		},
		{ status: 401 }
	);

function deferred<Value>() {
	let resolve!: (value: Value) => void;
	const promise = new Promise<Value>((settle) => (resolve = settle));
	return { promise, resolve };
}

describe("token session transport", () => {
	it("persists a login token before resolving and sends it on every request", async () => {
		const store = new MemoryStore();
		const persistenceStarted = deferred<void>();
		const releasePersistence = deferred<void>();
		const completionOrder: string[] = [];
		const persist = store.issued.bind(store);
		let persistence: Promise<void> | undefined;
		store.issued = (token, snapshot) => {
			persistence = (async () => {
				persistenceStarted.resolve();
				await releasePersistence.promise;
				persist(token, snapshot);
				completionOrder.push("persisted");
			})();
			return persistence;
		};
		const requests: Request[] = [];
		const client = createClient<TransportConfig, "users">({
			baseURL: "https://cms.example.test",
			auth: { collection: "users", token: store },
			fetch: async (input) => {
				const request = input as Request;
				requests.push(request);
				if (request.url.endsWith("/login")) {
					// A previous token must not accompany a new login.
					expect(request.headers.get("authorization")).toBeNull();
					expect(await request.json()).toEqual({
						email: "ada@example.test",
						password: "correct-horse",
						transport: "token",
					});
					return Response.json({ session: session(), token: "token_a" });
				}
				return Response.json({ docs: [], pagination: {} });
			},
		});
		store.token = "stale";
		const login = client.auth
			.login({ email: "ada@example.test", password: "correct-horse" })
			.then((signedIn) => {
				completionOrder.push("login");
				return signedIn;
			});
		try {
			await Promise.race([persistenceStarted.promise, login]);
			expect(persistence).toBeDefined();
			expect(store.token).toBe("stale");
			expect(completionOrder).toEqual([]);
			releasePersistence.resolve();
			const signedIn = await login;
			await persistence;
			expect(completionOrder).toEqual(["persisted", "login"]);
			expect(signedIn.user.displayName).toBe("Ada");
			expect(store.token).toBe("token_a");
			expect(client.auth.session?.user.email).toBe("ada@example.test");

			await client.request("/api/custom");
			store.token = "token_b";
			await client.request("/api/custom");
			expect(requests[1]?.headers.get("authorization")).toBe("Session token_a");
			expect(requests[2]?.headers.get("authorization")).toBe("Session token_b");
		} finally {
			releasePersistence.resolve();
			await Promise.allSettled([login, persistence]);
		}
	});

	it("omits ambient cookies from token requests", async () => {
		// Bun's Request does not report its credentials mode, so observe the constructed init.
		const modes: Array<RequestCredentials | undefined> = [];
		const NativeRequest = globalThis.Request;
		globalThis.Request = class extends NativeRequest {
			constructor(input: RequestInfo | URL, init?: RequestInit) {
				super(input, init);
				modes.push(init?.credentials);
			}
		} as typeof Request;
		try {
			const tokenClient = createClient<TransportConfig>({
				baseURL: "https://cms.example.test",
				auth: { token: { token: "token_a", issued() {}, cleared() {} } },
				fetch: async () => Response.json({}),
			});
			const cookieClient = createClient<TransportConfig>({
				baseURL: "https://cms.example.test",
				fetch: async () => Response.json({}),
			});
			await tokenClient.request("/api/custom");
			await cookieClient.request("/api/custom");
		} finally {
			globalThis.Request = NativeRequest;
		}
		expect(modes).toEqual(["omit", "include"]);
	});

	it("keeps cookie mode unchanged when no token store is configured", async () => {
		let captured: Request | undefined;
		const client = createClient<TransportConfig>({
			baseURL: "https://cms.example.test",
			fetch: async (input) => {
				captured = input as Request;
				return Response.json({ session: session() });
			},
		});
		await client.auth.login({
			collection: "users",
			email: "ada@example.test",
			password: "correct-horse",
		});
		expect(await captured?.json()).toEqual({
			email: "ada@example.test",
			password: "correct-horse",
		});
		expect(captured?.credentials).toBe("include");
		expect(client.auth.session).toBeNull();
	});

	it("forgets only a token Ridu rejected as an invalid credential", async () => {
		const store = new MemoryStore();
		store.token = "token_a";
		let reply: () => Response = invalidCredential;
		const client = createClient<TransportConfig>({
			baseURL: "https://cms.example.test",
			auth: { token: store },
			fetch: async () => reply(),
		});
		reply = () =>
			Response.json(
				{
					error: {
						code: "access_denied",
						status: 401,
						message: "invalid email or password",
						issues: [],
					},
				},
				{ status: 401 }
			);
		await expect(
			client.auth.changePassword({ currentPassword: "wrong", password: "next-password" })
		).rejects.toBeInstanceOf(RiduError);
		expect(store.token).toBe("token_a");

		reply = invalidCredential;
		const response = await client.request("/api/collections/bookmarks");
		expect(response.status).toBe(401);
		// The raw escape hatch still returns an unread body.
		expect((await response.json()).error.code).toBe("invalid_credential");
		expect(store.token).toBeNull();
	});

	it("never lets a late rejection clear a newer login", async () => {
		const store = new MemoryStore();
		store.token = "token_a";
		const pending = deferred<Response>();
		const client = createClient<TransportConfig>({
			baseURL: "https://cms.example.test",
			auth: { token: store },
			fetch: () => pending.promise,
		});
		const request = client.request("/api/collections/bookmarks");
		store.issued("token_b", session("session_b"));
		pending.resolve(invalidCredential());
		await request;
		expect(store.events).toContain("cleared:token_a");
		expect(store.token).toBe("token_b");
	});

	it("verifies sessions, distinguishing no session from an outage", async () => {
		const store = new MemoryStore();
		let requests = 0;
		let reply: () => Response = () => Response.json({ session: session() });
		const client = createClient<TransportConfig, "users">({
			baseURL: "https://cms.example.test",
			auth: { collection: "users", token: store },
			fetch: async () => {
				requests += 1;
				return reply();
			},
		});
		expect(await client.auth.getSession()).toBeNull();
		expect(requests).toBe(0);

		store.token = "token_a";
		expect((await client.auth.getSession())?.id).toBe("session_1");
		expect(store.events).toContain("verified:token_a");

		reply = () =>
			Response.json(
				{ error: { code: "internal", status: 503, message: "unavailable", issues: [] } },
				{ status: 503 }
			);
		await expect(client.auth.getSession()).rejects.toMatchObject({ status: 503 });
		expect(store.token).toBe("token_a");

		reply = () => Response.json({ session: { ...session(), collection: "admins" } });
		expect(await client.auth.getSession()).toBeNull();

		reply = invalidCredential;
		expect(await client.auth.getSession()).toBeNull();
		expect(store.token).toBeNull();
	});

	it("memoizes one verified session per token for request-scoped clients", async () => {
		const store = new MemoryStore();
		store.token = "token_a";
		let requests = 0;
		let fail = true;
		const client = createClient<TransportConfig, "users">({
			baseURL: "https://cms.example.test",
			auth: { collection: "users", token: store, memoizeSession: true },
			fetch: async () => {
				requests += 1;
				if (fail) return new Response("unavailable", { status: 502 });
				return Response.json({ session: session() });
			},
		});
		await expect(client.auth.getSession()).rejects.toBeInstanceOf(RiduError);
		fail = false;
		const [first, second] = await Promise.all([client.auth.getSession(), client.auth.getSession()]);
		expect(first).toEqual(second);
		expect(requests).toBe(2);
		store.token = "token_b";
		await client.auth.getSession();
		expect(requests).toBe(3);
	});

	it("forgets the token on logout even when revocation fails", async () => {
		const store = new MemoryStore();
		store.token = "token_a";
		const client = createClient<TransportConfig>({
			baseURL: "https://cms.example.test",
			auth: { token: store },
			fetch: async () => new Response("unavailable", { status: 503 }),
		});
		await expect(client.auth.logout()).rejects.toMatchObject({ status: 503 });
		expect(store.token).toBeNull();
		// Nothing to revoke without a token.
		expect(await client.auth.logout()).toEqual({ loggedOut: true });
	});

	it("stores a rotated token only while its predecessor is current", async () => {
		const store = new MemoryStore();
		store.token = "token_a";
		const pending = deferred<Response>();
		let rotations = 0;
		const client = createClient<TransportConfig>({
			baseURL: "https://cms.example.test",
			auth: { token: store },
			fetch: async (input) => {
				rotations += 1;
				if (rotations === 1) {
					expect((input as Request).headers.get("authorization")).toBe("Session token_a");
					return Response.json({ session: session(), token: "token_a2" });
				}
				expect((input as Request).headers.get("authorization")).toBe("Session token_a2");
				return pending.promise;
			},
		});
		expect((await client.auth.rotate()).id).toBe("session_1");
		expect(store.token).toBe("token_a2");
		expect(store.session).toEqual(session());
		const rotation = client.auth.rotate();
		try {
			store.issued("token_b", session("session_b"));
			pending.resolve(Response.json({ session: session(), token: "token_a3" }));
			await rotation;
			expect(store.token).toBe("token_b");
			expect(store.session).toEqual(session("session_b"));
		} finally {
			pending.resolve(Response.json({ session: session(), token: "token_a3" }));
			await Promise.allSettled([rotation]);
		}
	});

	it("requires an auth collection when the client has none configured", async () => {
		const client = createClient<RiduConfigShape & { collections: Record<string, never> }>({
			baseURL: "https://cms.example.test",
			fetch: async () => Response.json({}),
		});
		const auth = client.auth as unknown as { login(input: object): Promise<unknown> };
		await expect(auth.login({ email: "a@example.test", password: "x" })).rejects.toBeInstanceOf(
			TypeError
		);
	});

	it("returns absolute upload URLs in order across batches", async () => {
		const bodies: Array<{ items: Array<{ id: string; size?: string }>; expiresIn?: number }> = [];
		const client = createClient<TransportConfig>({
			baseURL: "https://cms.example.test",
			auth: { token: { token: "token_a", issued() {}, cleared() {} } },
			fetch: async (input) => {
				const request = input as Request;
				expect(new URL(request.url).pathname).toBe("/api/uploads/assets/grants");
				expect(request.headers.get("authorization")).toBe("Session token_a");
				const body = (await request.json()) as (typeof bodies)[number];
				bodies.push(body);
				return Response.json({
					grants: body.items.map((item) => ({
						...item,
						url: `/api/uploads/assets/${item.id}?grant=g`,
						expiresAt: "2030-01-01T00:10:00Z",
					})),
				});
			},
		});
		const ids = Array.from({ length: 150 }, (_, index) => `asset_${index}`);
		const urls = await client.getUploadURLs("assets", ids, { expiresIn: 300 });
		expect(bodies.map((body) => body.items.length)).toEqual([100, 50]);
		expect(bodies.flatMap((body) => body.items.map((item) => item.id))).toEqual(ids);
		expect(urls).toEqual(
			ids.map((id) => ({
				url: `https://cms.example.test/api/uploads/assets/${id}?grant=g`,
				expiresAt: "2030-01-01T00:10:00Z",
			}))
		);
		expect(bodies[0]?.expiresIn).toBe(300);
		const thumb = await client.getUploadURL("assets", "asset_1", { size: "thumb" });
		expect(bodies.at(-1)?.items).toEqual([{ id: "asset_1", size: "thumb" }]);
		expect(thumb.url.startsWith("https://cms.example.test/")).toBe(true);
	});

	it("keeps an explicit Authorization header such as a preview token", async () => {
		let authorization = "";
		const client = createClient<TransportConfig>({
			baseURL: "https://cms.example.test",
			auth: { token: { token: "token_a", issued() {}, cleared() {} } },
			fetch: async (input) => {
				authorization = (input as Request).headers.get("authorization") ?? "";
				return Response.json({});
			},
		});
		await client.request("/api/custom", { headers: { authorization: "Bearer preview" } });
		expect(authorization).toBe("Bearer preview");
	});

	it("keeps a script's session in memory without a cookie jar", async () => {
		const store = memoryTokenStore();
		const client = createClient<TransportConfig, "users">({
			baseURL: "https://cms.example.test",
			auth: { collection: "users", token: store },
			fetch: async (input) => {
				const request = input as Request;
				if (request.url.endsWith("/login")) {
					return Response.json({ session: session(), token: "token_a" });
				}
				expect(request.headers.get("authorization")).toBe("Session token_a");
				return Response.json({ session: session() });
			},
		});
		await client.auth.login({ email: "ada@example.test", password: "correct-horse" });
		expect((await client.auth.getSession())?.user.displayName).toBe("Ada");
		store.cleared("token_other");
		expect(store.token).toBe("token_a");
	});
});
