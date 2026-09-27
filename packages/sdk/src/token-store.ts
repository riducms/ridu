import type { SessionTokenStore } from "./types.js";

/**
 * Hold a token-transport session in memory, for scripts, tests, and server processes that act as
 * one user. It follows the store contract: a late event for a replaced token is ignored.
 *
 * @example
 * ```ts
 * const ridu = createClient({
 *   baseURL: "https://cms.example.com",
 *   auth: { collection: "users", token: memoryTokenStore() },
 * });
 * await ridu.auth.login({ email, password });
 * ```
 */
export function memoryTokenStore<Session = unknown>(
	initial: { token: string; session?: Session | null } | null = null
): SessionTokenStore<Session> {
	let token = initial?.token ?? null;
	let session: Session | null = initial?.session ?? null;
	return {
		get token() {
			return token;
		},
		get session() {
			return session;
		},
		issued(next, nextSession) {
			token = next;
			session = nextSession;
		},
		verified(current, nextSession) {
			if (current === token) session = nextSession;
		},
		cleared(current) {
			if (current !== token) return;
			token = null;
			session = null;
		},
	};
}
