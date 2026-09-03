import {
	isPageEnvelope,
	type AuthSession,
	type AccessCapabilitiesEnvelope,
	type CollectionSelectionEnvelope,
	type FieldCapabilities,
	type OperationCapabilities,
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
	type ScheduledPublish,
	type DocumentLockEnvelope,
	type PreviewToken,
} from "@riducms/protocol";

import { RiduError } from "./error.js";
import type {
	ClientOptions,
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
	MutationOptions,
	CreateOptions,
	RestoreOptions,
	SelectFor,
	UpdateFor,
	WhereFor,
	RiduClient,
	RiduConfigShape,
	UploadCollectionSlug,
	UploadOptions,
	UploadImageInput,
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
	GlobalQueryResult,
	AllLocalesOutputFor,
	GlobalAllLocalesOutputFor,
	CollectionVersionsFor,
	CollectionDraftsFor,
	GlobalDraftsFor,
	LocaleResult,
	DefaultRiduConfig,
} from "./types.js";

export function createClient<Config extends RiduConfigShape = DefaultRiduConfig>(
	options: ClientOptions
): RiduClient<Config> {
	return new FetchClient<Config>(options);
}

class FetchClient<Config extends RiduConfigShape> implements RiduClient<Config> {
	readonly #baseURL: string;
	readonly #fetch: NonNullable<ClientOptions["fetch"]>;
	readonly #headers: ClientOptions["headers"];
	readonly #credentials: NonNullable<ClientOptions["credentials"]>;
	readonly #dispatch: MiddlewareNext;

	constructor(options: ClientOptions) {
		const baseURL = new URL(options.baseURL);
		this.#baseURL = baseURL.href.replace(/\/$/, "");
		this.#fetch = options.fetch ?? globalThis.fetch.bind(globalThis);
		this.#headers = options.headers;
		this.#credentials = options.credentials ?? "include";
		this.#dispatch = composeMiddleware(options.middleware ?? [], (request) => this.#fetch(request));
	}

	async schema(options?: RequestOptions): Promise<SchemaManifest> {
		const body = await this.#request("/api/schema", { method: "GET" }, options);
		if (!isRecord(body) || !isRecord(body.schema) || !Array.isArray(body.schema.collections)) {
			throw invalidSuccessEnvelope("schema");
		}
		return body.schema as unknown as SchemaManifest;
	}

	async request(path: string, init: RequestInit = {}, options?: RequestOptions): Promise<Response> {
		return this.#response(path, init, options, false);
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

	async login<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		credentials: LoginCredentials,
		options?: RequestOptions
	): Promise<AuthSession<OutputFor<Config, Slug>>> {
		const body = await this.#request(
			`/api/auth/${encodeURIComponent(collection)}/login`,
			{ method: "POST", body: JSON.stringify(credentials) },
			options
		);
		return sessionFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async session(options?: RequestOptions): Promise<AuthSession<AuthUser<Config>>> {
		const body = await this.#request("/api/auth/me", { method: "GET" }, options);
		return sessionFromEnvelope<AuthUser<Config>>(body);
	}

	async refreshSession(options?: RequestOptions): Promise<AuthSession<AuthUser<Config>>> {
		const body = await this.#request("/api/auth/refresh", { method: "POST" }, options);
		return sessionFromEnvelope<AuthUser<Config>>(body);
	}

	async logout(options?: RequestOptions): Promise<LogoutEnvelope> {
		const body = await this.#request("/api/auth/logout", { method: "POST" }, options);
		if (!isRecord(body) || body.loggedOut !== true) {
			throw invalidSuccessEnvelope("logout");
		}
		return { loggedOut: true };
	}

	async logoutAll(options?: RequestOptions): Promise<LogoutEnvelope> {
		const body = await this.#request("/api/auth/logout-all", { method: "POST" }, options);
		if (!isRecord(body) || body.loggedOut !== true) {
			throw invalidSuccessEnvelope("logout-all");
		}
		return { loggedOut: true };
	}

	async sessions(options?: RequestOptions): Promise<AuthSessionInfo[]> {
		const body = await this.#request("/api/auth/sessions", { method: "GET" }, options);
		if (
			!isRecord(body) ||
			!Array.isArray(body.sessions) ||
			!body.sessions.every(isAuthSessionInfo)
		) {
			throw invalidSuccessEnvelope("sessions");
		}
		return body.sessions;
	}

	async revokeSession(id: string, options?: RequestOptions): Promise<DeleteEnvelope> {
		const body = await this.#request(
			`/api/auth/sessions/${encodeURIComponent(id)}`,
			{ method: "DELETE" },
			options
		);
		if (!isRecord(body) || body.id !== id || body.deleted !== true) {
			throw invalidSuccessEnvelope("session revocation");
		}
		return { id, deleted: true };
	}

	async requestPasswordReset<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		email: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope> {
		return this.#authAction(collection, "forgot-password", { email }, options);
	}

	async resetPassword<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		token: string,
		password: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope> {
		return this.#authAction(collection, "reset-password", { token, password }, options);
	}

	async authBootstrap<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		options?: RequestOptions
	): Promise<AuthBootstrapEnvelope> {
		const body = await this.#request(
			`/api/auth/${encodeURIComponent(collection)}/bootstrap`,
			{ method: "GET" },
			options
		);
		if (!isRecord(body) || typeof body.available !== "boolean") {
			throw invalidSuccessEnvelope("auth bootstrap");
		}
		return { available: body.available };
	}

	async createAuthUser<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		data: CreateFor<Config, Slug>,
		password: string,
		options?: MutationLocaleOptions
	): Promise<OutputFor<Config, Slug>> {
		const query = new URLSearchParams();
		appendLocaleQuery(query, options);
		const suffix = query.size === 0 ? "" : `?${query}`;
		const body = await this.#request(
			`/api/auth/${encodeURIComponent(collection)}/create-user${suffix}`,
			{ method: "POST", body: JSON.stringify({ data, password }) },
			options
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async requestVerification<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		email: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope> {
		return this.#authAction(collection, "request-verification", { email }, options);
	}

	async verifyEmail<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		token: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope> {
		return this.#authAction(collection, "verify", { token }, options);
	}

	async changePassword(
		currentPassword: string,
		password: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope> {
		const body = await this.#request(
			"/api/auth/change-password",
			{ method: "POST", body: JSON.stringify({ currentPassword, password }) },
			options
		);
		if (!isRecord(body) || body.success !== true) {
			throw invalidSuccessEnvelope("password change");
		}
		return { success: true };
	}

	async createAPIKey(input: CreateAPIKeyInput, options?: RequestOptions): Promise<APIKey> {
		const body = await this.#request(
			"/api/auth/api-keys",
			{ method: "POST", body: JSON.stringify(input) },
			options
		);
		if (!isRecord(body) || !isAPIKey(body.apiKey, true)) {
			throw invalidSuccessEnvelope("API key");
		}
		return body.apiKey;
	}

	async apiKeys(options?: RequestOptions): Promise<APIKeyInfo[]> {
		const body = await this.#request("/api/auth/api-keys", { method: "GET" }, options);
		if (
			!isRecord(body) ||
			!Array.isArray(body.apiKeys) ||
			!body.apiKeys.every((key) => isAPIKey(key, false))
		) {
			throw invalidSuccessEnvelope("API keys");
		}
		return body.apiKeys;
	}

	async revokeAPIKey(id: string, options?: RequestOptions): Promise<DeleteEnvelope> {
		const body = await this.#request(
			`/api/auth/api-keys/${encodeURIComponent(id)}`,
			{ method: "DELETE" },
			options
		);
		if (!isRecord(body) || body.id !== id || body.deleted !== true) {
			throw invalidSuccessEnvelope("API key revocation");
		}
		return { id, deleted: true };
	}

	async forceUnlock<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope> {
		const body = await this.#request(
			`/api/auth/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/unlock`,
			{ method: "POST" },
			options
		);
		if (!isRecord(body) || body.success !== true) {
			throw invalidSuccessEnvelope("account unlock");
		}
		return { success: true };
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

	async #authAction<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		action: string,
		input: Record<string, string>,
		options?: RequestOptions
	): Promise<AuthActionEnvelope> {
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
	): Promise<PageEnvelope<CollectionQueryResult<Config, Slug, Options>>> {
		const query = new URLSearchParams();
		if (options?.page !== undefined) query.set("page", String(options.page));
		if (options?.limit !== undefined) query.set("limit", String(options.limit));
		if (options?.depth !== undefined) query.set("depth", String(options.depth));
		if (options?.where !== undefined) query.set("where", JSON.stringify(options.where));
		if (options?.select !== undefined) query.set("select", JSON.stringify(options.select));
		if (options?.populate !== undefined) query.set("populate", JSON.stringify(options.populate));
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
		return body;
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
		form.set("file", file);
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
			{ method: "POST", body: JSON.stringify({ url, data: options?.data ?? {} }) },
			options
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
	}

	async updateUploadImage<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		input: UploadImageInput,
		options?: RevisionOptions
	): Promise<OutputFor<Config, Slug>> {
		const revision = revisionHeaders(options);
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/image`,
			{
				method: "PATCH",
				body: JSON.stringify(input),
				...(revision === undefined ? {} : { headers: revision }),
			},
			options
		);
		return documentFromEnvelope<OutputFor<Config, Slug>>(body);
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
		options?: RevisionOptions
	): Promise<ScheduledPublish> {
		const headers = revisionHeaders(options);
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/schedule`,
			{
				method: "POST",
				body: JSON.stringify({ runAt: runAt instanceof Date ? runAt.toISOString() : runAt }),
				...(headers === undefined ? {} : { headers }),
			},
			options
		);
		if (!isRecord(body) || !isScheduledPublish(body.scheduledPublish)) {
			throw invalidSuccessEnvelope("scheduled publish");
		}
		return body.scheduledPublish;
	}

	async scheduledPublishes<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<ScheduledPublish[]> {
		const body = await this.#request(
			`/api/collections/${encodeURIComponent(collection)}/${encodeDocumentID(id)}/schedule`,
			{ method: "GET" },
			options
		);
		if (
			!isRecord(body) ||
			!Array.isArray(body.scheduledPublishes) ||
			!body.scheduledPublishes.every(isScheduledPublish)
		) {
			throw invalidSuccessEnvelope("scheduled publishes");
		}
		return body.scheduledPublishes;
	}

	async cancelScheduledPublish<Slug extends VersionCollectionSlug<Config>>(
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
			throw invalidSuccessEnvelope("scheduled publish cancellation");
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

	async #request(path: string, init: RequestInit, options?: RequestOptions): Promise<unknown> {
		const response = await this.#response(path, init, options, true);
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
		defaultJSONContentType: boolean
	): Promise<Response> {
		if (!path.startsWith("/") || path.startsWith("//") || path.includes("#")) {
			throw new TypeError("request path must be an absolute-path reference");
		}
		const target = new URL(path, this.#baseURL);
		if (target.origin !== new URL(this.#baseURL).origin || target.hash !== "") {
			throw new TypeError("request path must stay on the configured origin and omit fragments");
		}
		const headers = new Headers(await resolveHeaders(this.#headers));
		for (const [name, value] of new Headers(init.headers)) headers.set(name, value);
		for (const [name, value] of new Headers(options?.headers)) headers.set(name, value);
		if (
			defaultJSONContentType &&
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
		return this.#dispatch(request);
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
	if (!isRecord(value) || !isOperationCapabilities(value.operations) || !isRecord(value.fields)) {
		throw invalidSuccessEnvelope("access capabilities");
	}
	const fields: Record<string, FieldCapabilities> = {};
	for (const [path, capability] of Object.entries(value.fields)) {
		if (!isFieldCapabilities(capability)) throw invalidSuccessEnvelope("field capabilities");
		fields[path] = capability;
	}
	return { operations: value.operations, fields };
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

function isOperationCapabilities(value: unknown): value is OperationCapabilities {
	return (
		isRecord(value) &&
		[
			"admin",
			"create",
			"read",
			"readVersions",
			"update",
			"delete",
			"duplicate",
			"publish",
			"unpublish",
			"restoreDeleted",
			"deletePermanent",
			"selectAll",
		].every((key) => typeof value[key] === "boolean")
	);
}

function isFieldCapabilities(value: unknown): value is FieldCapabilities {
	return (
		isRecord(value) &&
		typeof value.read === "boolean" &&
		typeof value.create === "boolean" &&
		typeof value.update === "boolean"
	);
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

function isScheduledPublish(value: unknown): value is ScheduledPublish {
	return (
		isRecord(value) &&
		typeof value.id === "string" &&
		typeof value.documentId === "string" &&
		typeof value.expectedRevision === "number" &&
		typeof value.runAt === "string" &&
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

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
