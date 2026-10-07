import type {
	AuthCollectionSlug,
	ClientOptions,
	RiduClient,
	RiduConfigShape,
	SessionFor,
} from "@riducms/sdk";
import type { RequestEvent } from "@sveltejs/kit";

import { definitionKey, type RiduDefinition } from "./definition.js";
import { RequestSessionStore } from "./request-session.js";

interface Binding<Config extends RiduConfigShape, Slug extends AuthCollectionSlug<Config>> {
	readonly [definitionKey]: RiduDefinition<Config, Slug>;
}

export interface ServerClientOptions {
	/**
	 * Fetch for server requests to Ridu. Defaults to the definition's fetch, then to SvelteKit's
	 * `event.fetch`. The client sends its token in a header with `credentials: "omit"`, so
	 * `event.fetch` does not forward the web app's cookies to Ridu.
	 */
	fetch?: ClientOptions["fetch"];
}

/**
 * Create the Ridu client for one SvelteKit request. It authenticates with the request's frontend
 * cookie, verifies `auth.getSession()` at most once, and writes the cookie on the response after a
 * server-side login, logout, or rejected token.
 */
export function createServerClient<
	Config extends RiduConfigShape,
	Slug extends AuthCollectionSlug<Config>,
>(
	ridu: Binding<Config, Slug>,
	event: Pick<RequestEvent, "cookies" | "url"> & Partial<Pick<RequestEvent, "fetch">>,
	options: ServerClientOptions = {}
): RiduClient<Config, Slug> {
	const definition = ridu[definitionKey];
	const store = new RequestSessionStore<SessionFor<Config, Slug>>(
		event.cookies,
		definition.cookieName,
		event.url.protocol === "https:"
	);
	return definition.client(store, {
		...(options.fetch === undefined ? {} : { fetch: options.fetch }),
		...(event.fetch === undefined ? {} : { fallbackFetch: event.fetch }),
		memoizeSession: true,
	});
}

/**
 * A server `handle` hook. SvelteKit 2 exports `Handle` from `@sveltejs/kit` and SvelteKit 3 from
 * `@sveltejs/kit/hooks`; this shape is assignable to both.
 */
export type RiduHandle = (input: {
	event: RequestEvent;
	resolve: (event: RequestEvent) => Response | Promise<Response>;
}) => Response | Promise<Response>;

/**
 * A SvelteKit `handle` that sets `event.locals.ridu` for each request. Compose it with other hooks
 * through `sequence`.
 */
export function createRiduHandle<
	Config extends RiduConfigShape,
	Slug extends AuthCollectionSlug<Config>,
>(ridu: Binding<Config, Slug>, options: ServerClientOptions = {}): RiduHandle {
	return ({ event, resolve }) => {
		// Applications type `App.Locals.ridu` with their own client; assign without widening it.
		Object.assign(event.locals, { ridu: createServerClient(ridu, event, options) });
		return resolve(event);
	};
}
