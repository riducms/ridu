import type { SessionTokenStore } from "@riducms/sdk";

import { expiredTokenCookie, readCookie, tokenCookie } from "#lib/cookie.js";

/** The safe session snapshot shared by tabs and rendered pages. It never contains the token. */
export interface SessionSnapshot {
	id: string;
	expiresAt: string;
}

/** Tabs share one cookie jar; a message only tells other tabs to adopt the change. */
interface SessionMessage<Session> {
	session: Session | null;
}

interface MessageChannel<Session> {
	postMessage(message: SessionMessage<Session>): void;
	addEventListener(type: "message", listener: (event: MessageEvent) => void): void;
	removeEventListener(type: "message", listener: (event: MessageEvent) => void): void;
	close(): void;
}

export interface BrowserSessionOptions<Session> {
	cookieName: string;
	/** The document whose cookie jar holds the token. */
	document: { cookie: string };
	secure: boolean;
	/**
	 * Server-rendered session snapshots, usually `() => data.session` from the root layout. They seed
	 * hydration and refresh the user; a stale snapshot never replaces newer browser state.
	 */
	snapshot?: (() => Session | null | undefined) | undefined;
	/** Cross-tab notifications. Tokens are never posted, only safe snapshots. */
	channel?: MessageChannel<Session> | undefined;
	/** Record a reactive read, such as a Svelte subscriber. */
	track?: (() => void) | undefined;
	/** The signed-in identity changed: SvelteKit loads that depend on auth must rerun. */
	onAuthChange(): void;
	/** Another tab or a server response replaced the token; verify the new one with Ridu. */
	onExternalToken(): void;
}

/**
 * Owns a token-transport session in the browser. The token lives in a frontend-domain cookie that
 * server rendering also receives; this store reads it on every request and never caches it.
 */
export class BrowserSessionStore<
	Session extends SessionSnapshot,
> implements SessionTokenStore<Session> {
	readonly #options: BrowserSessionOptions<Session>;
	readonly #listeners = new Set<() => void>();
	#session: Session | null = null;
	/** The token the current snapshot belongs to. Any other cookie value arrived out of band. */
	#knownToken: string | null = null;
	#lastSnapshot: Session | null | undefined;
	readonly #receive = (event: MessageEvent) => this.#adopt(event.data as SessionMessage<Session>);

	constructor(options: BrowserSessionOptions<Session>) {
		this.#options = options;
		options.channel?.addEventListener("message", this.#receive);
	}

	get token() {
		return readCookie(this.#options.document.cookie, this.#options.cookieName);
	}

	get session() {
		this.#options.track?.();
		this.#reconcile();
		return this.#session;
	}

	subscribe(listener: () => void) {
		this.#listeners.add(listener);
		return () => this.#listeners.delete(listener);
	}

	issued(token: string, session: Session) {
		this.#options.document.cookie = tokenCookie(this.#options.cookieName, token, {
			expires: new Date(session.expiresAt),
			secure: this.#options.secure,
		});
		this.#session = session;
		this.#knownToken = token;
		this.#changed(true);
	}

	verified(token: string, session: Session) {
		// A late verification for a replaced token must not restore the previous account.
		if (this.token !== token) return;
		this.#session = session;
		this.#knownToken = token;
		this.#emit();
	}

	cleared(token: string) {
		// A late rejection or logout must not clear a newer login.
		if (this.token !== token) return;
		this.#options.document.cookie = expiredTokenCookie(
			this.#options.cookieName,
			this.#options.secure
		);
		this.#session = null;
		this.#knownToken = null;
		this.#changed(true);
	}

	/**
	 * Detect a token that changed without this tab's involvement, such as a server action login, an
	 * expired cookie, or a tab without BroadcastChannel. Call it when the page becomes visible.
	 */
	checkExternalChange() {
		const token = this.token;
		if (token === this.#knownToken) return;
		this.#knownToken = token;
		this.#session = null;
		this.#emit();
		if (token === null) this.#options.onAuthChange();
		else this.#options.onExternalToken();
	}

	/**
	 * Call once after hydration. A broadcast sent while this page was loading was missed, so compare
	 * the rendered session with the cookie: a session that ended or a token that appeared meanwhile
	 * reruns auth-dependent loads.
	 */
	hydrated() {
		const rendered = this.#options.snapshot?.() ?? null;
		const token = this.token;
		if (rendered !== null && token === null) this.#options.onAuthChange();
		else if (rendered === null && token !== null) this.checkExternalChange();
	}

	dispose() {
		this.#options.channel?.removeEventListener("message", this.#receive);
		this.#options.channel?.close();
		this.#listeners.clear();
	}

	#reconcile() {
		const incoming = this.#options.snapshot?.();
		if (incoming === undefined || incoming === this.#lastSnapshot) return;
		this.#lastSnapshot = incoming;
		const token = this.token;
		if (incoming === null) {
			// The server saw no session. Accept that only if this tab agrees there is no token.
			if (token === null) {
				this.#session = null;
				this.#knownToken = null;
			}
			return;
		}
		if (token === null) return;
		// Accept the first snapshot, a refreshed user for the same session, or a token that changed
		// out of band. A snapshot for another session than this tab's newer login is stale.
		const outOfBand = token !== this.#knownToken;
		if (outOfBand || this.#session === null || this.#session.id === incoming.id) {
			this.#session = incoming;
			this.#knownToken = token;
		}
	}

	#adopt(message: SessionMessage<Session>) {
		const token = this.token;
		if (message.session === null ? token !== null : token === null) {
			// The cookie already moved on; let the visible state follow the cookie instead.
			this.checkExternalChange();
			return;
		}
		this.#session = message.session;
		this.#knownToken = token;
		this.#changed(false);
	}

	#changed(broadcast: boolean) {
		this.#emit();
		if (broadcast) this.#options.channel?.postMessage({ session: this.#session });
		this.#options.onAuthChange();
	}

	#emit() {
		for (const listener of this.#listeners) listener();
	}
}
