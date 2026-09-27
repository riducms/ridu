import {
	isErrorEnvelope,
	isRecord,
	bindSchemaManifest,
	isPageEnvelope,
	isAdminCollectionListData,
	type AdminCollectionListDataV1,
	isValidationIssue,
	type LiveValidationRequest,
	type LiveValidationEnvelope,
	type AuthSession,
	type AccessCapabilitiesEnvelope,
	type CollectionPageEnvelope,
	type CollectionSelectionEnvelope,
	isAccessCapabilities,
	type AuthSessionInfo,
	type AuthActionEnvelope,
	type AuthBootstrapEnvelope,
	type APIKey,
	type APIKeyInfo,
	type DeleteEnvelope,
	type CountEnvelope,
	type PageEnvelope,
	type LogoutEnvelope,
	type SchemaManifest,
	type ScheduledPublication,
	type DocumentLockEnvelope,
	type PreviewToken,
	type UploadGrant,
} from "@riducms/protocol";

import { RiduError } from "./error.js";
import type {
	ClientOptions,
	LiveValidationOptions,
	CreateAPIKeyInput,
	CollectionAccessOptions,
	CopyLocaleInput,
	CollectionSelectionOptions,
	AuthUser,
	AuthCollectionSlug,
	CollectionSlug,
	CreateFor,
	FindOptions,
	ListOptions,
	LocaleOptions,
	MutationLocaleOptions,
	LocaleFor,
	LoginCredentials,
	MiddlewareNext,
	OutputFor,
	PopulateFor,
	RequestOptions,
	RevisionOptions,
	PublicationScheduleOptions,
	MutationOptions,
	CreateOptions,
	RestoreOptions,
	SelectFor,
	UpdateFor,
	WhereFor,
	RiduAuth,
	RiduClient,
	RiduConfigShape,
	SessionCollection,
	SessionFor,
	SessionTokenStore,
	UploadURL,
	UploadURLOptions,
	UploadCollectionSlug,
	UploadOptions,
	UpdateUploadInput,
	UploadFile,
	Version,
	VersionCollectionSlug,
	DraftCollectionSlug,
	TrashCollectionSlug,
	GlobalSlug,
	GlobalOutputFor,
	GlobalUpdateFor,
	GlobalSelectFor,
	GlobalPopulateFor,
	GlobalAccessOptions,
	VersionGlobalSlug,
	DraftGlobalSlug,
	CollectionQueryResult,
	CollectionListResult,
	GlobalQueryResult,
	AllLocalesOutputFor,
	GlobalAllLocalesOutputFor,
	CollectionVersionsFor,
	CollectionDraftsFor,
	GlobalDraftsFor,
	LocaleResult,
} from "./types.js";

/**
 * Create a Fetch-backed client for a Ridu application.
 *
 * Pass the generated `RiduConfig` type when calling the runtime factory directly so collection,
 * global, input, query, and result types stay tied to that application. Generated projects also
 * export a pre-typed wrapper around this function, which infers `DefaultAuth` from
 * `auth.collection`; a direct call names it explicitly, as in `createClient<RiduConfig, "users">`.
 *
 * @param options The Ridu origin, auth collection and session transport, and optional Fetch,
 *   headers, credentials, and middleware.
 * @returns A client whose methods use the supplied application's generated contracts.
 * @example
 * ```ts
 * import { createClient } from "@riducms/sdk";
 * import type { RiduConfig } from "./generated/ridu.generated";
 *
 * const ridu = createClient<RiduConfig>({ baseURL: "https://cms.example.com" });
 * const { docs } = await ridu.list("posts", {
 *   where: { status: { equals: "published" } },
 *   sort: ["-createdAt"]
 * });
 * ```
 */
export function createClient<
	Config extends RiduConfigShape = RiduConfigShape,
	const DefaultAuth extends AuthCollectionSlug<Config> = never,
>(options: ClientOptions<DefaultAuth>): RiduClient<Config, DefaultAuth> {
	return new FetchClient<Config, DefaultAuth>(options);
}

/** Upload grant requests are chunked to the server's per-request bound. */
const UPLOAD_GRANT_BATCH = 100;

/** The wire session shape before it is narrowed to an application's auth collections. */
type AnySession = { id: string; collection: string; user: unknown; expiresAt: string };

interface RequestBehavior {
	/** Apply a JSON content type to string bodies. */
	json: boolean;
	/** Attach the token-transport credential. Login never sends a previous token. */
	credential: boolean;
	/** The exact token to send, when the caller reasons about which token it presented. */
	token?: string | undefined;
}

class FetchClient<
	Config extends RiduConfigShape,
	DefaultAuth extends AuthCollectionSlug<Config>,
> implements RiduClient<Config, DefaultAuth> {
	readonly baseURL: string;
	readonly auth: RiduAuth<Config, DefaultAuth>;
	readonly #fetch: NonNullable<ClientOptions["fetch"]>;
	readonly #headers: ClientOptions["headers"];
	readonly #credentials: NonNullable<ClientOptions["credentials"]>;
	readonly #dispatch: MiddlewareNext;
	readonly #tokens: SessionTokenStore<AnySession> | undefined;
	readonly #authCollection: string | undefined;
	readonly #memoizeSession: boolean;
	#sessionMemo: { token: string | undefined; session: Promise<AnySession | null> } | undefined;

	constructor(options: ClientOptions<DefaultAuth>) {
		const baseURL = new URL(options.baseURL);
		this.baseURL = baseURL.href.replace(/\/$/, "");
		this.#fetch = options.fetch ?? globalThis.fetch.bind(globalThis);
		this.#headers = options.headers;
		this.#tokens = options.auth?.token as SessionTokenStore<AnySession> | undefined;
		this.#authCollection = options.auth?.collection;
		this.#memoizeSession = options.auth?.memoizeSession === true;
		this.#credentials = options.credentials ?? (this.#tokens === undefined ? "include" : "omit");
		this.#dispatch = composeMiddleware(options.middleware ?? [], (request) => this.#fetch(request));
		this.auth = this.#createAuth();
	}

	#createAuth(): RiduAuth<Config, DefaultAuth> {
		// Auth input types are conditional on the configured collection; the runtime reads the
		// same fields through this structural view.
		type CollectionChoice = { collection?: string };
		const client = this;
		const auth = {
			get session() {
				return (client.#tokens?.session ?? null) as SessionFor<
					Config,
					SessionCollection<Config, DefaultAuth>
				> | null;
			},
			login: async (credentials: LoginCredentials & CollectionChoice, options?: RequestOptions) => {
				const collection = client.#collectionFor(credentials.collection, "login");
				const tokens = client.#tokens;
				const body = await client.#request(
					`/api/auth/${encodeURIComponent(collection)}/login`,
					{
						method: "POST",
						body: JSON.stringify({
							email: credentials.email,
							password: credentials.password,
							...(tokens === undefined ? {} : { transport: "token" }),
						}),
					},
					options,
					{ json: true, credential: false }
				);
				client.#sessionMemo = undefined;
				if (tokens === undefined) return sessionFromEnvelope(body);
				const issued = sessionTokenFromEnvelope(body);
				await tokens.issued(issued.token, issued.session);
				return issued.session;
			},
			getSession: (options?: RequestOptions) => client.#getSession(options),
			rotate: async (options?: RequestOptions) => {
				const tokens = client.#tokens;
				const sent = tokens?.token ?? undefined;
				const body = await client.#request("/api/auth/rotate", { method: "POST" }, options, {
					json: true,
					credential: true,
					token: sent,
				});
				client.#sessionMemo = undefined;
				if (tokens === undefined || sent === undefined) return sessionFromEnvelope(body);
				const issued = sessionTokenFromEnvelope(body);
				// A rotation that finishes after another login must not replace the newer token.
				if (tokens.token === sent) await tokens.issued(issued.token, issued.session);
				return issued.session;
			},
			logout: async (options?: RequestOptions) => {
				const tokens = client.#tokens;
				const sent = tokens?.token ?? undefined;
				if (tokens !== undefined && sent === undefined) return { loggedOut: true as const };
				try {
					const body = await client.#request("/api/auth/logout", { method: "POST" }, options, {
						json: true,
						credential: true,
						token: sent,
					});
					if (!isRecord(body) || body.loggedOut !== true) throw invalidSuccessEnvelope("logout");
					return { loggedOut: true as const };
				} finally {
					client.#sessionMemo = undefined;
					if (tokens !== undefined && sent !== undefined) await tokens.cleared(sent);
				}
			},
			logoutAll: async (options?: RequestOptions) => {
				const sent = client.#tokens?.token ?? undefined;
				const body = await client.#request("/api/auth/logout-all", { method: "POST" }, options, {
					json: true,
					credential: true,
					token: sent,
				});
				if (!isRecord(body) || body.loggedOut !== true) throw invalidSuccessEnvelope("logout-all");
				await client.#forget(sent);
				return { loggedOut: true as const };
			},
			sessions: async (options?: RequestOptions) => {
				const body = await client.#request("/api/auth/sessions", { method: "GET" }, options);
				if (
					!isRecord(body) ||
					!Array.isArray(body.sessions) ||
					!body.sessions.every(isAuthSessionInfo)
				) {
					throw invalidSuccessEnvelope("sessions");
				}
				return body.sessions;
			},
			revokeSession: async (id: string, options?: RequestOptions) => {
				const body = await client.#request(
					`/api/auth/sessions/${encodeURIComponent(id)}`,
					{ method: "DELETE" },
					options
				);
				if (!isRecord(body) || body.id !== id || body.deleted !== true) {
					throw invalidSuccessEnvelope("session revocation");
				}
				return { id, deleted: true as const };
			},
			changePassword: async (
				input: { currentPassword: string; password: string },
				options?: RequestOptions
			) => {
				const sent = client.#tokens?.token ?? undefined;
				const body = await client.#request(
					"/api/auth/change-password",
					{
						method: "POST",
						body: JSON.stringify({
							currentPassword: input.currentPassword,
							password: input.password,
						}),
					},
					options,
					{ json: true, credential: true, token: sent }
				);
				if (!isRecord(body) || body.success !== true) {
					throw invalidSuccessEnvelope("password change");
				}
				// A password change revokes every session, including this one.
				await client.#forget(sent);
				return { success: true as const };
			},
			createUser: async (
				input: { data: unknown; password: string } & CollectionChoice,
				options?: MutationLocaleOptions
			) => {
				const collection = client.#collectionFor(input.collection, "createUser");
				const query = new URLSearchParams();
				appendLocaleQuery(query, options);
				const suffix = query.size === 0 ? "" : `?${query}`;
				const body = await client.#request(
					`/api/auth/${encodeURIComponent(collection)}/create-user${suffix}`,
					{ method: "POST", body: JSON.stringify({ data: input.data, password: input.password }) },
					options
				);
				return documentFromEnvelope(body);
			},
			bootstrap: async (input: CollectionChoice, options?: RequestOptions) => {
				const collection = client.#collectionFor(input.collection, "bootstrap");
				const body = await client.#request(
					`/api/auth/${encodeURIComponent(collection)}/bootstrap`,
					{ method: "GET" },
					options
				);
				if (!isRecord(body) || typeof body.available !== "boolean") {
					throw invalidSuccessEnvelope("auth bootstrap");
				}
				return { available: body.available };
			},
			requestPasswordReset: (
				input: { email: string } & CollectionChoice,
				options?: RequestOptions
			) => client.#authAction(input.collection, "forgot-password", { email: input.email }, options),
			resetPassword: (
				input: { token: string; password: string } & CollectionChoice,
				options?: RequestOptions
			) =>
				client.#authAction(
					input.collection,
					"reset-password",
					{ token: input.token, password: input.password },
					options
				),
			requestVerification: (
				input: { email: string } & CollectionChoice,
				options?: RequestOptions
			) =>
				client.#authAction(
					input.collection,
					"request-verification",
					{ email: input.email },
					options
				),
			verifyEmail: (input: { token: string } & CollectionChoice, options?: RequestOptions) =>
				client.#authAction(input.collection, "verify", { token: input.token }, options),
			forceUnlock: async (input: { id: string } & CollectionChoice, options?: RequestOptions) => {
				const collection = client.#collectionFor(input.collection, "forceUnlock");
				const body = await client.#request(
					`/api/auth/${encodeURIComponent(collection)}/${encodeDocumentID(input.id)}/unlock`,
					{ method: "POST" },
					options
				);
				if (!isRecord(body) || body.success !== true) {
					throw invalidSuccessEnvelope("account unlock");
				}
				return { success: true as const };
			},
			createAPIKey: async (input: CreateAPIKeyInput, options?: RequestOptions) => {
				const body = await client.#request(
					"/api/auth/api-keys",
					{ method: "POST", body: JSON.stringify(input) },
					options
				);
				if (!isRecord(body) || !isAPIKey(body.apiKey, true)) {
					throw invalidSuccessEnvelope("API key");
				}
				return body.apiKey;
			},
			apiKeys: async (options?: RequestOptions) => {
				const body = await client.#request("/api/auth/api-keys", { method: "GET" }, options);
				if (
					!isRecord(body) ||
					!Array.isArray(body.apiKeys) ||
					!body.apiKeys.every((key) => isAPIKey(key, false))
				) {
					throw invalidSuccessEnvelope("API keys");
				}
				return body.apiKeys;
			},
			revokeAPIKey: async (id: string, options?: RequestOptions) => {
				const body = await client.#request(
					`/api/auth/api-keys/${encodeURIComponent(id)}`,
					{ method: "DELETE" },
					options
				);
				if (!isRecord(body) || body.id !== id || body.deleted !== true) {
					throw invalidSuccessEnvelope("API key revocation");
				}
				return { id, deleted: true as const };
			},
		};
		return auth as unknown as RiduAuth<Config, DefaultAuth>;
	}

	#collectionFor(selected: string | undefined, operation: string): string {
		const collection = selected ?? this.#authCollection;
		if (collection === undefined || collection === "") {
			throw new TypeError(
				`auth.${operation} needs an auth collection; pass { collection } or configure auth.collection`
			);
		}
		return collection;
	}

	async #getSession(options?: RequestOptions) {
		const tokens = this.#tokens;
		const token = tokens?.token ?? undefined;
		// Without a stored token there is nothing to verify; skip the network.
		if (tokens !== undefined && token === undefined) return null;
		const memo = this.#sessionMemo;
		if (this.#memoizeSession && memo !== undefined && memo.token === token) {
			return (await memo.session) as SessionFor<
				Config,
				SessionCollection<Config, DefaultAuth>
			> | null;
		}
		const session = this.#verifySession(token, options);
		if (this.#memoizeSession) {
			const entry = { token, session };
			this.#sessionMemo = entry;
			// A failure is not a verdict about the session; the next call must retry.
			session.catch(() => {
				if (this.#sessionMemo === entry) this.#sessionMemo = undefined;
			});
		}
		return (await session) as SessionFor<Config, SessionCollection<Config, DefaultAuth>> | null;
	}

	async #verifySession(token: string | undefined, options?: RequestOptions) {
		let body: unknown;
		try {
			body = await this.#request("/api/auth/me", { method: "GET" }, options, {
				json: true,
				credential: true,
				token,
			});
		} catch (error) {
			// 401 means "no valid session". Every other failure, including an outage, rejects.
			if (error instanceof RiduError && error.status === 401) return null;
			throw error;
		}
		const session = sessionFromEnvelope<unknown>(body) as AnySession;
		if (this.#authCollection !== undefined && session.collection !== this.#authCollection) {
			return null;
		}
		if (token !== undefined) this.#tokens?.verified?.(token, session);
		return session;
	}

	async #forget(token: string | undefined) {
		this.#sessionMemo = undefined;
		if (token !== undefined) await this.#tokens?.cleared(token);
	}

	async #authAction(
		selected: string | undefined,
		action: string,
		input: Record<string, string>,
		options?: RequestOptions
	): Promise<AuthActionEnvelope> {
		const collection = this.#collectionFor(selected, action);
		const body = await this.#request(
			`/api/auth/${encodeURIComponent(collection)}/${action}`,
			{ method: "POST", body: JSON.stringify(input) },
			options
		);
		if (!isRecord(body) || body.success !== true) {
			throw invalidSuccessEnvelope("auth action");
		}
		return { success: true };
	}

	async getUploadURL<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: UploadURLOptions
	): Promise<UploadURL> {
		const [url] = await this.getUploadURLs(
			collection,
			[options?.size === undefined ? id : { id, size: options.size }],
			options
		);
		if (url === undefined) throw invalidSuccessEnvelope("upload URL");
		return url;
	}

	async getUploadURLs<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		items: readonly (string | { id: string; size?: string })[],
		options?: Omit<UploadURLOptions, "size">
	): Promise<UploadURL[]> {
		const requested = items.map((item) =>
			typeof item === "string"
				? { id: item }
				: { id: item.id, ...(item.size === undefined ? {} : { size: item.size }) }
		);
		const batches: (typeof requested)[] = [];
		for (let index = 0; index < requested.length; index += UPLOAD_GRANT_BATCH) {
			batches.push(requested.slice(index, index + UPLOAD_GRANT_BATCH));
		}
		const results = await Promise.all(
			batches.map(async (batch) => {
				const body = await this.#request(
					`/api/uploads/${encodeURIComponent(collection)}/grants`,
					{
						method: "POST",
						body: JSON.stringify({
							items: batch,
							...(options?.expiresIn === undefined ? {} : { expiresIn: options.expiresIn }),
						}),
					},
					options
				);
				if (
					!isRecord(body) ||
					!Array.isArray(body.grants) ||
					body.grants.length !== batch.length ||
					!body.grants.every(isUploadGrant)
				) {
					throw invalidSuccessEnvelope("upload grants");
				}
				return body.grants.map((grant) => ({
					url: new URL(grant.url, this.baseURL).href,
					expiresAt: grant.expiresAt,
				}));
			})
		);
		return results.flat();
	}

	async schema(options?: RequestOptions): Promise<SchemaManifest> {
		const body = await this.#request("/api/schema", { method: "GET" }, options);
		if (!isRecord(body) || !isRecord(body.schema) || !Array.isArray(body.schema.collections)) {
			throw invalidSuccessEnvelope("schema");
		}
		return bindSchemaManifest(body.schema as unknown as SchemaManifest);
	}

	async request(path: string, init: RequestInit = {}, options?: RequestOptions): Promise<Response> {
		return this.#response(path, init, options, { json: false, credential: true });
	}

	async requestPlugin<Result = unknown>(
		plugin: string,
		path: string,
		body: unknown,
		options?: RequestOptions
	): Promise<Result> {
		if (!/^[a-z][a-z0-9-]*$/.test(plugin)) throw new TypeError("invalid plugin key");
		const segments = path.split("/");
		if (
			path.length === 0 ||
			path.trim() !== path ||
			/[\s?#*]/u.test(path) ||
			segments.some((segment) => segment === "" || segment === "." || segment === "..")
		) {
			throw new TypeError("invalid plugin endpoint path");
		}
		const encodedPath = segments.map((segment) => encodeURIComponent(segment)).join("/");
		return (await this.#request(
			`/api/plugins/${encodeURIComponent(plugin)}/${encodedPath}`,
			{ method: "POST", body: JSON.stringify(body) },
			options
		)) as Result;
	}

	/**
	 * Compiled admin read. The result stays unknown here because only the generated
	 * loader reference carries this application's Go-derived output contract.
	 */
	async adminLoad(key: string, route: string, options?: RequestOptions): Promise<unknown> {
		return this.#request(
			`/api/admin/loaders/${encodeURIComponent(key)}?${new URLSearchParams({ route })}`,
			{ method: "GET" },
			options
		);
	}

	/** Framework list reads share the planner and decoder used for embedded route data. */
	async adminCollectionList(
		collection: string,
		query: string,
		part: "page" | "counts",
		options?: RequestOptions
	): Promise<AdminCollectionListDataV1> {
		const params = new URLSearchParams(query);
		params.set("part", part);
		const body = await this.#request(
			`/api/admin/collection-list/${encodeURIComponent(collection)}?${params}`,
			{ method: "GET" },
			options
		);
		if (!isAdminCollectionListData(body) || (part === "page" && body.page === undefined))
			throw new TypeError("Invalid collection list response");
		return body;
	}

	async preference<Value = unknown>(key: string, options?: RequestOptions): Promise<Value> {
		const body = await this.#request(
			`/api/preferences/${encodeURIComponent(key)}`,
			{ method: "GET" },
			options
		);
		if (!isRecord(body) || !("value" in body)) throw invalidSuccessEnvelope("preference");
		return body.value as Value;
	}

	async setPreference<Value>(key: string, value: Value, options?: RequestOptions): Promise<Value> {
		const body = await this.#request(
			`/api/preferences/${encodeURIComponent(key)}`,
			{ method: "PUT", body: JSON.stringify({ value }) },
			options
		);
		if (!isRecord(body) || !("value" in body)) throw invalidSuccessEnvelope("preference");
		return body.value as Value;
	}

	async deletePreference(key: string, options?: RequestOptions): Promise<DeleteEnvelope> {
		const body = await this.#request(
			`/api/preferences/${encodeURIComponent(key)}`,
			{ method: "DELETE" },
			options
		);
		if (!isRecord(body) || body.deleted !== true || typeof body.id !== "string") {
			throw invalidSuccessEnvelope("delete preference");
		}
		return { id: body.id, deleted: true };
	}

	async resetPreferences(options?: RequestOptions): Promise<AuthActionEnvelope> {
		const body = await this.#request("/api/preferences", { method: "DELETE" }, options);
		if (!isRecord(body) || body.success !== true) {
			throw invalidSuccessEnvelope("preference reset");
		}
		return { success: true };
	}

	async collectionLiveValidation<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		input: LiveValidationRequest,
		options?: LiveValidationOptions
	): Promise<LiveValidationEnvelope> {
		const query = new URLSearchParams();
		if (options?.locale !== undefined) query.set("locale", options.locale);
		const suffix = query.size === 0 ? "" : `?${query}`;
		return liveValidationFromEnvelope(
			await this.#request(
				`/api/collections/${encodeURIComponent(collection)}/validate${suffix}`,
				{ method: "POST", body: JSON.stringify(input) },
				options
			)
		);
	}

	async globalLiveValidation<Slug extends GlobalSlug<Config>>(
		slug: Slug,
		input: Omit<LiveValidationRequest, "id"> & { id?: never },
		options?: LiveValidationOptions
	): Promise<LiveValidationEnvelope> {
		const query = new URLSearchParams();
		if (options?.locale !== undefined) query.set("locale", options.locale);
		const suffix = query.size === 0 ? "" : `?${query}`;
		return liveValidationFromEnvelope(
			await this.#request(
				`/api/globals/${encodeURIComponent(slug)}/validate${suffix}`,
				{ method: "POST", body: JSON.stringify(input) },
				options
			)
		);
	}

	async collectionAccess<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		options?: CollectionAccessOptions<CreateFor<Config, Slug> & UpdateFor<Config, Slug>>
	): Promise<AccessCapabilitiesEnvelope> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/access/collections/${encodeURIComponent(collection)}${suffix}`,
			{
				method: "POST",
				body: JSON.stringify({
					...(options?.id === undefined ? {} : { id: options.id }),
					...(options?.data === undefined ? {} : { data: options.data }),
					...(options?.trash === true ? { trash: true } : {}),
				}),
			},
			options
		);
		return accessCapabilitiesFromEnvelope(body);
	}

	async resolveFilteredSelection<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		options?: CollectionSelectionOptions<WhereFor<Config, Slug>>
	): Promise<CollectionSelectionEnvelope> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/access/collections/${encodeURIComponent(collection)}/selection${suffix}`,
			{
				method: "POST",
				body: JSON.stringify({
					...(options?.where === undefined ? {} : { where: options.where }),
					...(options?.trash === true ? { trash: true } : {}),
				}),
			},
			options
		);
		return collectionSelectionFromEnvelope(body);
	}

	async globalAccess<Slug extends GlobalSlug<Config>>(
		slug: Slug,
		options?: GlobalAccessOptions<GlobalUpdateFor<Config, Slug>>
	): Promise<AccessCapabilitiesEnvelope> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/access/globals/${encodeURIComponent(slug)}${suffix}`,
			{
				method: "POST",
				body: JSON.stringify(options?.data === undefined ? {} : { data: options.data }),
			},
			options
		);
		return accessCapabilitiesFromEnvelope(body);
	}

	async createPreviewToken<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<PreviewToken> {
		const body = await this.#request(
			`/api/preview/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/token`,
			{ method: "POST" },
			options
		);
		return previewTokenFromEnvelope(body);
	}

	async revokePreviewToken(token: string, options?: RequestOptions): Promise<AuthActionEnvelope> {
		const body = await this.#request(
			"/api/preview/token/revoke",
			{ method: "POST", body: JSON.stringify({ token }) },
			options
		);
		if (!isRecord(body) || body.success !== true) {
			throw invalidSuccessEnvelope("preview token revocation");
		}
		return { success: true };
	}

	async preview<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		token: string,
		options?: RequestOptions
	): Promise<OutputFor<Config, Slug>> {
		const body = await this.#request(
			`/api/preview/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}`,
			{ method: "GET" },
			previewRequestOptions(options, token)
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async createGlobalPreviewToken<Slug extends DraftGlobalSlug<Config>>(
		slug: Slug,
		options?: RequestOptions
	): Promise<PreviewToken> {
		const body = await this.#request(
			`/api/preview/globals/${encodeURIComponent(slug)}/token`,
			{ method: "POST" },
			options
		);
		return previewTokenFromEnvelope(body);
	}

	async previewGlobal<Slug extends DraftGlobalSlug<Config>>(
		slug: Slug,
		token: string,
		options?: RequestOptions
	): Promise<GlobalOutputFor<Config, Slug>> {
		const body = await this.#request(
			`/api/preview/globals/${encodeURIComponent(slug)}`,
			{ method: "GET" },
			previewRequestOptions(options, token)
		);
		return documentFromEnvelope<GlobalOutputFor<Config, Slug>>(body);
	}

	async documentLock<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<DocumentLockEnvelope> {
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/lock`,
			{ method: "GET" },
			options
		);
		return documentLockFromEnvelope(body);
	}

	async acquireDocumentLock<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		takeover = false,
		options?: RequestOptions
	): Promise<DocumentLockEnvelope> {
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/lock`,
			{ method: "POST", body: JSON.stringify({ takeover }) },
			options
		);
		return documentLockFromEnvelope(body);
	}

	async releaseDocumentLock<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<DeleteEnvelope> {
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/lock`,
			{ method: "DELETE" },
			options
		);
		if (!isRecord(body) || body.id !== id || body.deleted !== true) {
			throw invalidSuccessEnvelope("document lock release");
		}
		return { id, deleted: true };
	}

	async list<
		Slug extends CollectionSlug<Config>,
		const Options extends
			| ListOptions<
					WhereFor<Config, Slug>,
					SelectFor<Config, Slug>,
					PopulateFor<Config, Slug>,
					LocaleFor<Config>
			  >
			| undefined = undefined,
	>(
		collection: Slug,
		options?: Options
	): Promise<CollectionListResult<CollectionQueryResult<Config, Slug, Options>, Options>> {
		const query = new URLSearchParams();
		if (options?.page !== undefined) query.set("page", String(options.page));
		if (options?.limit !== undefined) query.set("limit", String(options.limit));
		if (options?.depth !== undefined) query.set("depth", String(options.depth));
		if (options?.where !== undefined) query.set("where", JSON.stringify(options.where));
		if (options?.select !== undefined) query.set("select", JSON.stringify(options.select));
		if (options?.populate !== undefined) query.set("populate", JSON.stringify(options.populate));
		if (options?.includeAccess === true) query.set("include-access", "true");
		if (options?.trash === true) query.set("trash", "true");
		appendLocaleQuery(query, options);
		for (const sort of options?.sort ?? []) query.append("sort", sort);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}${suffix}`,
			{ method: "GET" },
			options
		);
		if (!isPageEnvelope<CollectionQueryResult<Config, Slug, Options>>(body)) {
			throw invalidSuccessEnvelope("page");
		}
		if (options?.includeAccess !== true) {
			return body as CollectionListResult<CollectionQueryResult<Config, Slug, Options>, Options>;
		}
		const page = collectionPageFromEnvelope(body);
		return page as CollectionListResult<CollectionQueryResult<Config, Slug, Options>, Options>;
	}

	async count<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		options?: Pick<
			ListOptions<WhereFor<Config, Slug>, never, never, LocaleFor<Config>>,
			"where" | "trash" | "locale" | "fallbackLocale" | "signal" | "headers"
		>
	): Promise<CountEnvelope> {
		const query = new URLSearchParams();
		if (options?.where !== undefined) query.set("where", JSON.stringify(options.where));
		if (options?.trash === true) query.set("trash", "true");
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/count${suffix}`,
			{ method: "GET" },
			options
		);
		if (!isRecord(body) || typeof body.totalDocs !== "number") {
			throw invalidSuccessEnvelope("count");
		}
		return { totalDocs: body.totalDocs };
	}

	async find<
		Slug extends CollectionSlug<Config>,
		const Options extends
			| FindOptions<SelectFor<Config, Slug>, PopulateFor<Config, Slug>, LocaleFor<Config>>
			| undefined = undefined,
	>(
		collection: Slug,
		id: string,
		options?: Options
	): Promise<CollectionQueryResult<Config, Slug, Options>> {
		const query = new URLSearchParams();
		if (options?.depth !== undefined) query.set("depth", String(options.depth));
		if (options?.select !== undefined) query.set("select", JSON.stringify(options.select));
		if (options?.populate !== undefined) query.set("populate", JSON.stringify(options.populate));
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}${suffix}`,
			{ method: "GET" },
			options
		);
		return documentFromEnvelope<CollectionQueryResult<Config, Slug, Options>>(body);
	}

	async create<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		data: CreateFor<Config, Slug>,
		options?: CreateOptions<
			LocaleFor<Config>,
			CollectionVersionsFor<Config, Slug>,
			CollectionDraftsFor<Config, Slug>
		>
	): Promise<OutputFor<Config, Slug>> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		appendDraftQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}${suffix}`,
			{ method: "POST", body: JSON.stringify(data) },
			options
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async duplicate<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		overrides: Partial<Omit<CreateFor<Config, Slug>, "id">> = {},
		options?: MutationLocaleOptions
	): Promise<OutputFor<Config, Slug>> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/duplicate${suffix}`,
			{ method: "POST", body: JSON.stringify(overrides) },
			options
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async copyLocale<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		input: CopyLocaleInput,
		options?: RevisionOptions
	): Promise<OutputFor<Config, Slug>> {
		const revision = revisionHeaders(options);
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/copy-locale`,
			{
				method: "POST",
				body: JSON.stringify(input),
				...(revision === undefined ? {} : { headers: revision }),
			},
			options
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async update<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		data: UpdateFor<Config, Slug>,
		options?: MutationOptions
	): Promise<OutputFor<Config, Slug>> {
		const revision = revisionHeaders(options);
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}${suffix}`,
			{
				method: "PATCH",
				body: JSON.stringify(data),
				...(revision === undefined ? {} : { headers: revision }),
			},
			options
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async upload<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		file: Blob,
		options?: UploadOptions<CreateFor<Config, Slug>>
	): Promise<OutputFor<Config, Slug>> {
		const form = new FormData();
		if (options?.filename) form.set("file", file, options.filename);
		else form.set("file", file);
		if (options?.image) form.set("image", JSON.stringify(options.image));
		if (options?.publish) form.set("publish", "true");
		if (options?.data !== undefined) form.set("data", JSON.stringify(options.data));
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			"/api/collections/" + encodeURIComponent(collection) + suffix,
			{ method: "POST", body: form },
			options
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async uploadFromURL<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		url: string,
		options?: UploadOptions<CreateFor<Config, Slug>>
	): Promise<OutputFor<Config, Slug>> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/remote-upload${suffix}`,
			{
				method: "POST",
				body: JSON.stringify({
					url,
					data: options?.data ?? {},
					image: options?.image,
					publish: options?.publish,
					filename: options?.filename,
				}),
			},
			options
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async updateUpload<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		input: UpdateUploadInput<UpdateFor<Config, Slug>>,
		options?: MutationOptions
	): Promise<OutputFor<Config, Slug>> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		let payload: FormData | string;
		if (input.file) {
			const form = new FormData();
			if (input.filename) form.set("file", input.file, input.filename);
			else form.set("file", input.file);
			if (input.data) form.set("data", JSON.stringify(input.data));
			if (input.image) form.set("image", JSON.stringify(input.image));
			if (input.publish) form.set("publish", "true");
			payload = form;
		} else payload = JSON.stringify(input);
		const revision = revisionHeaders(options);
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/upload${suffix}`,
			{ method: "PATCH", body: payload, ...(revision === undefined ? {} : { headers: revision }) },
			options
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async readUploadSource<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<Blob> {
		const response = await this.#response(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/upload-source`,
			{ method: "GET" },
			options,
			{ json: false, credential: true }
		);
		if (!response.ok) throw await RiduError.fromResponse(response);
		return response.blob();
	}

	async previewUploadFromURL<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		url: string,
		options?: RequestOptions & { id?: string }
	): Promise<UploadFile> {
		const response = await this.#response(
			`/api/collections/${encodeURIComponent(collection)}/upload-preview`,
			{ method: "POST", body: JSON.stringify({ url, id: options?.id }) },
			options,
			{ json: true, credential: true }
		);
		if (!response.ok) throw await RiduError.fromResponse(response);
		const disposition = response.headers.get("Content-Disposition") ?? "";
		const filename =
			/filename="([^"\\]*(?:\\.[^"\\]*)*)"/i.exec(disposition)?.[1]?.replace(/\\(.)/g, "$1") ??
			/filename=([^;]+)/i.exec(disposition)?.[1]?.trim() ??
			"upload";
		return { blob: await response.blob(), filename };
	}

	async versions<
		Slug extends VersionCollectionSlug<Config>,
		const Options extends LocaleOptions<LocaleFor<Config>> | undefined = undefined,
	>(
		collection: Slug,
		id: string,
		options?: Options
	): Promise<
		Version<LocaleResult<OutputFor<Config, Slug>, AllLocalesOutputFor<Config, Slug>, Options>>[]
	> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			"/api/collections/" +
				encodeURIComponent(collection) +
				"/" +
				encodeDocumentID(id) +
				`/versions${suffix}`,
			{ method: "GET" },
			options
		);
		if (!isRecord(body) || !Array.isArray(body.versions)) {
			throw invalidSuccessEnvelope("versions");
		}
		return body.versions as Version<
			LocaleResult<OutputFor<Config, Slug>, AllLocalesOutputFor<Config, Slug>, Options>
		>[];
	}

	async version<
		Slug extends VersionCollectionSlug<Config>,
		const Options extends LocaleOptions<LocaleFor<Config>> | undefined = undefined,
	>(
		collection: Slug,
		id: string,
		revision: number,
		options?: Options
	): Promise<
		Version<LocaleResult<OutputFor<Config, Slug>, AllLocalesOutputFor<Config, Slug>, Options>>
	> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/versions/${revision}${suffix}`,
			{ method: "GET" },
			options
		);
		if (!isRecord(body) || !isRecord(body.version)) {
			throw invalidSuccessEnvelope("version");
		}
		return body.version as unknown as Version<
			LocaleResult<OutputFor<Config, Slug>, AllLocalesOutputFor<Config, Slug>, Options>
		>;
	}

	async schedulePublish<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		runAt: string | Date,
		options?: PublicationScheduleOptions
	): Promise<ScheduledPublication> {
		return this.#schedulePublication(collection, id, runAt, "publish", options);
	}

	async scheduleUnpublish<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		runAt: string | Date,
		options?: PublicationScheduleOptions
	): Promise<ScheduledPublication> {
		return this.#schedulePublication(collection, id, runAt, "unpublish", options);
	}

	async #schedulePublication<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		runAt: string | Date,
		action: ScheduledPublication["action"],
		options?: PublicationScheduleOptions
	): Promise<ScheduledPublication> {
		const headers = revisionHeaders(options);
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/schedule`,
			{
				method: "POST",
				body: JSON.stringify({
					action,
					runAt: runAt instanceof Date ? runAt.toISOString() : runAt,
					timeZone: options?.timeZone,
				}),
				...(headers === undefined ? {} : { headers }),
			},
			options
		);
		if (!isRecord(body) || !isScheduledPublication(body.scheduledPublication)) {
			throw invalidSuccessEnvelope("scheduled publication");
		}
		return body.scheduledPublication;
	}

	async scheduledPublications<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<ScheduledPublication[]> {
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/schedule`,
			{ method: "GET" },
			options
		);
		if (
			!isRecord(body) ||
			!Array.isArray(body.scheduledPublications) ||
			!body.scheduledPublications.every(isScheduledPublication)
		) {
			throw invalidSuccessEnvelope("scheduled publications");
		}
		return body.scheduledPublications;
	}

	async cancelScheduledPublication<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		jobID: string,
		options?: RequestOptions
	): Promise<DeleteEnvelope> {
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/schedule/${encodeURIComponent(jobID)}`,
			{ method: "DELETE" },
			options
		);
		if (!isRecord(body) || body.id !== jobID || body.deleted !== true) {
			throw invalidSuccessEnvelope("scheduled publication cancellation");
		}
		return { id: jobID, deleted: true };
	}

	async publish<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationOptions
	): Promise<OutputFor<Config, Slug>> {
		return this.#versionMutation(collection, id, "publish", options);
	}

	async publishChanges<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		data: UpdateFor<Config, Slug>,
		options?: MutationOptions
	): Promise<OutputFor<Config, Slug>> {
		return this.#versionMutation(collection, id, "publish", options, data);
	}

	async unpublish<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationOptions
	): Promise<OutputFor<Config, Slug>> {
		return this.#versionMutation(collection, id, "unpublish", options);
	}

	async restore<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		revision: number,
		options?: RestoreOptions<LocaleFor<Config>, CollectionDraftsFor<Config, Slug>>
	): Promise<OutputFor<Config, Slug>> {
		const query = options?.draft === true ? "?draft=true" : "";
		return this.#versionMutation(collection, id, "restore/" + revision + query, options);
	}

	async #versionMutation<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		action: string,
		options?: MutationOptions,
		data?: UpdateFor<Config, Slug>
	): Promise<OutputFor<Config, Slug>> {
		const revision = revisionHeaders(options);
		const [actionPath, encodedQuery = ""] = action.split("?", 2);
		const query = new URLSearchParams(encodedQuery);
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			"/api/collections/" +
				encodeURIComponent(collection) +
				"/" +
				encodeDocumentID(id) +
				"/" +
				actionPath +
				suffix,
			{
				method: "POST",
				...(data === undefined ? {} : { body: JSON.stringify(data) }),
				...(revision === undefined ? {} : { headers: revision }),
			},
			options
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async delete<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationLocaleOptions
	): Promise<DeleteEnvelope> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}${suffix}`,
			{ method: "DELETE" },
			options
		);
		if (!isRecord(body) || typeof body.id !== "string" || body.deleted !== true) {
			throw invalidSuccessEnvelope("delete");
		}
		return { id: body.id, deleted: true };
	}

	async mutateJoin<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		field: string,
		input: { additions: string[]; removals: string[] },
		options?: MutationLocaleOptions
	): Promise<{ doc: OutputFor<Config, Slug>; added: number; removed: number }> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/joins/${encodeURIComponent(field)}${suffix}`,
			{ method: "PATCH", body: JSON.stringify(input) },
			options
		);
		if (
			!isRecord(body) ||
			!isRecord(body.doc) ||
			typeof body.added !== "number" ||
			!Number.isInteger(body.added) ||
			typeof body.removed !== "number" ||
			!Number.isInteger(body.removed)
		) {
			throw invalidSuccessEnvelope("join mutation");
		}
		return {
			doc: body.doc as OutputFor<Config, Slug>,
			added: body.added,
			removed: body.removed,
		};
	}

	async bulkUpdate<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		data: UpdateFor<Config, Slug>,
		options?: MutationLocaleOptions
	): Promise<OutputFor<Config, Slug>[]> {
		return this.#bulk(collection, "update", ids, data, options);
	}

	async bulkPublish<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions
	): Promise<OutputFor<Config, Slug>[]> {
		return this.#bulk(collection, "publish", ids, undefined, options);
	}

	async bulkUnpublish<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions
	): Promise<OutputFor<Config, Slug>[]> {
		return this.#bulk(collection, "unpublish", ids, undefined, options);
	}

	async bulkDelete<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions
	): Promise<OutputFor<Config, Slug>[]> {
		return this.#bulk(collection, "delete", ids, undefined, options);
	}

	async bulkRestoreDeleted<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions
	): Promise<OutputFor<Config, Slug>[]> {
		return this.#bulk(collection, "restoreDeleted", ids, undefined, options);
	}

	async bulkDeletePermanent<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions
	): Promise<OutputFor<Config, Slug>[]> {
		return this.#bulk(collection, "deletePermanent", ids, undefined, options);
	}

	async emptyTrash<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		options?: MutationLocaleOptions
	): Promise<OutputFor<Config, Slug>[]> {
		const query = new URLSearchParams();
		query.set("trash", "true");
		appendLocaleQuery(query, options);
		const suffix = `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}${suffix}`,
			{ method: "DELETE" },
			options
		);
		if (!isRecord(body) || !Array.isArray(body.docs) || !body.docs.every(isRecord)) {
			throw invalidSuccessEnvelope("empty trash");
		}
		return body.docs as OutputFor<Config, Slug>[];
	}

	async #bulk<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		action: "update" | "publish" | "unpublish" | "delete" | "restoreDeleted" | "deletePermanent",
		ids: readonly string[],
		data: UpdateFor<Config, Slug> | undefined,
		options?: MutationLocaleOptions
	): Promise<OutputFor<Config, Slug>[]> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/bulk${suffix}`,
			{
				method: "POST",
				body: JSON.stringify({ action, ids, ...(data === undefined ? {} : { data }) }),
			},
			options
		);
		if (!isRecord(body) || !Array.isArray(body.docs) || !body.docs.every(isRecord)) {
			throw invalidSuccessEnvelope("bulk operation");
		}
		return body.docs as OutputFor<Config, Slug>[];
	}

	async restoreDeleted<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationLocaleOptions
	): Promise<OutputFor<Config, Slug>> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/restore-deleted${suffix}`,
			{ method: "POST" },
			options
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async deletePermanent<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationLocaleOptions
	): Promise<DeleteEnvelope> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/permanent${suffix}`,
			{ method: "DELETE" },
			options
		);
		if (!isRecord(body) || typeof body.id !== "string" || body.deleted !== true) {
			throw invalidSuccessEnvelope("permanent delete");
		}
		return { id: body.id, deleted: true };
	}

	async global<
		Slug extends GlobalSlug<Config>,
		const Options extends
			| FindOptions<
					GlobalSelectFor<Config, Slug>,
					GlobalPopulateFor<Config, Slug>,
					LocaleFor<Config>
			  >
			| undefined = undefined,
	>(slug: Slug, options?: Options): Promise<GlobalQueryResult<Config, Slug, Options>> {
		const query = new URLSearchParams();
		if (options?.depth !== undefined) query.set("depth", String(options.depth));
		if (options?.select !== undefined) query.set("select", JSON.stringify(options.select));
		if (options?.populate !== undefined) query.set("populate", JSON.stringify(options.populate));
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/globals/${encodeURIComponent(slug)}${suffix}`,
			{ method: "GET" },
			options
		);
		return documentFromEnvelope<GlobalQueryResult<Config, Slug, Options>>(body);
	}

	async updateGlobal<Slug extends GlobalSlug<Config>>(
		slug: Slug,
		data: GlobalUpdateFor<Config, Slug>,
		options?: MutationOptions
	): Promise<GlobalOutputFor<Config, Slug>> {
		const revision = revisionHeaders(options);
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/globals/${encodeURIComponent(slug)}${suffix}`,
			{
				method: "PATCH",
				body: JSON.stringify(data),
				...(revision === undefined ? {} : { headers: revision }),
			},
			options
		);
		return documentFromEnvelope<GlobalOutputFor<Config, Slug>>(body);
	}

	async copyGlobalLocale<Slug extends GlobalSlug<Config>>(
		slug: Slug,
		input: CopyLocaleInput,
		options?: RevisionOptions
	): Promise<GlobalOutputFor<Config, Slug>> {
		const revision = revisionHeaders(options);
		const body = await this.#request(
			`/api/globals/${encodeURIComponent(slug)}/copy-locale`,
			{
				method: "POST",
				body: JSON.stringify(input),
				...(revision === undefined ? {} : { headers: revision }),
			},
			options
		);
		return documentFromEnvelope<GlobalOutputFor<Config, Slug>>(body);
	}

	async globalVersions<
		Slug extends VersionGlobalSlug<Config>,
		const Options extends LocaleOptions<LocaleFor<Config>> | undefined = undefined,
	>(
		slug: Slug,
		options?: Options
	): Promise<
		Version<
			LocaleResult<GlobalOutputFor<Config, Slug>, GlobalAllLocalesOutputFor<Config, Slug>, Options>
		>[]
	> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/globals/${encodeURIComponent(slug)}/versions${suffix}`,
			{ method: "GET" },
			options
		);
		if (!isRecord(body) || !Array.isArray(body.versions)) {
			throw invalidSuccessEnvelope("global versions");
		}
		return body.versions as Version<
			LocaleResult<GlobalOutputFor<Config, Slug>, GlobalAllLocalesOutputFor<Config, Slug>, Options>
		>[];
	}

	async globalVersion<
		Slug extends VersionGlobalSlug<Config>,
		const Options extends LocaleOptions<LocaleFor<Config>> | undefined = undefined,
	>(
		slug: Slug,
		revision: number,
		options?: Options
	): Promise<
		Version<
			LocaleResult<GlobalOutputFor<Config, Slug>, GlobalAllLocalesOutputFor<Config, Slug>, Options>
		>
	> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/globals/${encodeURIComponent(slug)}/versions/${revision}${suffix}`,
			{ method: "GET" },
			options
		);
		if (!isRecord(body) || !isRecord(body.version)) {
			throw invalidSuccessEnvelope("global version");
		}
		return body.version as unknown as Version<
			LocaleResult<GlobalOutputFor<Config, Slug>, GlobalAllLocalesOutputFor<Config, Slug>, Options>
		>;
	}

	async publishGlobal<Slug extends VersionGlobalSlug<Config>>(
		slug: Slug,
		options?: MutationOptions
	): Promise<GlobalOutputFor<Config, Slug>> {
		return this.#globalVersionMutation(slug, "publish", options);
	}

	async publishGlobalChanges<Slug extends VersionGlobalSlug<Config>>(
		slug: Slug,
		data: GlobalUpdateFor<Config, Slug>,
		options?: MutationOptions
	): Promise<GlobalOutputFor<Config, Slug>> {
		return this.#globalVersionMutation(slug, "publish", options, data);
	}

	async unpublishGlobal<Slug extends DraftGlobalSlug<Config>>(
		slug: Slug,
		options?: MutationOptions
	): Promise<GlobalOutputFor<Config, Slug>> {
		return this.#globalVersionMutation(slug, "unpublish", options);
	}

	async restoreGlobal<Slug extends VersionGlobalSlug<Config>>(
		slug: Slug,
		revision: number,
		options?: RestoreOptions<LocaleFor<Config>, GlobalDraftsFor<Config, Slug>>
	): Promise<GlobalOutputFor<Config, Slug>> {
		return this.#globalVersionMutation(
			slug,
			`restore/${revision}${options?.draft === true ? "?draft=true" : ""}`,
			options
		);
	}

	async #globalVersionMutation<Slug extends GlobalSlug<Config>>(
		slug: Slug,
		action: string,
		options?: MutationOptions,
		data?: GlobalUpdateFor<Config, Slug>
	): Promise<GlobalOutputFor<Config, Slug>> {
		const revision = revisionHeaders(options);
		const [actionPath, encodedQuery = ""] = action.split("?", 2);
		const query = new URLSearchParams(encodedQuery);
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/globals/${encodeURIComponent(slug)}/${actionPath}${suffix}`,
			{
				method: "POST",
				...(data === undefined ? {} : { body: JSON.stringify(data) }),
				...(revision === undefined ? {} : { headers: revision }),
			},
			options
		);
		return documentFromEnvelope<GlobalOutputFor<Config, Slug>>(body);
	}

	async #request(
		path: string,
		init: RequestInit,
		options?: RequestOptions,
		behavior: RequestBehavior = { json: true, credential: true }
	): Promise<unknown> {
		const response = await this.#response(path, init, options, behavior);
		if (!response.ok) {
			throw await RiduError.fromResponse(response);
		}
		try {
			return await response.json();
		} catch {
			throw invalidSuccessEnvelope("JSON");
		}
	}

	async #response(
		path: string,
		init: RequestInit,
		options: RequestOptions | undefined,
		behavior: RequestBehavior
	): Promise<Response> {
		// Read the token before any await so the credential sent is the one current at the call.
		const storedToken =
			behavior.credential && this.#tokens !== undefined
				? (behavior.token ?? this.#tokens.token ?? undefined)
				: undefined;
		if (!path.startsWith("/") || path.startsWith("//") || path.includes("#")) {
			throw new TypeError("request path must be an absolute-path reference");
		}
		const target = new URL(path, this.baseURL);
		if (target.origin !== new URL(this.baseURL).origin || target.hash !== "") {
			throw new TypeError("request path must stay on the configured origin and omit fragments");
		}
		const headers = new Headers(await resolveHeaders(this.#headers));
		for (const [name, value] of new Headers(init.headers)) headers.set(name, value);
		for (const [name, value] of new Headers(options?.headers)) headers.set(name, value);
		// An explicit Authorization header, such as a preview token, is never replaced.
		let sentToken: string | undefined;
		if (storedToken !== undefined && !headers.has("authorization")) {
			sentToken = storedToken;
			headers.set("authorization", `Session ${sentToken}`);
		}
		if (
			behavior.json &&
			init.body !== undefined &&
			!(init.body instanceof FormData) &&
			!headers.has("content-type")
		) {
			headers.set("content-type", "application/json");
		}
		const request = new Request(target, {
			...init,
			headers,
			credentials: this.#credentials,
			...(options?.signal === undefined ? {} : { signal: options.signal }),
			...(options?.keepalive === undefined ? {} : { keepalive: options.keepalive }),
		});
		const response = await this.#dispatch(request);
		if (sentToken !== undefined && response.status === 401) {
			await this.#rejected(response, sentToken);
		}
		return response;
	}

	/** Forget a stored token that Ridu rejected, unless a newer token has replaced it. */
	async #rejected(response: Response, token: string) {
		let body: unknown;
		try {
			body = await response.clone().json();
		} catch {
			return;
		}
		if (isErrorEnvelope(body) && body.error.code === "invalid_credential") {
			await this.#forget(token);
		}
	}
}

function revisionHeaders(options?: MutationOptions) {
	return options?.revision === undefined ? undefined : { "If-Match": '"' + options.revision + '"' };
}

function encodeDocumentID(id: string): string {
	return encodeURIComponent(id);
}

function previewRequestOptions(options: RequestOptions | undefined, token: string): RequestOptions {
	const headers = new Headers(options?.headers);
	headers.set("Authorization", `Bearer ${token}`);
	return { ...options, headers };
}

function composeMiddleware(
	middleware: readonly NonNullable<ClientOptions["middleware"]>[number][],
	terminal: MiddlewareNext
) {
	return middleware.reduceRight<MiddlewareNext>(
		(next, current) => (request) => current(request, next),
		terminal
	);
}

async function resolveHeaders(headers: ClientOptions["headers"]) {
	return typeof headers === "function" ? await headers() : headers;
}

function documentFromEnvelope<Document>(value: unknown) {
	if (!isRecord(value) || !("doc" in value)) {
		throw invalidSuccessEnvelope("document");
	}
	return value.doc as Document;
}

function previewTokenFromEnvelope(value: unknown): PreviewToken {
	if (
		!isRecord(value) ||
		!isRecord(value.previewToken) ||
		typeof value.previewToken.token !== "string" ||
		(value.previewToken.resource !== "collection" && value.previewToken.resource !== "global") ||
		typeof value.previewToken.slug !== "string" ||
		typeof value.previewToken.documentId !== "string" ||
		typeof value.previewToken.expiresAt !== "string"
	) {
		throw invalidSuccessEnvelope("preview token");
	}
	return value.previewToken as unknown as PreviewToken;
}

function accessCapabilitiesFromEnvelope(value: unknown): AccessCapabilitiesEnvelope {
	if (!isAccessCapabilities(value)) throw invalidSuccessEnvelope("access capabilities");
	return value;
}

function collectionPageFromEnvelope<Document>(
	value: PageEnvelope<Document>
): CollectionPageEnvelope<Document> {
	const access = Reflect.get(value, "access");
	if (!isRecord(access)) throw invalidSuccessEnvelope("collection page access");
	const collection = accessCapabilitiesFromEnvelope(access.collection);
	if (!isRecord(access.documents)) {
		throw invalidSuccessEnvelope("collection page document access");
	}
	const documents: Record<string, AccessCapabilitiesEnvelope> = {};
	for (const document of value.docs) {
		if (!isRecord(document) || typeof document.id !== "string") {
			throw invalidSuccessEnvelope("collection page document");
		}
		documents[document.id] = accessCapabilitiesFromEnvelope(access.documents[document.id]);
	}
	return { docs: value.docs, pagination: value.pagination, access: { collection, documents } };
}

function collectionSelectionFromEnvelope(value: unknown): CollectionSelectionEnvelope {
	if (
		!isRecord(value) ||
		!Array.isArray(value.items) ||
		value.items.length > 100 ||
		typeof value.totalDocs !== "number" ||
		!Number.isInteger(value.totalDocs) ||
		value.totalDocs !== value.items.length
	) {
		throw invalidSuccessEnvelope("filtered collection selection");
	}
	const seen = new Set<string>();
	let previousID: string | undefined;
	const items = value.items.map((item) => {
		if (
			!isRecord(item) ||
			typeof item.id !== "string" ||
			item.id.length === 0 ||
			!isRecord(item.access)
		) {
			throw invalidSuccessEnvelope("filtered collection selection item");
		}
		if (seen.has(item.id) || (previousID !== undefined && compareUTF8(previousID, item.id) >= 0)) {
			throw invalidSuccessEnvelope("filtered collection selection IDs");
		}
		seen.add(item.id);
		previousID = item.id;
		return { id: item.id, access: accessCapabilitiesFromEnvelope(item.access) };
	});
	return { items, totalDocs: value.totalDocs };
}

function compareUTF8(left: string, right: string) {
	const encoder = new TextEncoder();
	const leftBytes = encoder.encode(left);
	const rightBytes = encoder.encode(right);
	const length = Math.min(leftBytes.length, rightBytes.length);
	for (let index = 0; index < length; index += 1) {
		const difference = (leftBytes[index] ?? 0) - (rightBytes[index] ?? 0);
		if (difference !== 0) return difference;
	}
	return leftBytes.length - rightBytes.length;
}

function documentLockFromEnvelope(value: unknown): DocumentLockEnvelope {
	if (
		!isRecord(value) ||
		typeof value.owned !== "boolean" ||
		typeof value.acquired !== "boolean" ||
		typeof value.canTakeOver !== "boolean" ||
		(value.lock !== null &&
			(!isRecord(value.lock) ||
				typeof value.lock.documentId !== "string" ||
				typeof value.lock.ownerId !== "string" ||
				typeof value.lock.ownerLabel !== "string" ||
				typeof value.lock.createdAt !== "string" ||
				typeof value.lock.updatedAt !== "string" ||
				typeof value.lock.expiresAt !== "string"))
	) {
		throw invalidSuccessEnvelope("document lock");
	}
	return value as unknown as DocumentLockEnvelope;
}

function isUploadGrant(value: unknown): value is UploadGrant {
	return (
		isRecord(value) &&
		typeof value.id === "string" &&
		typeof value.url === "string" &&
		typeof value.expiresAt === "string" &&
		(value.size === undefined || typeof value.size === "string")
	);
}

function sessionTokenFromEnvelope(value: unknown) {
	if (!isRecord(value) || typeof value.token !== "string" || value.token === "") {
		throw invalidSuccessEnvelope("session token");
	}
	return { session: sessionFromEnvelope<unknown>(value) as AnySession, token: value.token };
}

function sessionFromEnvelope<User>(value: unknown) {
	if (
		!isRecord(value) ||
		!isRecord(value.session) ||
		!("user" in value.session) ||
		typeof value.session.id !== "string" ||
		typeof value.session.collection !== "string" ||
		typeof value.session.expiresAt !== "string"
	) {
		throw invalidSuccessEnvelope("session");
	}
	return {
		id: value.session.id,
		collection: value.session.collection,
		user: value.session.user as User,
		expiresAt: value.session.expiresAt,
	};
}

function isAuthSessionInfo(value: unknown): value is AuthSessionInfo {
	return (
		isRecord(value) &&
		typeof value.id === "string" &&
		typeof value.createdAt === "string" &&
		typeof value.lastSeenAt === "string" &&
		typeof value.expiresAt === "string" &&
		(value.ipAddress === undefined || typeof value.ipAddress === "string") &&
		(value.userAgent === undefined || typeof value.userAgent === "string") &&
		typeof value.current === "boolean"
	);
}

function isAPIKey(value: unknown, includeSecret: true): value is APIKey;
function isAPIKey(value: unknown, includeSecret: false): value is APIKeyInfo;
function isAPIKey(value: unknown, includeSecret: boolean): value is APIKey | APIKeyInfo {
	return (
		isRecord(value) &&
		typeof value.id === "string" &&
		typeof value.name === "string" &&
		typeof value.createdAt === "string" &&
		(!includeSecret || typeof value.key === "string") &&
		(value.lastUsedAt === undefined || typeof value.lastUsedAt === "string") &&
		(value.expiresAt === undefined || typeof value.expiresAt === "string")
	);
}

function isScheduledPublication(value: unknown): value is ScheduledPublication {
	return (
		isRecord(value) &&
		typeof value.id === "string" &&
		(value.action === "publish" || value.action === "unpublish") &&
		typeof value.documentId === "string" &&
		typeof value.expectedRevision === "number" &&
		typeof value.runAt === "string" &&
		(value.timeZone === undefined || typeof value.timeZone === "string") &&
		typeof value.attempts === "number" &&
		(value.lastError === undefined || typeof value.lastError === "string") &&
		typeof value.createdAt === "string"
	);
}

function appendLocaleQuery(query: URLSearchParams, options: LocaleOptions | undefined) {
	if (options?.locale !== undefined) query.set("locale", options.locale);
	if (options?.fallbackLocale === false) {
		query.set("fallback-locale", "false");
	} else if (typeof options?.fallbackLocale === "string") {
		query.set("fallback-locale", options.fallbackLocale);
	} else if (options?.fallbackLocale !== undefined) {
		query.set("fallback-locale", options.fallbackLocale.join(","));
	}
}

function appendDraftQuery(query: URLSearchParams, options: CreateOptions | undefined) {
	if (options?.draft !== undefined) query.set("draft", String(options.draft));
}

function invalidSuccessEnvelope(kind: string) {
	return new RiduError({
		code: "internal",
		status: 500,
		message: `Server returned an invalid ${kind} response envelope`,
		issues: [],
	});
}

function liveValidationFromEnvelope(value: unknown): LiveValidationEnvelope {
	if (!isRecord(value) || !Array.isArray(value.evaluations))
		throw invalidSuccessEnvelope("live validation");
	return {
		evaluations: value.evaluations.map((entry: unknown) => {
			if (
				!isRecord(entry) ||
				typeof entry.path !== "string" ||
				(entry.target !== undefined && typeof entry.target !== "string") ||
				(entry.status !== "checked" && entry.status !== "skipped") ||
				!Array.isArray(entry.issues) ||
				!entry.issues.every(isValidationIssue) ||
				(entry.status === "skipped" && entry.issues.length !== 0)
			)
				throw invalidSuccessEnvelope("live validation");
			return {
				path: entry.path,
				status: entry.status,
				issues: entry.issues.map((issue) => ({ ...issue })),
				...(entry.target === undefined ? {} : { target: entry.target }),
			};
		}),
	};
}
