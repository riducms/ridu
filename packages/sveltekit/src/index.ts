import type { AuthCollectionSlug, RiduClient, RiduConfigShape, SessionFor } from "@riducms/sdk";
import { getContext, onDestroy, onMount, setContext } from "svelte";
import { createSubscriber } from "svelte/reactivity";
import { browser } from "$app/environment";
import { invalidate } from "$app/navigation";

import { BrowserSessionStore } from "./browser-session.js";
import {
	createDefinition,
	definitionKey,
	type ClientFactory,
	type ConfigOf,
	type RiduDefinition,
	type RiduDefinitionOptions,
} from "./definition.js";

export { DEFAULT_COOKIE_NAME } from "./cookie.js";
export type { ClientFactory, ConfigOf, RiduDefinitionOptions } from "./definition.js";

/** The dependency every auth-sensitive load should declare with `depends(AUTH_DEPENDENCY)`. */
export const AUTH_DEPENDENCY = "ridu:auth";

export interface ProvideOptions<Session> {
	/**
	 * The session rendered by the root layout, usually `() => data.session`. It seeds hydration and
	 * later refreshes the user; it never overrides a newer sign-in or sign-out in this tab.
	 */
	session?: Session | null | (() => Session | null);
}

/** One application's Ridu binding. It holds configuration, never an authenticated client. */
export interface Ridu<Config extends RiduConfigShape, Slug extends AuthCollectionSlug<Config>> {
	readonly baseURL: string;
	readonly authCollection: Slug;
	readonly cookieName: string;
	/**
	 * Create the client for this mounted application and share it with descendants. Call it once,
	 * during initialization of the root layout. Server renders get an isolated, credential-free
	 * client whose `auth.session` is the rendered snapshot.
	 */
	provide(options?: ProvideOptions<SessionFor<Config, Slug>>): RiduClient<Config, Slug>;
	/** Read the client provided by the root layout. Call it during component initialization. */
	use(): RiduClient<Config, Slug>;
	/** @internal */
	readonly [definitionKey]: RiduDefinition<Config, Slug>;
}

/** The client type of a `defineRidu` result, for `App.Locals`. */
export type InferClient<Binding> =
	Binding extends Ridu<infer Config, infer Slug> ? RiduClient<Config, Slug> : never;

/** The session type of a `defineRidu` result. */
export type InferSession<Binding> =
	Binding extends Ridu<infer Config, infer Slug> ? SessionFor<Config, Slug> : never;

const clientKey = Symbol("ridu.sveltekit.client");

/**
 * Bind an application's generated client to SvelteKit.
 *
 * ```ts
 * export const ridu = defineRidu({ createClient, baseURL: PUBLIC_RIDU_URL, authCollection: "users" });
 * ```
 */
export function defineRidu<
	Factory extends ClientFactory,
	const Slug extends AuthCollectionSlug<ConfigOf<Factory>>,
>(options: RiduDefinitionOptions<Factory, Slug>): Ridu<ConfigOf<Factory>, Slug> {
	type Config = ConfigOf<Factory>;
	type Session = SessionFor<Config, Slug>;
	const definition = createDefinition<Factory, Slug>(options);
	return {
		baseURL: definition.baseURL,
		authCollection: definition.authCollection,
		cookieName: definition.cookieName,
		[definitionKey]: definition,
		provide(provideOptions = {}) {
			const provided = provideOptions.session;
			const snapshot =
				typeof provided === "function" ? provided : () => provided as Session | null | undefined;
			const client = browser
				? browserClient(definition, snapshot)
				: renderClient(definition, snapshot);
			setContext(clientKey, client);
			return client;
		},
		use() {
			const client = getContext<RiduClient<Config, Slug> | undefined>(clientKey);
			if (client === undefined) {
				throw new Error("ridu.use() needs ridu.provide() in the root layout");
			}
			return client;
		},
	};
}

function browserClient<Config extends RiduConfigShape, Slug extends AuthCollectionSlug<Config>>(
	definition: RiduDefinition<Config, Slug>,
	snapshot: () => SessionFor<Config, Slug> | null | undefined
) {
	type Session = SessionFor<Config, Slug>;
	let client: RiduClient<Config, Slug> | undefined;
	const channel =
		typeof BroadcastChannel === "undefined"
			? undefined
			: new BroadcastChannel(`ridu-auth:${definition.cookieName}`);
	const store: BrowserSessionStore<Session> = new BrowserSessionStore<Session>({
		cookieName: definition.cookieName,
		document,
		secure: location.protocol === "https:",
		snapshot,
		channel,
		track: createSubscriber((update) => store.subscribe(update)),
		onAuthChange: refreshAuthLoads,
		onExternalToken: () => {
			// getSession reports the verified snapshot back to the store.
			client?.auth.getSession().then(refreshAuthLoads, refreshAuthLoads);
		},
	});
	client = definition.client(store);
	const visible = () => {
		if (document.visibilityState === "visible") store.checkExternalChange();
	};
	document.addEventListener("visibilitychange", visible);
	onMount(() => store.hydrated());
	onDestroy(() => {
		document.removeEventListener("visibilitychange", visible);
		store.dispose();
	});
	return client;
}

/** Rerun loads that declared `depends(AUTH_DEPENDENCY)`. Ordinary mutations never do this. */
function refreshAuthLoads() {
	invalidate(AUTH_DEPENDENCY).catch((error: unknown) => {
		// A failed load renders SvelteKit's error page; keep the failure visible to developers.
		console.error("Ridu could not refresh auth-dependent loads", error);
	});
}

/** A server render never holds a credential; components see the rendered snapshot only. */
function renderClient<Config extends RiduConfigShape, Slug extends AuthCollectionSlug<Config>>(
	definition: RiduDefinition<Config, Slug>,
	snapshot: () => SessionFor<Config, Slug> | null | undefined
) {
	return definition.client({
		token: null,
		get session() {
			return snapshot() ?? null;
		},
		issued() {
			throw new Error("Sign in from a browser event, a form action, or a server load");
		},
		cleared() {},
	});
}
