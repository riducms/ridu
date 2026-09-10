import type {
	AuthSession,
	AuthSessionInfo,
	AuthActionEnvelope,
	AuthBootstrapEnvelope,
	APIKey,
	APIKeyInfo,
	AccessCapabilitiesEnvelope,
	LiveValidationRequest,
	LiveValidationEnvelope,
	CollectionSelectionEnvelope,
	DeleteEnvelope,
	CountEnvelope,
	LogoutEnvelope,
	PageEnvelope,
	SchemaManifest,
	ScheduledPublish,
	DocumentLockEnvelope,
	JoinMutationEnvelope,
	JoinMutationInput,
	PreviewToken,
} from "@riducms/protocol";

export type { JoinMutationInput, PreviewToken, ScheduledPublish } from "@riducms/protocol";

/**
 * Generated type and capability contract for one collection.
 *
 * Ridu's generator places these entries in `RiduConfigShape.collections`; application authors
 * consume the derived client types and normally do not construct this interface themselves.
 */
export interface CollectionContract {
	auth: boolean;
	upload: boolean;
	versions: boolean;
	drafts?: boolean;
	trash: boolean;
	output: unknown;
	allOutput?: unknown;
	create: unknown;
	update: unknown;
	where: unknown;
	select: unknown;
	populate: unknown;
	populateOutput?: unknown;
	allPopulateOutput?: unknown;
	validationPath?: string;
}

/**
 * Generated type and capability contract for one global.
 *
 * Ridu's generator places these entries in `RiduConfigShape.globals` so global methods can infer
 * their input, output, locale, selection, population, draft, and version contracts.
 */
export interface GlobalContract {
	versions: boolean;
	drafts?: boolean;
	output: unknown;
	allOutput?: unknown;
	update: unknown;
	select: unknown;
	populate: unknown;
	populateOutput?: unknown;
	allPopulateOutput?: unknown;
	validationPath?: string;
}

/** Application contract shape used to type every resource-aware SDK method. */
export interface RiduConfigShape {
	/** Generated union of configured content locales, when localization is enabled. */
	locale?: string;
	/** Generated collection contracts keyed by collection slug. */
	collections: object;
	/** Generated global contracts keyed by global slug. */
	globals?: object;
}

/**
 * Application generators add one manifest-digest-keyed entry through module augmentation. Keeping a
 * registry rather than extending one config directly lets explicit clients coexist in tooling and
 * multi-application TypeScript programs without declaration-merging conflicts.
 */
export interface GeneratedRiduConfigRegistry {}

type GeneratedRiduConfigKey = keyof GeneratedRiduConfigRegistry;

type IsUnion<Value, Whole = Value> = Value extends unknown
	? [Whole] extends [Value]
		? false
		: true
	: never;

type RegisteredRiduConfig = GeneratedRiduConfigRegistry[GeneratedRiduConfigKey];

/**
 * The one generated config visible to the current TypeScript program. With none or more than one,
 * callers use an explicit config (normally through a generated client package).
 */
export type DefaultRiduConfig = [GeneratedRiduConfigKey] extends [never]
	? RiduConfigShape
	: true extends IsUnion<GeneratedRiduConfigKey>
		? RiduConfigShape
		: RegisteredRiduConfig extends RiduConfigShape
			? RegisteredRiduConfig
			: RiduConfigShape;

/** Collection slugs available in a generated application contract. */
export type CollectionSlug<Config extends RiduConfigShape> = keyof Config["collections"] & string;

/** Global slugs available in a generated application contract. */
export type GlobalSlug<Config extends RiduConfigShape> = keyof NonNullable<Config["globals"]> &
	string;

/** Generated contract for one global slug. */
export type GlobalContractFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> = NonNullable<Config["globals"]>[Slug] extends GlobalContract
	? NonNullable<Config["globals"]>[Slug]
	: never;

/** Read shape for one generated global. */
export type GlobalOutputFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> = GlobalContractFor<Config, Slug>["output"];

/** All-locales read shape for one generated global. */
export type GlobalAllLocalesOutputFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> =
	GlobalContractFor<Config, Slug> extends { allOutput: infer Output }
		? Output
		: GlobalOutputFor<Config, Slug>;

/** Update input accepted by one generated global. */
export type GlobalUpdateFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> = GlobalContractFor<Config, Slug>["update"];

/** Top-level field selection accepted by one generated global. */
export type GlobalSelectFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> = GlobalContractFor<Config, Slug>["select"];

/** Relationship population options accepted by one generated global. */
export type GlobalPopulateFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> = GlobalContractFor<Config, Slug>["populate"];

/** Mapping used to infer populated relationship values for one generated global. */
export type GlobalPopulateOutputFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> =
	GlobalContractFor<Config, Slug> extends { populateOutput: infer Output }
		? Output
		: Record<never, never>;

/** All-locales relationship mapping for one generated global. */
export type GlobalAllLocalesPopulateOutputFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> =
	GlobalContractFor<Config, Slug> extends { allPopulateOutput: infer Output }
		? Output
		: GlobalPopulateOutputFor<Config, Slug>;

/** Canonical field paths returned by validation for one generated global. */
export type GlobalValidationPathFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> =
	GlobalContractFor<Config, Slug> extends { validationPath: infer Path extends string }
		? Path
		: string;

/** Global slugs that enable versions. */
export type VersionGlobalSlug<Config extends RiduConfigShape> = {
	[Slug in GlobalSlug<Config>]: GlobalContractFor<Config, Slug>["versions"] extends true
		? Slug
		: never;
}[GlobalSlug<Config>];

/** Global slugs that support draft state. */
export type DraftGlobalSlug<Config extends RiduConfigShape> = {
	[Slug in GlobalSlug<Config>]: GlobalContractFor<Config, Slug> extends { drafts: true }
		? Slug
		: GlobalContractFor<Config, Slug> extends { drafts: false }
			? never
			: GlobalContractFor<Config, Slug>["versions"] extends true
				? Slug
				: never;
}[GlobalSlug<Config>];

/** Generated contract for one collection slug. */
export type ContractFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = Config["collections"][Slug] extends CollectionContract ? Config["collections"][Slug] : never;

/** Read shape for one generated collection. */
export type OutputFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["output"];

/** All-locales read shape for one generated collection. */
export type AllLocalesOutputFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> =
	ContractFor<Config, Slug> extends { allOutput: infer Output } ? Output : OutputFor<Config, Slug>;

/** Create input accepted by one generated collection. */
export type CreateFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["create"];

/** Update input accepted by one generated collection. */
export type UpdateFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["update"];

/** Filter expression accepted by one generated collection. */
export type WhereFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["where"];

/** Top-level field selection accepted by one generated collection. */
export type SelectFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["select"];

/** Relationship population options accepted by one generated collection. */
export type PopulateFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["populate"];

/** Mapping used to infer populated relationship values for one generated collection. */
export type PopulateOutputFor<Config extends RiduConfigShape, Slug extends CollectionSlug<Config>> =
	ContractFor<Config, Slug> extends { populateOutput: infer Output }
		? Output
		: Record<never, never>;

/** All-locales relationship mapping for one generated collection. */
export type AllLocalesPopulateOutputFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> =
	ContractFor<Config, Slug> extends { allPopulateOutput: infer Output }
		? Output
		: PopulateOutputFor<Config, Slug>;

/** Canonical field paths returned by validation for one generated collection. */
export type ValidationPathFor<Config extends RiduConfigShape, Slug extends CollectionSlug<Config>> =
	ContractFor<Config, Slug> extends { validationPath: infer Path extends string } ? Path : string;

/** Content locale union generated for an application. */
export type LocaleFor<Config extends RiduConfigShape> = Config extends { locale: infer Locale }
	? Locale extends string
		? Locale
		: never
	: string;

/** Union of document shapes that can represent the current authenticated user. */
export type AuthUser<Config extends RiduConfigShape> = OutputFor<
	Config,
	AuthCollectionSlug<Config>
>;

/** Collection slugs that enable authentication. */
export type AuthCollectionSlug<Config extends RiduConfigShape> = {
	[Slug in CollectionSlug<Config>]: ContractFor<Config, Slug>["auth"] extends true ? Slug : never;
}[CollectionSlug<Config>];

/** Collection slugs that accept file uploads. */
export type UploadCollectionSlug<Config extends RiduConfigShape> = {
	[Slug in CollectionSlug<Config>]: ContractFor<Config, Slug>["upload"] extends true ? Slug : never;
}[CollectionSlug<Config>];

/** Collection slugs that enable versions. */
export type VersionCollectionSlug<Config extends RiduConfigShape> = {
	[Slug in CollectionSlug<Config>]: ContractFor<Config, Slug>["versions"] extends true
		? Slug
		: never;
}[CollectionSlug<Config>];

/** Collection slugs that support draft state. */
export type DraftCollectionSlug<Config extends RiduConfigShape> = {
	[Slug in CollectionSlug<Config>]: ContractFor<Config, Slug> extends { drafts: true }
		? Slug
		: ContractFor<Config, Slug> extends { drafts: false }
			? never
			: ContractFor<Config, Slug>["versions"] extends true
				? Slug
				: never;
}[CollectionSlug<Config>];

/** Whether one generated collection enables versions. */
export type CollectionVersionsFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["versions"];

/** Whether one generated collection supports drafts. */
export type CollectionDraftsFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> =
	ContractFor<Config, Slug> extends { drafts: infer Drafts extends boolean }
		? Drafts
		: CollectionVersionsFor<Config, Slug>;

/** Whether one generated global supports drafts. */
export type GlobalDraftsFor<Config extends RiduConfigShape, Slug extends GlobalSlug<Config>> =
	GlobalContractFor<Config, Slug> extends { drafts: infer Drafts extends boolean }
		? Drafts
		: GlobalContractFor<Config, Slug>["versions"];

/** Collection slugs that support soft deletion and trash operations. */
export type TrashCollectionSlug<Config extends RiduConfigShape> = {
	[Slug in CollectionSlug<Config>]: ContractFor<Config, Slug>["trash"] extends true ? Slug : never;
}[CollectionSlug<Config>];

/** Email and password submitted to an auth-enabled collection. */
export interface LoginCredentials {
	email: string;
	password: string;
}

/** Details used to create an API key for the current actor. */
export interface CreateAPIKeyInput {
	name: string;
	expiresAt?: string;
}

/** Exact-locale, cancellable live feedback; fallback and all-locales are not supported. */
export interface LiveValidationOptions<Locale extends string = string> extends RequestOptions {
	/** Exact content locale whose unsaved values should be validated. */
	locale?: Locale;
}

/** Per-call Fetch controls shared by SDK requests. */
export interface RequestOptions {
	/** Abort signal used to cancel the request. */
	signal?: AbortSignal;
	/** Headers merged after the client's default headers. */
	headers?: ConstructorParameters<typeof Headers>[0];
	/** Whether the request may outlive the page that initiated it. */
	keepalive?: boolean;
}

/** Locale, fallback, and Fetch controls for content reads. */
export interface LocaleOptions<Locale extends string = string> extends RequestOptions {
	/** Content locale. Use "all" to receive locale-keyed localized fields. */
	locale?: [Locale] extends [never] ? never : Locale | "all";
	/** Explicit fallback chain, or false to require an exact locale value. */
	fallbackLocale?: [Locale] extends [never] ? never : Locale | readonly Locale[] | false;
}

interface SingleLocaleMutationOptions<Locale extends string = string> extends Omit<
	LocaleOptions<Locale>,
	"locale"
> {
	/** Mutations target one locale; `"all"` is read-only. */
	locale?: Locale;
}

/** Locale-aware mutation options for methods that do not consume a revision. */
export interface MutationLocaleOptions<
	Locale extends string = string,
> extends SingleLocaleMutationOptions<Locale> {
	/** This method has no optimistic-concurrency transport contract. */
	revision?: never;
}

/** Locale-aware mutation options for methods that send an optimistic revision. */
export interface MutationOptions<
	Locale extends string = string,
> extends SingleLocaleMutationOptions<Locale> {
	/** Last observed document revision used for optimistic concurrency. */
	revision?: number;
}

/** Optimistic-concurrency options for mutations that have no locale semantics. */
export interface RevisionOptions extends RequestOptions {
	/** Last observed document revision used for optimistic concurrency. */
	revision?: number;
	/** This mutation does not select or return a content locale. */
	locale?: never;
	/** This mutation does not resolve localized fallback values. */
	fallbackLocale?: never;
}

type DraftCreateOption<Versions extends boolean, Drafts extends boolean> = [Versions] extends [
	false,
]
	? { draft?: never }
	: [Drafts] extends [false]
		? { draft?: false }
		: { draft?: boolean };

/** Locale and draft controls accepted when creating a collection document. */
export type CreateOptions<
	Locale extends string = string,
	Versions extends boolean = boolean,
	Drafts extends boolean = boolean,
> = MutationLocaleOptions<Locale> & DraftCreateOption<Versions, Drafts>;

/** Locale, revision, and resulting draft-state controls accepted when restoring a version. */
export type RestoreOptions<
	Locale extends string = string,
	Drafts extends boolean = boolean,
> = MutationOptions<Locale> & ([Drafts] extends [false] ? { draft?: false } : { draft?: boolean });

/** Source and destination locales for copying a localized document or global. */
export interface CopyLocaleInput<Locale extends string = string> {
	/** Locale whose current values are copied. */
	from: Locale;
	/** Locale that receives the copied values. */
	to: Locale;
}

/** Optional metadata and request controls supplied with a file upload. */
export interface UploadOptions<
	Data,
	Locale extends string = string,
> extends MutationLocaleOptions<Locale> {
	/** Generated document fields to save alongside the uploaded file. */
	data?: Partial<Data>;
}

/** Focal point and optional crop rectangle used to reprocess an uploaded image. */
export interface UploadImageInput {
	/** Horizontal focal point as a percentage from 0 through 100. */
	focalX: number;
	/** Vertical focal point as a percentage from 0 through 100. */
	focalY: number;
	/** Crop rectangle's horizontal origin as a percentage, when cropping. */
	cropX?: number;
	/** Crop rectangle's vertical origin as a percentage, when cropping. */
	cropY?: number;
	/** Crop rectangle width as a percentage, when cropping. */
	cropWidth?: number;
	/** Crop rectangle height as a percentage, when cropping. */
	cropHeight?: number;
}

/** Proposed document context used to resolve collection operation capabilities. */
export interface CollectionAccessOptions<
	Data,
	Locale extends string = string,
> extends LocaleOptions<Locale> {
	/** Existing document ID when resolving update, delete, or document-bound capabilities. */
	id?: string;
	/** Proposed values used when field and operation access depends on submitted data. */
	data?: Partial<Data>;
	/** Resolve capabilities in the collection's trash view. */
	trash?: boolean;
}

/** Filter and locale context used to resolve selectable documents. */
export interface CollectionSelectionOptions<
	Where,
	Locale extends string = string,
> extends LocaleOptions<Locale> {
	/** Typed filter applied before access rules and the bounded selection limit. */
	where?: Where;
	/** Select from soft-deleted documents instead of active documents. */
	trash?: boolean;
}

/** Proposed global data and locale context used to resolve operation capabilities. */
export interface GlobalAccessOptions<
	Data,
	Locale extends string = string,
> extends LocaleOptions<Locale> {
	/** Proposed values used when field and operation access depends on submitted data. */
	data?: Partial<Data>;
}

type DocumentStatus<Document> = Document extends { _status: infer Status }
	? Extract<Status, "draft" | "published">
	: "draft" | "published";

/** Stored snapshot and metadata for one document or global revision. */
export interface Version<Document> {
	ID: string;
	DocumentID: string;
	Revision: number;
	Status: DocumentStatus<Document>;
	Snapshot: Document;
	CreatedAt: string;
}

/** Pagination, filtering, projection, population, locale, and trash controls for a collection list. */
export interface ListOptions<
	Where,
	Select,
	Populate,
	Locale extends string = string,
> extends LocaleOptions<Locale> {
	/** One-based result page; defaults to the first page. */
	page?: number;
	/** Maximum documents returned in this page. */
	limit?: number;
	/** Maximum relationship depth available to explicit population. */
	depth?: number;
	/** Typed document filter combined atomically with collection access rules. */
	where?: Where;
	/** Top-level fields to return in addition to always-present metadata. */
	select?: Select;
	/** Relationship paths to replace with access-checked document data. */
	populate?: Populate;
	/** Ordered field names; prefix a field with `-` for descending order. */
	sort?: readonly string[];
	/** Read soft-deleted documents instead of active documents. */
	trash?: boolean;
}

/** Projection, population, and locale controls for reading one document or global. */
export interface FindOptions<
	Select,
	Populate,
	Locale extends string = string,
> extends LocaleOptions<Locale> {
	/** Maximum relationship depth available to explicit population. */
	depth?: number;
	/** Top-level fields to return in addition to always-present metadata. */
	select?: Select;
	/** Relationship paths to replace with access-checked document data. */
	populate?: Populate;
}

type DocumentMetadataKey =
	"id" | "createdAt" | "updatedAt" | "deletedAt" | "_status" | "_revision" | "_localization";

type SelectedKey<Selection> = Selection extends object
	? {
			[Key in keyof Selection]-?: Selection[Key] extends true ? Key : never;
		}[keyof Selection]
	: never;

type HasWidenedSelection<Selection> = Selection extends object
	? boolean extends Exclude<Selection[keyof Selection], undefined>
		? true
		: false
	: false;

type HasOnlyBooleanSelectionValues<Selection> = Selection extends object
	? Exclude<Selection[keyof Selection], undefined> extends boolean
		? true
		: false
	: false;

/** Applies the REST top-level boolean projection while retaining always-returned metadata. */
export type ApplySelect<Document, Selection> = [Selection] extends [undefined]
	? Document
	: HasWidenedSelection<Selection> extends true
		? Document
		: Document extends object
			? Pick<Document, Extract<DocumentMetadataKey | SelectedKey<Selection>, keyof Document>>
			: Document;

type PopulationSelection<Option> = Option extends boolean
	? undefined
	: HasOnlyBooleanSelectionValues<Option> extends true
		? Option
		: "select" extends keyof Option
			? Option extends { select?: infer Selection }
				? Selection
				: undefined
			: undefined;

type NarrowPopulatedValue<Value, Option> = Value extends readonly (infer Item)[]
	? Array<NarrowPopulatedValue<Item, Option>>
	: Value extends { relationTo: infer Relation; id: infer IDValue }
		? Omit<Value, "relationTo" | "id"> & {
				relationTo: Relation;
				id: NarrowPopulatedValue<IDValue, Option>;
			}
		: Value extends { id: unknown }
			? ApplySelect<Value, PopulationSelection<Option>>
			: Value extends object
				? { [Key in keyof Value]: NarrowPopulatedValue<Value[Key], Option> }
				: Value;

type DataPath<Value, Prefix extends string, Key extends string> = Value extends {
	blockType: infer Block extends string;
}
	? Key extends "blockType"
		? `${Prefix}.blockType`
		: `${Prefix}.${Block}.${Key}`
	: Prefix extends ""
		? Key
		: `${Prefix}.${Key}`;

type IsDefinitePopulationOption<Option> = undefined extends Option
	? false
	: [Option] extends [object]
		? true
		: false extends Option
			? false
			: true;

type HasDefinitePopulation<Population, Path extends PropertyKey> = Path extends keyof Population
	? IsDefinitePopulationOption<Population[Path]>
	: false;

type IsLocaleMap<Value, Locale extends string> = [Locale] extends [never]
	? false
	: Value extends object
		? typeof Symbol.toStringTag extends keyof Value
			? true
			: false
		: false;

type ApplyPopulationTree<
	Value,
	Mapping,
	Population,
	Locale extends string,
	Prefix extends string = "",
> = Value extends null | undefined
	? Value
	: Value extends readonly (infer Item)[]
		? Array<ApplyPopulationTree<Item, Mapping, Population, Locale, Prefix>>
		: Value extends object
			? IsLocaleMap<Value, Locale> extends true
				? {
						[Key in keyof Value]: ApplyPopulationTree<
							Value[Key],
							Mapping,
							Population,
							Locale,
							Prefix
						>;
					}
				: {
						[Key in keyof Value]: Key extends string
							? DataPath<Value, Prefix, Key> extends infer Path extends string
								? Path extends keyof Mapping
									? HasDefinitePopulation<Population, Path> extends true
										? NarrowPopulatedValue<Mapping[Path], Population[Path & keyof Population]>
										: ApplyPopulationTree<Value[Key], Mapping, Population, Locale, Path>
									: ApplyPopulationTree<Value[Key], Mapping, Population, Locale, Path>
								: Value[Key]
							: Value[Key];
					}
			: Value;

/** Applies every explicit canonical populate path, including array and block traversal. */
export type ApplyPopulate<
	Document,
	Mapping,
	Population,
	Locale extends string = never,
> = Population extends object
	? [keyof Mapping & string] extends [never]
		? Document
		: ApplyPopulationTree<Document, Mapping, Population, Locale>
	: Document;

type OptionProperty<Options, Key extends PropertyKey> = Options extends object
	? Key extends keyof Options
		? Options[Key]
		: undefined
	: undefined;

/** Selects the single-locale or all-locales result shape from a literal options type. */
export type LocaleResult<Single, All, Options> = Options extends object
	? "locale" extends keyof Options
		? "all" extends OptionProperty<Options, "locale">
			? OptionProperty<Options, "locale"> extends "all"
				? All
				: Single | All
			: Single
		: Single
	: Single;

/** Result shape inferred from a collection slug and its literal locale, select, and populate options. */
export type CollectionQueryResult<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
	Options,
> = LocaleResult<
	ApplySelect<
		ApplyPopulate<
			OutputFor<Config, Slug>,
			PopulateOutputFor<Config, Slug>,
			OptionProperty<Options, "populate">
		>,
		OptionProperty<Options, "select">
	>,
	ApplySelect<
		ApplyPopulate<
			AllLocalesOutputFor<Config, Slug>,
			AllLocalesPopulateOutputFor<Config, Slug>,
			OptionProperty<Options, "populate">,
			LocaleFor<Config>
		>,
		OptionProperty<Options, "select">
	>,
	Options
>;

/** Result shape inferred from a global slug and its literal locale, select, and populate options. */
export type GlobalQueryResult<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
	Options,
> = LocaleResult<
	ApplySelect<
		ApplyPopulate<
			GlobalOutputFor<Config, Slug>,
			GlobalPopulateOutputFor<Config, Slug>,
			OptionProperty<Options, "populate">
		>,
		OptionProperty<Options, "select">
	>,
	ApplySelect<
		ApplyPopulate<
			GlobalAllLocalesOutputFor<Config, Slug>,
			GlobalAllLocalesPopulateOutputFor<Config, Slug>,
			OptionProperty<Options, "populate">,
			LocaleFor<Config>
		>,
		OptionProperty<Options, "select">
	>,
	Options
>;

/** Transport configuration shared by every request from a Ridu client. */
export interface ClientOptions {
	/** Absolute Ridu server origin; any trailing slash is normalized away. */
	baseURL: string;
	/** Fetch implementation, usually supplied by server frameworks or tests. */
	fetch?: (
		input: Parameters<typeof globalThis.fetch>[0],
		init?: Parameters<typeof globalThis.fetch>[1]
	) => ReturnType<typeof globalThis.fetch>;
	/** Static or lazily resolved headers applied before per-call headers. */
	headers?:
		| ConstructorParameters<typeof Headers>[0]
		| (() =>
				| ConstructorParameters<typeof Headers>[0]
				| Promise<ConstructorParameters<typeof Headers>[0]>);
	/** Fetch credentials mode; defaults to `"include"` for browser cookie sessions. */
	credentials?: RequestInit["credentials"];
	/** Ordered request middleware wrapped around the configured Fetch implementation. */
	middleware?: readonly Middleware[];
}

/** Dispatches a concrete request to the next middleware or Fetch implementation. */
export type MiddlewareNext = (request: Request) => Promise<Response>;

/** Intercepts an SDK request for concerns such as tracing, retries, or test transport. */
export type Middleware = (request: Request, next: MiddlewareNext) => Promise<Response>;

/**
 * Typed client operations for one generated Ridu application contract.
 *
 * Resource slugs and capability-specific methods narrow from `Config`; query results also follow
 * literal locale, selection, and population options. Methods return ordinary promises and reject
 * server failures with `RiduError`, except `request`, which preserves raw Fetch semantics.
 */
export interface RiduClient<Config extends RiduConfigShape = DefaultRiduConfig> {
	/** Fetch and bind the schema manifest visible to the current client. */
	schema(options?: RequestOptions): Promise<SchemaManifest>;
	/**
	 * Send a same-origin request to a custom application endpoint with raw Fetch semantics.
	 *
	 * This method returns every HTTP status unchanged and does not parse the body or add a JSON
	 * content type. The path must start with one `/`, stay on `baseURL`'s origin, and omit fragments.
	 *
	 * @example
	 * ```ts
	 * const response = await ridu.request("/api/revalidate/storefront", {
	 *   method: "POST",
	 *   body: JSON.stringify({ paths: ["/products"] }),
	 *   headers: { "content-type": "application/json" }
	 * });
	 * if (!response.ok) throw new Error(`Revalidation failed: ${response.status}`);
	 * ```
	 */
	request(path: string, init?: RequestInit, options?: RequestOptions): Promise<Response>;
	/**
	 * Post JSON to an endpoint declared by a paired backend plugin.
	 *
	 * The plugin owns the request and result contract, so supply its documented result type. This
	 * method applies the client's transport settings and throws `RiduError` for server failures.
	 *
	 * @example
	 * ```ts
	 * const generated = await ridu.requestPlugin<{ result: string }>(
	 *   "seo",
	 *   "generate-title",
	 *   { document: { title: "Draft" } }
	 * );
	 * ```
	 */
	requestPlugin<Result = unknown>(
		plugin: string,
		path: string,
		body: unknown,
		options?: RequestOptions
	): Promise<Result>;

	/** Start a session by logging in through an auth-enabled collection. */
	login<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		credentials: LoginCredentials,
		options?: RequestOptions
	): Promise<AuthSession<OutputFor<Config, Slug>>>;

	/** Read the current actor and session metadata. */
	session(options?: RequestOptions): Promise<AuthSession<AuthUser<Config>>>;
	/** Refresh the current session and return its updated actor and metadata. */
	refreshSession(options?: RequestOptions): Promise<AuthSession<AuthUser<Config>>>;

	/** End the current session. */
	logout(options?: RequestOptions): Promise<LogoutEnvelope>;

	/** End every session belonging to the current actor. */
	logoutAll(options?: RequestOptions): Promise<LogoutEnvelope>;

	/** List active sessions belonging to the current actor. */
	sessions(options?: RequestOptions): Promise<AuthSessionInfo[]>;

	/** Revoke one session belonging to the current actor. */
	revokeSession(id: string, options?: RequestOptions): Promise<DeleteEnvelope>;

	/** Request a password-reset message for an auth-enabled collection. */
	requestPasswordReset<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		email: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope>;

	/** Replace a password using a valid password-reset token. */
	resetPassword<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		token: string,
		password: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope>;

	/** Check whether an auth collection still permits its first bootstrap user. */
	authBootstrap<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		options?: RequestOptions
	): Promise<AuthBootstrapEnvelope>;

	/** Create a user in an auth-enabled collection, including bootstrap and managed-user flows. */
	createAuthUser<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		data: CreateFor<Config, Slug>,
		password: string,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	/** Request an email-verification message for an auth-enabled collection. */
	requestVerification<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		email: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope>;

	/** Verify an email address with a valid verification token. */
	verifyEmail<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		token: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope>;

	/** Change the current actor's password after confirming the existing password. */
	changePassword(
		currentPassword: string,
		password: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope>;

	/** Create an API key and return its secret, which later listings omit. */
	createAPIKey(input: CreateAPIKeyInput, options?: RequestOptions): Promise<APIKey>;

	/** List API-key metadata for the current actor without returning key secrets. */
	apiKeys(options?: RequestOptions): Promise<APIKeyInfo[]>;

	/** Revoke one API key belonging to the current actor. */
	revokeAPIKey(id: string, options?: RequestOptions): Promise<DeleteEnvelope>;

	/** Clear an auth user's login-attempt lock when the current actor is allowed to do so. */
	forceUnlock<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope>;

	/** Read one actor-scoped preference value. */
	preference<Value = unknown>(key: string, options?: RequestOptions): Promise<Value>;

	/** Create or replace one actor-scoped preference value. */
	setPreference<Value>(key: string, value: Value, options?: RequestOptions): Promise<Value>;

	/** Delete one actor-scoped preference value. */
	deletePreference(key: string, options?: RequestOptions): Promise<DeleteEnvelope>;

	/** Delete every preference stored for the current actor. */
	resetPreferences(options?: RequestOptions): Promise<AuthActionEnvelope>;

	/** Evaluate only explicitly opted-in advisory validators, independently of Save. */
	collectionLiveValidation<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		input: LiveValidationRequest,
		options?: LiveValidationOptions<LocaleFor<Config>>
	): Promise<LiveValidationEnvelope>;

	/** Evaluate advisory validators for one global's unsaved input. */
	globalLiveValidation<Slug extends GlobalSlug<Config>>(
		slug: Slug,
		input: Omit<LiveValidationRequest, "id"> & { id?: never },
		options?: LiveValidationOptions<LocaleFor<Config>>
	): Promise<LiveValidationEnvelope>;

	/** Resolve operation and field capabilities for a proposed collection-document context. */
	collectionAccess<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		options?: CollectionAccessOptions<
			CreateFor<Config, Slug> & UpdateFor<Config, Slug>,
			LocaleFor<Config>
		>
	): Promise<AccessCapabilitiesEnvelope>;

	/** Resolve an access-checked collection filter into an explicit bounded ID selection. */
	resolveFilteredSelection<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		options?: CollectionSelectionOptions<WhereFor<Config, Slug>, LocaleFor<Config>>
	): Promise<CollectionSelectionEnvelope>;

	/** Read the current collaborative editing lock for a collection document. */
	documentLock<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<DocumentLockEnvelope>;

	/** Acquire or refresh a collaborative editing lock, optionally requesting an allowed takeover. */
	acquireDocumentLock<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		takeover?: boolean,
		options?: RequestOptions
	): Promise<DocumentLockEnvelope>;

	/** Release the current actor's collaborative editing lock. */
	releaseDocumentLock<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<DeleteEnvelope>;

	/** Resolve operation and field capabilities for a proposed global context. */
	globalAccess<Slug extends GlobalSlug<Config>>(
		slug: Slug,
		options?: GlobalAccessOptions<GlobalUpdateFor<Config, Slug>, LocaleFor<Config>>
	): Promise<AccessCapabilitiesEnvelope>;

	/** Mint a short-lived token for reading one draft-capable collection document in preview. */
	createPreviewToken<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<PreviewToken>;

	/** Revoke a previously minted preview token. */
	revokePreviewToken(token: string, options?: RequestOptions): Promise<AuthActionEnvelope>;

	/** Read one collection document using its short-lived preview token. */
	preview<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		token: string,
		options?: RequestOptions
	): Promise<OutputFor<Config, Slug>>;

	/** Mint a short-lived token for reading one draft-capable global in preview. */
	createGlobalPreviewToken<Slug extends DraftGlobalSlug<Config>>(
		slug: Slug,
		options?: RequestOptions
	): Promise<PreviewToken>;

	/** Read one global using its short-lived preview token. */
	previewGlobal<Slug extends DraftGlobalSlug<Config>>(
		slug: Slug,
		token: string,
		options?: RequestOptions
	): Promise<GlobalOutputFor<Config, Slug>>;

	/** List a page of collection documents with typed filters, projection, population, and locale. */
	list<
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
	): Promise<PageEnvelope<CollectionQueryResult<Config, Slug, Options>>>;

	/** Count collection documents matching an access-checked filter. */
	count<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		options?: Pick<
			ListOptions<WhereFor<Config, Slug>, never, never, LocaleFor<Config>>,
			"where" | "trash" | "locale" | "fallbackLocale" | "signal" | "headers"
		>
	): Promise<CountEnvelope>;

	/** Read one collection document by ID with typed projection, population, and locale. */
	find<
		Slug extends CollectionSlug<Config>,
		const Options extends
			| FindOptions<SelectFor<Config, Slug>, PopulateFor<Config, Slug>, LocaleFor<Config>>
			| undefined = undefined,
	>(
		collection: Slug,
		id: string,
		options?: Options
	): Promise<CollectionQueryResult<Config, Slug, Options>>;

	/** Create a collection document from its generated input contract. */
	create<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		data: CreateFor<Config, Slug>,
		options?: CreateOptions<
			LocaleFor<Config>,
			CollectionVersionsFor<Config, Slug>,
			CollectionDraftsFor<Config, Slug>
		>
	): Promise<OutputFor<Config, Slug>>;

	/** Duplicate one collection document and optionally override createable fields. */
	duplicate<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		overrides?: Partial<Omit<CreateFor<Config, Slug>, "id">>,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	/** Copy localized values between two locales of one collection document. */
	copyLocale<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		input: CopyLocaleInput<LocaleFor<Config>>,
		options?: RevisionOptions
	): Promise<OutputFor<Config, Slug>>;

	/**
	 * Update one collection document, optionally requiring its last observed revision.
	 *
	 * Pass `_revision` to prevent overwriting a concurrent edit. Server validation and conflict
	 * responses reject with `RiduError`, whose stable code and field issues are safe to inspect.
	 *
	 * @example
	 * ```ts
	 * import { RiduError } from "@riducms/sdk";
	 *
	 * const post = await ridu.create("posts", { title: "SDK guide" });
	 * try {
	 *   await ridu.update(
	 *     "posts",
	 *     post.id,
	 *     { title: "Published guide" },
	 *     { revision: post._revision }
	 *   );
	 * } catch (error) {
	 *   if (error instanceof RiduError && error.code === "validation") {
	 *     console.error(error.issues);
	 *   } else {
	 *     throw error;
	 *   }
	 * }
	 * ```
	 */
	update<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		data: UpdateFor<Config, Slug>,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	/** Add and remove target IDs through one writable inverse join field. */
	mutateJoin<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		field: string,
		input: JoinMutationInput,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<JoinMutationEnvelope<OutputFor<Config, Slug>>>;

	/** Delete one collection document according to the collection's trash policy. */
	delete<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<DeleteEnvelope>;

	/** Apply one generated update to an explicit set of collection-document IDs. */
	bulkUpdate<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		data: UpdateFor<Config, Slug>,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	/** Publish an explicit set of versioned collection documents. */
	bulkPublish<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	/** Move an explicit set of draft-capable collection documents out of published state. */
	bulkUnpublish<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	/** Delete an explicit set of collection documents according to the collection's trash policy. */
	bulkDelete<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	/** Restore an explicit set of soft-deleted collection documents from trash. */
	bulkRestoreDeleted<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	/** Permanently delete an explicit set of trashed collection documents. */
	bulkDeletePermanent<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	/** Permanently delete every trashed document in one collection. */
	emptyTrash<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	/** Restore one soft-deleted collection document from trash. */
	restoreDeleted<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	/** Permanently delete one trashed collection document. */
	deletePermanent<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<DeleteEnvelope>;

	/** Upload a browser `Blob` and optional document metadata to an upload collection. */
	upload<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		file: Blob,
		options?: UploadOptions<CreateFor<Config, Slug>, LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	/** Ask the Ridu server to fetch a remote file into an upload collection. */
	uploadFromURL<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		url: string,
		options?: UploadOptions<CreateFor<Config, Slug>, LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	/** Update an uploaded image's focal point or crop and reprocess its derived sizes. */
	updateUploadImage<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		input: UploadImageInput,
		options?: RevisionOptions
	): Promise<OutputFor<Config, Slug>>;

	/** List stored versions for one collection document. */
	versions<
		Slug extends VersionCollectionSlug<Config>,
		const Options extends LocaleOptions<LocaleFor<Config>> | undefined = undefined,
	>(
		collection: Slug,
		id: string,
		options?: Options
	): Promise<
		Version<LocaleResult<OutputFor<Config, Slug>, AllLocalesOutputFor<Config, Slug>, Options>>[]
	>;

	/** Read one stored collection-document version by revision number. */
	version<
		Slug extends VersionCollectionSlug<Config>,
		const Options extends LocaleOptions<LocaleFor<Config>> | undefined = undefined,
	>(
		collection: Slug,
		id: string,
		revision: number,
		options?: Options
	): Promise<
		Version<LocaleResult<OutputFor<Config, Slug>, AllLocalesOutputFor<Config, Slug>, Options>>
	>;

	/** Schedule a versioned collection document to publish at a future instant. */
	schedulePublish<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		runAt: string | Date,
		options?: RevisionOptions
	): Promise<ScheduledPublish>;

	/** List pending publish jobs for one collection document. */
	scheduledPublishes<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<ScheduledPublish[]>;

	/** Cancel one pending publish job for a collection document. */
	cancelScheduledPublish<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		jobID: string,
		options?: RequestOptions
	): Promise<DeleteEnvelope>;

	/** Publish the current version of one collection document. */
	publish<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	/** Apply generated update data and publish the resulting collection document atomically. */
	publishChanges<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		data: UpdateFor<Config, Slug>,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	/** Move one draft-capable collection document out of published state. */
	unpublish<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	/** Restore one stored collection-document revision, optionally as a draft. */
	restore<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		revision: number,
		options?: RestoreOptions<LocaleFor<Config>, CollectionDraftsFor<Config, Slug>>
	): Promise<OutputFor<Config, Slug>>;

	/** Read one global with typed projection, population, and locale. */
	global<
		Slug extends GlobalSlug<Config>,
		const Options extends
			| FindOptions<
					GlobalSelectFor<Config, Slug>,
					GlobalPopulateFor<Config, Slug>,
					LocaleFor<Config>
			  >
			| undefined = undefined,
	>(
		slug: Slug,
		options?: Options
	): Promise<GlobalQueryResult<Config, Slug, Options>>;

	/** Update one global, optionally requiring its last observed revision. */
	updateGlobal<Slug extends GlobalSlug<Config>>(
		slug: Slug,
		data: GlobalUpdateFor<Config, Slug>,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<GlobalOutputFor<Config, Slug>>;

	/** Copy localized values between two locales of one global. */
	copyGlobalLocale<Slug extends GlobalSlug<Config>>(
		slug: Slug,
		input: CopyLocaleInput<LocaleFor<Config>>,
		options?: RevisionOptions
	): Promise<GlobalOutputFor<Config, Slug>>;

	/** List stored versions for one global. */
	globalVersions<
		Slug extends VersionGlobalSlug<Config>,
		const Options extends LocaleOptions<LocaleFor<Config>> | undefined = undefined,
	>(
		slug: Slug,
		options?: Options
	): Promise<
		Version<
			LocaleResult<GlobalOutputFor<Config, Slug>, GlobalAllLocalesOutputFor<Config, Slug>, Options>
		>[]
	>;

	/** Read one stored global version by revision number. */
	globalVersion<
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
	>;

	/** Publish the current version of one global. */
	publishGlobal<Slug extends VersionGlobalSlug<Config>>(
		slug: Slug,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<GlobalOutputFor<Config, Slug>>;

	/** Apply generated update data and publish the resulting global atomically. */
	publishGlobalChanges<Slug extends VersionGlobalSlug<Config>>(
		slug: Slug,
		data: GlobalUpdateFor<Config, Slug>,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<GlobalOutputFor<Config, Slug>>;

	/** Move one draft-capable global out of published state. */
	unpublishGlobal<Slug extends DraftGlobalSlug<Config>>(
		slug: Slug,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<GlobalOutputFor<Config, Slug>>;

	/** Restore one stored global revision, optionally as a draft. */
	restoreGlobal<Slug extends VersionGlobalSlug<Config>>(
		slug: Slug,
		revision: number,
		options?: RestoreOptions<LocaleFor<Config>, GlobalDraftsFor<Config, Slug>>
	): Promise<GlobalOutputFor<Config, Slug>>;
}
