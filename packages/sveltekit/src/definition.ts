import type {
	AuthCollectionSlug,
	ClientOptions,
	RiduClient,
	RiduConfigShape,
	SessionFor,
	SessionTokenStore,
} from "@riducms/sdk";

import { DEFAULT_COOKIE_NAME } from "./cookie.js";

/**
 * The generated `createClient` from `ridu.generated.ts`. Its auth parameter is generic, so `any`
 * is the only parameter type every generated signature accepts.
 */
export type ClientFactory = (options: ClientOptions<any>) => RiduClient<any, any>;

/** The application contract bound by a generated factory. */
export type ConfigOf<Factory extends ClientFactory> =
	ReturnType<Factory> extends RiduClient<infer Config, infer _Default> ? Config : never;

export interface RiduDefinitionOptions<Factory extends ClientFactory, Slug extends string> {
	/** The generated factory, which binds the application's types. */
	createClient: Factory;
	/** Public Ridu origin, such as `PUBLIC_RIDU_URL`. Browsers call it directly. */
	baseURL: string;
	/** The auth collection whose users sign in to this application. */
	authCollection: Slug;
	/** Frontend-domain cookie that stores the session token. Defaults to `ridu_token`. */
	cookieName?: string;
	/** Fetch used by server-side clients. Defaults to the platform fetch. */
	fetch?: ClientOptions["fetch"];
}

export const definitionKey: unique symbol = Symbol("ridu.sveltekit.definition");

/** Internal binding shared by the browser provider and the server hook. */
export interface RiduDefinition<
	Config extends RiduConfigShape,
	Slug extends AuthCollectionSlug<Config>,
> {
	readonly baseURL: string;
	readonly authCollection: Slug;
	readonly cookieName: string;
	client(
		token: SessionTokenStore<SessionFor<Config, Slug>>,
		options?: { fetch?: ClientOptions["fetch"]; memoizeSession?: boolean }
	): RiduClient<Config, Slug>;
}

export function createDefinition<
	Factory extends ClientFactory,
	Slug extends AuthCollectionSlug<ConfigOf<Factory>>,
>(options: RiduDefinitionOptions<Factory, Slug>): RiduDefinition<ConfigOf<Factory>, Slug> {
	const baseURL = new URL(options.baseURL).href.replace(/\/$/, "");
	const cookieName = options.cookieName ?? DEFAULT_COOKIE_NAME;
	if (!/^[A-Za-z0-9_-]+$/.test(cookieName)) {
		throw new TypeError("cookieName may contain only letters, digits, underscores, and hyphens");
	}
	return {
		baseURL,
		authCollection: options.authCollection,
		cookieName,
		client(token, clientOptions = {}) {
			const fetch = clientOptions.fetch ?? options.fetch;
			return options.createClient({
				baseURL,
				...(fetch === undefined ? {} : { fetch }),
				auth: {
					collection: options.authCollection,
					token: token as SessionTokenStore,
					memoizeSession: clientOptions.memoizeSession === true,
				},
			}) as RiduClient<ConfigOf<Factory>, Slug>;
		},
	};
}
