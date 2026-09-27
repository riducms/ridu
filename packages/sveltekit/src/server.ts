import type {
	AuthCollectionSlug,
	ClientOptions,
	RiduClient,
	RiduConfigShape,
	SessionFor,
} from "@riducms/sdk";
import type { Handle, RequestEvent } from "@sveltejs/kit";

import { definitionKey, type RiduDefinition } from "./definition.js";
import { RequestSessionStore } from "./request-session.js";

interface Binding<Config extends RiduConfigShape, Slug extends AuthCollectionSlug<Config>> {
	readonly [definitionKey]: RiduDefinition<Config, Slug>;
}

export interface ServerClientOptions {
	/** Fetch for server requests to Ridu. Defaults to the definition's fetch or the platform fetch. */
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
	event: Pick<RequestEvent, "cookies" | "url">,
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
		memoizeSession: true,
	});
}

/**
 * A SvelteKit `handle` that sets `event.locals.ridu` for each request. Compose it with other hooks
 * through `sequence`.
 */
export function createRiduHandle<
	Config extends RiduConfigShape,
	Slug extends AuthCollectionSlug<Config>,
>(ridu: Binding<Config, Slug>, options: ServerClientOptions = {}): Handle {
	return ({ event, resolve }) => {
		// Applications type `App.Locals.ridu` with their own client; assign without widening it.
		Object.assign(event.locals, { ridu: createServerClient(ridu, event, options) });
		return resolve(event);
	};
}
