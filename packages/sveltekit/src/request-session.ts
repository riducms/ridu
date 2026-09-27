import type { SessionTokenStore } from "@riducms/sdk";
import type { Cookies } from "@sveltejs/kit";

import type { SessionSnapshot } from "./browser-session.js";

/**
 * Owns the token for one SvelteKit request. It reads the same frontend cookie the browser client
 * writes, and a server-side login, logout, or rejected token updates that cookie on the response.
 */
export class RequestSessionStore<
	Session extends SessionSnapshot,
> implements SessionTokenStore<Session> {
	#session: Session | null = null;

	constructor(
		private readonly cookies: Cookies,
		private readonly cookieName: string,
		private readonly secure: boolean
	) {}

	get token() {
		return this.cookies.get(this.cookieName) ?? null;
	}

	get session() {
		return this.#session;
	}

	issued(token: string, session: Session) {
		this.cookies.set(this.cookieName, token, {
			path: "/",
			expires: new Date(session.expiresAt),
			httpOnly: false,
			sameSite: "lax",
			secure: this.secure,
		});
		this.#session = session;
	}

	verified(token: string, session: Session) {
		if (this.token === token) this.#session = session;
	}

	cleared(token: string) {
		if (this.token !== token) return;
		this.cookies.delete(this.cookieName, {
			path: "/",
			httpOnly: false,
			sameSite: "lax",
			secure: this.secure,
		});
		this.#session = null;
	}
}
