import { describe, expect, it } from "bun:test";
import { createClient, type RiduConfigShape } from "@riducms/sdk";
import type { Cookies } from "@sveltejs/kit";

import { BrowserSessionStore } from "../src/browser-session";
import { readCookie, tokenCookie } from "../src/cookie";
import { createDefinition, definitionKey } from "../src/definition";
import { RequestSessionStore } from "../src/request-session";
import { createServerClient } from "../src/server";

interface Account {
	id: string;
	email: string;
}

interface AppConfig extends RiduConfigShape {
	collections: {
		users: {
			auth: true;
			upload: false;
			versions: false;
			drafts: false;
			trash: false;
			output: Account;
			create: { email: string };
			update: Record<string, unknown>;
			where: Record<string, unknown>;
			select: Record<string, boolean>;
			populate: Record<never, never>;
		};
	};
}

type Session = { id: string; collection: "users"; user: Account; expiresAt: string };

const session = (id: string, email = `${id}@example.test`): Session => ({
	id,
	collection: "users",
	user: { id: `user_${id}`, email },
	expiresAt: "2030-01-01T00:00:00.000Z",
});

/** A cookie jar with document.cookie assignment semantics for one cookie name. */
class Jar {
	values = new Map<string, string>();
	get cookie() {
		return [...this.values].map(([name, value]) => `${name}=${value}`).join("; ");
	}
	set cookie(serialized: string) {
		const [pair = "", ...attributes] = serialized.split("; ");
		const separator = pair.indexOf("=");
		const name = pair.slice(0, separator);
		if (attributes.includes("Max-Age=0") || pair.slice(separator + 1) === "") {
			this.values.delete(name);
		} else {
			this.values.set(name, pair.slice(separator + 1));
		}
	}
}

class Channel {
	peers: Channel[] = [];
	posted: unknown[] = [];
	#listeners = new Set<(event: MessageEvent) => void>();
	postMessage(message: unknown) {
		this.posted.push(message);
		for (const peer of this.peers) peer.deliver(message);
	}
	deliver(message: unknown) {
		for (const listener of this.#listeners) listener({ data: message } as MessageEvent);
	}
	addEventListener(_type: "message", listener: (event: MessageEvent) => void) {
		this.#listeners.add(listener);
	}
	removeEventListener(_type: "message", listener: (event: MessageEvent) => void) {
		this.#listeners.delete(listener);
	}
	close() {
		this.#listeners.clear();
	}
}

function browserStore(jar: Jar, snapshot?: () => Session | null | undefined, channel?: Channel) {
	const events = { authChanges: 0, externalTokens: 0, notified: 0 };
	const store = new BrowserSessionStore<Session>({
		cookieName: "ridu_token",
		document: jar,
		secure: false,
		snapshot,
		channel,
		onAuthChange: () => (events.authChanges += 1),
		onExternalToken: () => (events.externalTokens += 1),
	});
	store.subscribe(() => (events.notified += 1));
	return { store, events };
}

describe("token cookie", () => {
	it("reads, writes, and expires only the named cookie", () => {
		expect(readCookie("theme=dark; ridu_token=abc%3D; other=1", "ridu_token")).toBe("abc=");
		expect(readCookie("ridu_token_other=x", "ridu_token")).toBeNull();
		const cookie = tokenCookie("ridu_token", "token", {
			expires: new Date("2030-01-01T00:00:00Z"),
			secure: true,
		});
		expect(cookie).toBe(
			"ridu_token=token; Path=/; Expires=Tue, 01 Jan 2030 00:00:00 GMT; SameSite=Lax; Secure"
		);
		expect(cookie).not.toContain("HttpOnly");
	});
});

describe("browser session store", () => {
	it("hydrates from the rendered session only when the tab holds a token", () => {
		const jar = new Jar();
		let rendered: Session | null = session("a");
		const { store } = browserStore(jar, () => rendered);
		expect(store.session).toBeNull();
		jar.values.set("ridu_token", "token_a");
		rendered = { ...session("a") };
		expect(store.session?.id).toBe("a");
	});

	it("persists a login before resolving and notifies the tab and its peers", () => {
		const jar = new Jar();
		const [first, second] = [new Channel(), new Channel()];
		first.peers.push(second);
		second.peers.push(first);
		const signingIn = browserStore(jar, undefined, first);
		const watching = browserStore(jar, undefined, second);
		signingIn.store.issued("token_a", session("a"));
		expect(jar.values.get("ridu_token")).toBe("token_a");
		expect(signingIn.store.session?.id).toBe("a");
		expect(watching.store.session?.id).toBe("a");
		expect(watching.events.authChanges).toBe(1);
		// Only the safe snapshot crosses tabs.
		expect(JSON.stringify(first.posted)).not.toContain("token_a");

		signingIn.store.cleared("token_a");
		expect(jar.values.has("ridu_token")).toBe(false);
		expect(watching.store.session).toBeNull();
	});

	it("never lets a stale snapshot or late event replace a newer login", () => {
		const jar = new Jar();
		let rendered: Session | null = null;
		const { store } = browserStore(jar, () => rendered);
		store.issued("token_b", session("b"));
		rendered = session("a");
		expect(store.session?.id).toBe("b");
		store.verified("token_a", session("a"));
		store.cleared("token_a");
		expect(store.session?.id).toBe("b");
		expect(jar.values.get("ridu_token")).toBe("token_b");
		// A refreshed user for the same session is accepted.
		rendered = session("b", "renamed@example.test");
		expect(store.session?.user.email).toBe("renamed@example.test");
	});

	it("adopts a token changed out of band, such as a server action login", () => {
		const jar = new Jar();
		let rendered: Session | null = null;
		const { store, events } = browserStore(jar, () => rendered);
		store.issued("token_a", session("a"));
		jar.values.set("ridu_token", "token_c");
		store.checkExternalChange();
		expect(store.session).toBeNull();
		expect(events.externalTokens).toBe(1);
		rendered = session("c");
		expect(store.session?.id).toBe("c");

		jar.values.delete("ridu_token");
		const before = events.authChanges;
		store.checkExternalChange();
		expect(store.session).toBeNull();
		expect(events.authChanges).toBe(before + 1);
	});
});

describe("browser session hydration", () => {
	it("reruns auth loads when the session changed while the page loaded", () => {
		const jar = new Jar();
		const ended = browserStore(jar, () => session("a"));
		ended.store.hydrated();
		expect(ended.events.authChanges).toBe(1);

		jar.values.set("ridu_token", "token_b");
		const started = browserStore(jar, () => null);
		started.store.hydrated();
		expect(started.events.externalTokens).toBe(1);

		const unchanged = browserStore(jar, () => session("b"));
		unchanged.store.hydrated();
		expect(unchanged.events.authChanges + unchanged.events.externalTokens).toBe(0);
	});
});

describe("request session store", () => {
	it("keeps server cookie changes on the response and ignores stale tokens", () => {
		const cookies = fakeCookies({ ridu_token: "token_a" });
		const store = new RequestSessionStore<Session>(cookies, "ridu_token", true);
		store.cleared("token_old");
		expect(cookies.get("ridu_token")).toBe("token_a");
		store.issued("token_b", session("b"));
		expect(cookies.writes.at(-1)).toMatchObject({
			name: "ridu_token",
			value: "token_b",
			options: { path: "/", httpOnly: false, sameSite: "lax", secure: true },
		});
		store.cleared("token_b");
		expect(cookies.get("ridu_token")).toBeUndefined();
	});
});

describe("server client", () => {
	it("verifies a request's session once and clears a rejected token cookie", async () => {
		let reads = 0;
		const requests: Request[] = [];
		const definition = createDefinition({
			createClient: (options) => createClient<AppConfig>(options as never),
			baseURL: "https://cms.example.test/",
			authCollection: "users",
			fetch: async (input) => {
				const request = input as Request;
				requests.push(request);
				if (request.headers.get("authorization") === "Session revoked") {
					return Response.json(
						{ error: { code: "invalid_credential", status: 401, message: "invalid", issues: [] } },
						{ status: 401 }
					);
				}
				reads += 1;
				return Response.json({ session: session("a") });
			},
		});
		const ridu = { [definitionKey]: definition };
		const cookies = fakeCookies({ ridu_token: "token_a" });
		const client = createServerClient(ridu, { cookies, url: new URL("https://app.example.test/") });
		const [first, second] = await Promise.all([client.auth.getSession(), client.auth.getSession()]);
		expect(first?.user.email).toBe("a@example.test");
		expect(second).toEqual(first);
		expect(reads).toBe(1);
		expect(requests[0]?.url).toBe("https://cms.example.test/api/auth/me");
		expect(requests[0]?.headers.get("authorization")).toBe("Session token_a");

		const revoked = fakeCookies({ ridu_token: "revoked" });
		const rejected = createServerClient(ridu, {
			cookies: revoked,
			url: new URL("http://127.0.0.1/"),
		});
		expect(await rejected.auth.getSession()).toBeNull();
		expect(revoked.get("ridu_token")).toBeUndefined();

		const anonymous = createServerClient(ridu, {
			cookies: fakeCookies({}),
			url: new URL("https://app.example.test/"),
		});
		expect(await anonymous.auth.getSession()).toBeNull();
		expect(requests).toHaveLength(2);
	});
});

function fakeCookies(initial: Record<string, string>) {
	const values = new Map(Object.entries(initial));
	const writes: Array<{ name: string; value: string; options: object }> = [];
	const cookies = {
		writes,
		get: (name: string) => values.get(name),
		getAll: () => [...values].map(([name, value]) => ({ name, value })),
		set: (name: string, value: string, options: object) => {
			values.set(name, value);
			writes.push({ name, value, options });
		},
		delete: (name: string, options: object) => {
			values.delete(name);
			writes.push({ name, value: "", options });
		},
		serialize: () => "",
	};
	return cookies as unknown as Cookies & { writes: typeof writes };
}
