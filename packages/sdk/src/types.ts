import type {
	AuthSession,
	AuthSessionInfo,
	AuthActionEnvelope,
	AuthBootstrapEnvelope,
	APIKey,
	APIKeyInfo,
	AccessCapabilitiesEnvelope,
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

export interface RiduConfigShape {
	locale?: string;
	collections: object;
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

export type CollectionSlug<Config extends RiduConfigShape> = keyof Config["collections"] & string;

export type GlobalSlug<Config extends RiduConfigShape> = keyof NonNullable<Config["globals"]> &
	string;

export type GlobalContractFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> = NonNullable<Config["globals"]>[Slug] extends GlobalContract
	? NonNullable<Config["globals"]>[Slug]
	: never;

export type GlobalOutputFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> = GlobalContractFor<Config, Slug>["output"];

export type GlobalAllLocalesOutputFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> =
	GlobalContractFor<Config, Slug> extends { allOutput: infer Output }
		? Output
		: GlobalOutputFor<Config, Slug>;

export type GlobalUpdateFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> = GlobalContractFor<Config, Slug>["update"];

export type GlobalSelectFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> = GlobalContractFor<Config, Slug>["select"];

export type GlobalPopulateFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> = GlobalContractFor<Config, Slug>["populate"];

export type GlobalPopulateOutputFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> =
	GlobalContractFor<Config, Slug> extends { populateOutput: infer Output }
		? Output
		: Record<never, never>;

export type GlobalAllLocalesPopulateOutputFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> =
	GlobalContractFor<Config, Slug> extends { allPopulateOutput: infer Output }
		? Output
		: GlobalPopulateOutputFor<Config, Slug>;

export type GlobalValidationPathFor<
	Config extends RiduConfigShape,
	Slug extends GlobalSlug<Config>,
> =
	GlobalContractFor<Config, Slug> extends { validationPath: infer Path extends string }
		? Path
		: string;

export type VersionGlobalSlug<Config extends RiduConfigShape> = {
	[Slug in GlobalSlug<Config>]: GlobalContractFor<Config, Slug>["versions"] extends true
		? Slug
		: never;
}[GlobalSlug<Config>];

export type DraftGlobalSlug<Config extends RiduConfigShape> = {
	[Slug in GlobalSlug<Config>]: GlobalContractFor<Config, Slug> extends { drafts: true }
		? Slug
		: GlobalContractFor<Config, Slug> extends { drafts: false }
			? never
			: GlobalContractFor<Config, Slug>["versions"] extends true
				? Slug
				: never;
}[GlobalSlug<Config>];

export type ContractFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = Config["collections"][Slug] extends CollectionContract ? Config["collections"][Slug] : never;

export type OutputFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["output"];

export type AllLocalesOutputFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> =
	ContractFor<Config, Slug> extends { allOutput: infer Output } ? Output : OutputFor<Config, Slug>;

export type CreateFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["create"];

export type UpdateFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["update"];

export type WhereFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["where"];

export type SelectFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["select"];

export type PopulateFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["populate"];

export type PopulateOutputFor<Config extends RiduConfigShape, Slug extends CollectionSlug<Config>> =
	ContractFor<Config, Slug> extends { populateOutput: infer Output }
		? Output
		: Record<never, never>;

export type AllLocalesPopulateOutputFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> =
	ContractFor<Config, Slug> extends { allPopulateOutput: infer Output }
		? Output
		: PopulateOutputFor<Config, Slug>;

export type ValidationPathFor<Config extends RiduConfigShape, Slug extends CollectionSlug<Config>> =
	ContractFor<Config, Slug> extends { validationPath: infer Path extends string } ? Path : string;

export type LocaleFor<Config extends RiduConfigShape> = Config extends { locale: infer Locale }
	? Locale extends string
		? Locale
		: never
	: string;

export type AuthUser<Config extends RiduConfigShape> = OutputFor<
	Config,
	AuthCollectionSlug<Config>
>;

export type AuthCollectionSlug<Config extends RiduConfigShape> = {
	[Slug in CollectionSlug<Config>]: ContractFor<Config, Slug>["auth"] extends true ? Slug : never;
}[CollectionSlug<Config>];

export type UploadCollectionSlug<Config extends RiduConfigShape> = {
	[Slug in CollectionSlug<Config>]: ContractFor<Config, Slug>["upload"] extends true ? Slug : never;
}[CollectionSlug<Config>];

export type VersionCollectionSlug<Config extends RiduConfigShape> = {
	[Slug in CollectionSlug<Config>]: ContractFor<Config, Slug>["versions"] extends true
		? Slug
		: never;
}[CollectionSlug<Config>];

export type DraftCollectionSlug<Config extends RiduConfigShape> = {
	[Slug in CollectionSlug<Config>]: ContractFor<Config, Slug> extends { drafts: true }
		? Slug
		: ContractFor<Config, Slug> extends { drafts: false }
			? never
			: ContractFor<Config, Slug>["versions"] extends true
				? Slug
				: never;
}[CollectionSlug<Config>];

export type CollectionVersionsFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> = ContractFor<Config, Slug>["versions"];

export type CollectionDraftsFor<
	Config extends RiduConfigShape,
	Slug extends CollectionSlug<Config>,
> =
	ContractFor<Config, Slug> extends { drafts: infer Drafts extends boolean }
		? Drafts
		: CollectionVersionsFor<Config, Slug>;

export type GlobalDraftsFor<Config extends RiduConfigShape, Slug extends GlobalSlug<Config>> =
	GlobalContractFor<Config, Slug> extends { drafts: infer Drafts extends boolean }
		? Drafts
		: GlobalContractFor<Config, Slug>["versions"];

export type TrashCollectionSlug<Config extends RiduConfigShape> = {
	[Slug in CollectionSlug<Config>]: ContractFor<Config, Slug>["trash"] extends true ? Slug : never;
}[CollectionSlug<Config>];

export interface LoginCredentials {
	email: string;
	password: string;
}

export interface CreateAPIKeyInput {
	name: string;
	expiresAt?: string;
}

export interface RequestOptions {
	signal?: AbortSignal;
	headers?: ConstructorParameters<typeof Headers>[0];
	keepalive?: boolean;
}

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
	revision?: number;
}

/** Optimistic-concurrency options for mutations that have no locale semantics. */
export interface RevisionOptions extends RequestOptions {
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

export type CreateOptions<
	Locale extends string = string,
	Versions extends boolean = boolean,
	Drafts extends boolean = boolean,
> = MutationLocaleOptions<Locale> & DraftCreateOption<Versions, Drafts>;

export type RestoreOptions<
	Locale extends string = string,
	Drafts extends boolean = boolean,
> = MutationOptions<Locale> & ([Drafts] extends [false] ? { draft?: false } : { draft?: boolean });

export interface CopyLocaleInput<Locale extends string = string> {
	from: Locale;
	to: Locale;
}

export interface UploadOptions<
	Data,
	Locale extends string = string,
> extends MutationLocaleOptions<Locale> {
	data?: Partial<Data>;
}

export interface UploadImageInput {
	focalX: number;
	focalY: number;
	cropX?: number;
	cropY?: number;
	cropWidth?: number;
	cropHeight?: number;
}

export interface CollectionAccessOptions<
	Data,
	Locale extends string = string,
> extends LocaleOptions<Locale> {
	id?: string;
	data?: Partial<Data>;
	trash?: boolean;
}

export interface CollectionSelectionOptions<
	Where,
	Locale extends string = string,
> extends LocaleOptions<Locale> {
	where?: Where;
	trash?: boolean;
}

export interface GlobalAccessOptions<
	Data,
	Locale extends string = string,
> extends LocaleOptions<Locale> {
	data?: Partial<Data>;
}

type DocumentStatus<Document> = Document extends { _status: infer Status }
	? Extract<Status, "draft" | "published">
	: "draft" | "published";

export interface Version<Document> {
	ID: string;
	DocumentID: string;
	Revision: number;
	Status: DocumentStatus<Document>;
	Snapshot: Document;
	CreatedAt: string;
}

export interface ListOptions<
	Where,
	Select,
	Populate,
	Locale extends string = string,
> extends LocaleOptions<Locale> {
	page?: number;
	limit?: number;
	depth?: number;
	where?: Where;
	select?: Select;
	populate?: Populate;
	sort?: readonly string[];
	trash?: boolean;
}

export interface FindOptions<
	Select,
	Populate,
	Locale extends string = string,
> extends LocaleOptions<Locale> {
	depth?: number;
	select?: Select;
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

export type LocaleResult<Single, All, Options> = Options extends object
	? "locale" extends keyof Options
		? "all" extends OptionProperty<Options, "locale">
			? OptionProperty<Options, "locale"> extends "all"
				? All
				: Single | All
			: Single
		: Single
	: Single;

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

export interface ClientOptions {
	baseURL: string;
	fetch?: (
		input: Parameters<typeof globalThis.fetch>[0],
		init?: Parameters<typeof globalThis.fetch>[1]
	) => ReturnType<typeof globalThis.fetch>;
	headers?:
		| ConstructorParameters<typeof Headers>[0]
		| (() =>
				| ConstructorParameters<typeof Headers>[0]
				| Promise<ConstructorParameters<typeof Headers>[0]>);
	credentials?: RequestInit["credentials"];
	middleware?: readonly Middleware[];
}

export type MiddlewareNext = (request: Request) => Promise<Response>;

export type Middleware = (request: Request, next: MiddlewareNext) => Promise<Response>;

export interface RiduClient<Config extends RiduConfigShape = DefaultRiduConfig> {
	schema(options?: RequestOptions): Promise<SchemaManifest>;
	request(path: string, init?: RequestInit, options?: RequestOptions): Promise<Response>;
	requestPlugin<Result = unknown>(
		plugin: string,
		path: string,
		body: unknown,
		options?: RequestOptions
	): Promise<Result>;

	login<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		credentials: LoginCredentials,
		options?: RequestOptions
	): Promise<AuthSession<OutputFor<Config, Slug>>>;

	session(options?: RequestOptions): Promise<AuthSession<AuthUser<Config>>>;
	refreshSession(options?: RequestOptions): Promise<AuthSession<AuthUser<Config>>>;

	logout(options?: RequestOptions): Promise<LogoutEnvelope>;

	logoutAll(options?: RequestOptions): Promise<LogoutEnvelope>;

	sessions(options?: RequestOptions): Promise<AuthSessionInfo[]>;

	revokeSession(id: string, options?: RequestOptions): Promise<DeleteEnvelope>;

	requestPasswordReset<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		email: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope>;

	resetPassword<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		token: string,
		password: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope>;

	authBootstrap<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		options?: RequestOptions
	): Promise<AuthBootstrapEnvelope>;

	createAuthUser<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		data: CreateFor<Config, Slug>,
		password: string,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	requestVerification<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		email: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope>;

	verifyEmail<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		token: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope>;

	changePassword(
		currentPassword: string,
		password: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope>;

	createAPIKey(input: CreateAPIKeyInput, options?: RequestOptions): Promise<APIKey>;

	apiKeys(options?: RequestOptions): Promise<APIKeyInfo[]>;

	revokeAPIKey(id: string, options?: RequestOptions): Promise<DeleteEnvelope>;

	forceUnlock<Slug extends AuthCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<AuthActionEnvelope>;

	preference<Value = unknown>(key: string, options?: RequestOptions): Promise<Value>;

	setPreference<Value>(key: string, value: Value, options?: RequestOptions): Promise<Value>;

	deletePreference(key: string, options?: RequestOptions): Promise<DeleteEnvelope>;

	resetPreferences(options?: RequestOptions): Promise<AuthActionEnvelope>;

	collectionAccess<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		options?: CollectionAccessOptions<
			CreateFor<Config, Slug> & UpdateFor<Config, Slug>,
			LocaleFor<Config>
		>
	): Promise<AccessCapabilitiesEnvelope>;

	resolveFilteredSelection<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		options?: CollectionSelectionOptions<WhereFor<Config, Slug>, LocaleFor<Config>>
	): Promise<CollectionSelectionEnvelope>;

	documentLock<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<DocumentLockEnvelope>;

	acquireDocumentLock<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		takeover?: boolean,
		options?: RequestOptions
	): Promise<DocumentLockEnvelope>;

	releaseDocumentLock<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<DeleteEnvelope>;

	globalAccess<Slug extends GlobalSlug<Config>>(
		slug: Slug,
		options?: GlobalAccessOptions<GlobalUpdateFor<Config, Slug>, LocaleFor<Config>>
	): Promise<AccessCapabilitiesEnvelope>;

	createPreviewToken<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<PreviewToken>;

	revokePreviewToken(token: string, options?: RequestOptions): Promise<AuthActionEnvelope>;

	preview<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		token: string,
		options?: RequestOptions
	): Promise<OutputFor<Config, Slug>>;

	createGlobalPreviewToken<Slug extends DraftGlobalSlug<Config>>(
		slug: Slug,
		options?: RequestOptions
	): Promise<PreviewToken>;

	previewGlobal<Slug extends DraftGlobalSlug<Config>>(
		slug: Slug,
		token: string,
		options?: RequestOptions
	): Promise<GlobalOutputFor<Config, Slug>>;

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

	count<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		options?: Pick<
			ListOptions<WhereFor<Config, Slug>, never, never, LocaleFor<Config>>,
			"where" | "trash" | "locale" | "fallbackLocale" | "signal" | "headers"
		>
	): Promise<CountEnvelope>;

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

	create<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		data: CreateFor<Config, Slug>,
		options?: CreateOptions<
			LocaleFor<Config>,
			CollectionVersionsFor<Config, Slug>,
			CollectionDraftsFor<Config, Slug>
		>
	): Promise<OutputFor<Config, Slug>>;

	duplicate<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		overrides?: Partial<Omit<CreateFor<Config, Slug>, "id">>,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	copyLocale<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		input: CopyLocaleInput<LocaleFor<Config>>,
		options?: RevisionOptions
	): Promise<OutputFor<Config, Slug>>;

	update<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		data: UpdateFor<Config, Slug>,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	mutateJoin<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		field: string,
		input: JoinMutationInput,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<JoinMutationEnvelope<OutputFor<Config, Slug>>>;

	delete<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<DeleteEnvelope>;

	bulkUpdate<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		data: UpdateFor<Config, Slug>,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	bulkPublish<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	bulkUnpublish<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	bulkDelete<Slug extends CollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	bulkRestoreDeleted<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	bulkDeletePermanent<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		ids: readonly string[],
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	emptyTrash<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>[]>;

	restoreDeleted<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	deletePermanent<Slug extends TrashCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationLocaleOptions<LocaleFor<Config>>
	): Promise<DeleteEnvelope>;

	upload<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		file: Blob,
		options?: UploadOptions<CreateFor<Config, Slug>, LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	uploadFromURL<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		url: string,
		options?: UploadOptions<CreateFor<Config, Slug>, LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	updateUploadImage<Slug extends UploadCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		input: UploadImageInput,
		options?: RevisionOptions
	): Promise<OutputFor<Config, Slug>>;

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

	schedulePublish<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		runAt: string | Date,
		options?: RevisionOptions
	): Promise<ScheduledPublish>;

	scheduledPublishes<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: RequestOptions
	): Promise<ScheduledPublish[]>;

	cancelScheduledPublish<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		jobID: string,
		options?: RequestOptions
	): Promise<DeleteEnvelope>;

	publish<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	publishChanges<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		data: UpdateFor<Config, Slug>,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	unpublish<Slug extends DraftCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<OutputFor<Config, Slug>>;

	restore<Slug extends VersionCollectionSlug<Config>>(
		collection: Slug,
		id: string,
		revision: number,
		options?: RestoreOptions<LocaleFor<Config>, CollectionDraftsFor<Config, Slug>>
	): Promise<OutputFor<Config, Slug>>;

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

	updateGlobal<Slug extends GlobalSlug<Config>>(
		slug: Slug,
		data: GlobalUpdateFor<Config, Slug>,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<GlobalOutputFor<Config, Slug>>;

	copyGlobalLocale<Slug extends GlobalSlug<Config>>(
		slug: Slug,
		input: CopyLocaleInput<LocaleFor<Config>>,
		options?: RevisionOptions
	): Promise<GlobalOutputFor<Config, Slug>>;

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

	publishGlobal<Slug extends VersionGlobalSlug<Config>>(
		slug: Slug,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<GlobalOutputFor<Config, Slug>>;

	publishGlobalChanges<Slug extends VersionGlobalSlug<Config>>(
		slug: Slug,
		data: GlobalUpdateFor<Config, Slug>,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<GlobalOutputFor<Config, Slug>>;

	unpublishGlobal<Slug extends DraftGlobalSlug<Config>>(
		slug: Slug,
		options?: MutationOptions<LocaleFor<Config>>
	): Promise<GlobalOutputFor<Config, Slug>>;

	restoreGlobal<Slug extends VersionGlobalSlug<Config>>(
		slug: Slug,
		revision: number,
		options?: RestoreOptions<LocaleFor<Config>, GlobalDraftsFor<Config, Slug>>
	): Promise<GlobalOutputFor<Config, Slug>>;
}
