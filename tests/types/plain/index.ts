import {
	createClient,
	type Authors,
	type AuthorsPopulationSelect,
	type AuthorsSelect,
	type Posts,
	type PostsAllLocales,
	type PostsCreate,
	type PostsPopulate,
	type PostsSelect,
	type PostsUpdate,
	type PostsValidationPath,
	type PostsWhere,
	type RiduConfig,
} from "../../../testdata/generated/ridu.generated";
import type { AdminClient } from "../../../admin/src/core/api/admin-client";
import {
	createClient as createRuntimeClient,
	type MutationOptions,
	type RevisionOptions,
} from "@riducms/sdk";
import type { ApplyPopulate } from "../../../packages/sdk/src/types";
import type {
	CollectionPageEnvelope,
	PageEnvelope,
	Pagination,
	UncountedPagination,
} from "../../../packages/protocol/src/index";

const author: Authors = {
	id: "author_1",
	createdAt: "2026-08-04T10:00:00Z",
	updatedAt: "2026-08-04T10:00:00Z",
	_status: "published",
	_revision: 1,
	name: "Ada",
};

const post: Posts = {
	id: "post_1",
	createdAt: "2026-08-04T10:00:00Z",
	updatedAt: "2026-08-04T10:00:00Z",
	_status: "draft",
	_revision: 1,
	title: "Typed contracts",
	status: "draft",
	author: author.id,
	seo: { description: null, reviewer: null },
	subject: null,
};

const create: PostsCreate = {
	title: "Typed contracts",
	author: author.id,
};
const update: PostsUpdate = { status: "published" };
const where: PostsWhere = {
	and: [{ title: { equals: "Typed contracts" } }, { author: { in: [author.id] } }],
	seo: { exists: true },
	"seo.description": { exists: true },
};
const select: PostsSelect = { title: true, seo: true };
const authorRootSelect: AuthorsSelect = { displayName: true, posts: true };
const authorPopulationSelect: AuthorsPopulationSelect = {
	id: true,
	createdAt: true,
	updatedAt: true,
	deletedAt: true,
	_status: true,
	_revision: true,
	name: true,
};
const invalidVirtualPopulationSelect: AuthorsPopulationSelect = {
	// @ts-expect-error virtual output fields are not valid population target projections.
	displayName: true,
};
const invalidJoinPopulationSelect: AuthorsPopulationSelect = {
	// @ts-expect-error join output fields are not valid population target projections.
	posts: true,
};
const invalidPresentationPopulation: PostsPopulate = {
	author: {
		select: {
			// @ts-expect-error population options use AuthorsPopulationSelect, not AuthorsSelect.
			displayName: true,
		},
	},
};
void authorRootSelect;
void authorPopulationSelect;
void invalidVirtualPopulationSelect;
void invalidJoinPopulationSelect;
void invalidPresentationPopulation;
const populate: PostsPopulate = {
	author: { id: true, name: true },
	"seo.reviewer": { id: true, name: true },
	subject: { depth: 2, select: { title: true } },
};

const client = createClient({ baseURL: "https://cms.example.test" });
void client.create("posts", create);
void client.update("posts", post.id, update);
void client.list("posts", { where, select, populate });

const plainPage = client.list("posts");
void plainPage.then((page) => {
	// @ts-expect-error ordinary list pages do not include access metadata.
	void page.access;
});
const explicitlyPlainPage = client.list("posts", { includeAccess: false });
void explicitlyPlainPage.then((page) => {
	// @ts-expect-error literal false preserves the ordinary page envelope.
	void page.access;
});
const enrichedPage = client.list("posts", { includeAccess: true });
void enrichedPage.then((page) => page.access.collection.operations.read);
declare const includeAccess: boolean;
const conditionalPage = client.list("posts", { includeAccess });
void plainPage.then((page) => {
	const total: number = page.pagination.totalDocs;
	const pages: number = page.pagination.totalPages;
	void total;
	void pages;
});
const uncountedPage = client.list("posts", { pagination: false, limit: 20 });
void uncountedPage.then((page) => {
	const hasNextPage: boolean = page.pagination.hasNextPage;
	void hasNextPage;
	// @ts-expect-error pagination: false pages carry no total.
	const total: number = page.pagination.totalDocs;
	void total;
});
const uncountedAccessPage = client.list("posts", { pagination: false, includeAccess: true });
void uncountedAccessPage.then((page) => {
	void page.access.collection.operations.read;
	// @ts-expect-error pagination: false pages carry no page count.
	const pages: number = page.pagination.totalPages;
	void pages;
});
const explicitlyCountedPage = client.list("posts", { pagination: true });
declare const pagination: boolean;
const conditionallyCountedPage = client.list("posts", { pagination });
void conditionallyCountedPage.then((page) => {
	const total: number | undefined = page.pagination.totalDocs;
	// @ts-expect-error a widened pagination option may omit totals.
	const counted: number = page.pagination.totalDocs;
	void total;
	void counted;
});
void client.create("posts", create, { locale: "fr", draft: true });
void client.create("history", { event: "generated" }, { draft: false }).then((document) => {
	const published: "published" = document._status;
	void published;
});

const projectedPost = client.find("posts", post.id, { select: { title: true } });
void projectedPost.then((document) => {
	void document.id;
	void document.title;
	// @ts-expect-error a literal select projection omits unselected content fields.
	void document.status;
});

const metadataOnlyPost = client.find("posts", post.id, { select: { id: true } });
void metadataOnlyPost.then((document) => {
	void document.id;
	// @ts-expect-error metadata-only projections omit every authored field.
	void document.title;
});

const nestedPopulation = client.find("posts", post.id, {
	select: { seo: true },
	populate: { "seo.reviewer": { select: { name: true } } },
});

const metadataOnlyPopulation = client.find("posts", post.id, {
	select: { author: true },
	populate: { author: { select: { id: true } } },
});
void metadataOnlyPopulation.then((document) => {
	if (document.author && typeof document.author !== "string") {
		void document.author.id;
		// @ts-expect-error a metadata-only target projection omits authored target fields.
		void document.author.name;
	}
});

const depthOnlyPopulation = client.find("posts", post.id, {
	select: { author: true },
	populate: { author: { depth: 2 } },
});
void depthOnlyPopulation.then((document) => {
	if (document.author && typeof document.author !== "string") void document.author.bio;
});
void nestedPopulation.then((document) => {
	const reviewer = document.seo?.reviewer;
	if (reviewer && typeof reviewer !== "string") {
		void reviewer.name;
		// @ts-expect-error populated target projections omit unselected target fields.
		void reviewer.bio;
	}
});

type Equal<Left, Right> =
	(<Value>() => Value extends Left ? 1 : 2) extends <Value>() => Value extends Right ? 1 : 2
		? true
		: false;
type Expect<Value extends true> = Value;
type ConditionalPage = Expect<
	Equal<Awaited<typeof conditionalPage>, PageEnvelope<Posts> | CollectionPageEnvelope<Posts>>
>;
type UncountedPage = Expect<
	Equal<Awaited<typeof uncountedPage>, PageEnvelope<Posts, UncountedPagination>>
>;
type UncountedAccessPage = Expect<
	Equal<Awaited<typeof uncountedAccessPage>, CollectionPageEnvelope<Posts, UncountedPagination>>
>;
type ExplicitlyCountedPage = Expect<
	Equal<Awaited<typeof explicitlyCountedPage>, PageEnvelope<Posts, Pagination>>
>;
type ConditionallyCountedPage = Expect<
	Equal<
		Awaited<typeof conditionallyCountedPage>,
		PageEnvelope<Posts, Pagination | UncountedPagination>
	>
>;
type CollisionTarget = {
	id: string;
	createdAt: string;
	updatedAt: string;
	depth?: string;
	select?: string;
	other?: string;
};
type CollisionDocument = { id: string; author: string | CollisionTarget };
type PopulatedAuthor<Population> = Exclude<
	ApplyPopulate<CollisionDocument, Population>["author"],
	string
>;

type AuthoredDepthPopulation = Expect<
	Equal<
		keyof PopulatedAuthor<{ author: { depth: true } }>,
		"id" | "createdAt" | "updatedAt" | "depth"
	>
>;
type AuthoredSelectPopulation = Expect<
	Equal<
		keyof PopulatedAuthor<{ author: { select: true } }>,
		"id" | "createdAt" | "updatedAt" | "select"
	>
>;
type AuthoredCollisionPopulation = Expect<
	Equal<
		keyof PopulatedAuthor<{ author: { depth: true; select: true } }>,
		"id" | "createdAt" | "updatedAt" | "depth" | "select"
	>
>;
type DepthOptionPopulation = Expect<
	Equal<keyof PopulatedAuthor<{ author: { depth: 2 } }>, keyof CollisionTarget>
>;
type SelectOptionPopulation = Expect<
	Equal<
		keyof PopulatedAuthor<{ author: { depth: 2; select: { other: true } } }>,
		"id" | "createdAt" | "updatedAt" | "other"
	>
>;
type EmptyShorthandPopulation = Expect<
	Equal<keyof PopulatedAuthor<{ author: Record<never, never> }>, "id" | "createdAt" | "updatedAt">
>;
type EmptySelectOptionPopulation = Expect<
	Equal<
		keyof PopulatedAuthor<{ author: { select: Record<never, never> } }>,
		"id" | "createdAt" | "updatedAt"
	>
>;
type BooleanPopulation = Expect<
	Equal<keyof PopulatedAuthor<{ author: true }>, keyof CollisionTarget>
>;
type DisabledPopulation = Expect<
	Equal<ApplyPopulate<CollisionDocument, { author: false }>, CollisionDocument>
>;

void (0 as unknown as AuthoredDepthPopulation);
void (0 as unknown as AuthoredSelectPopulation);
void (0 as unknown as AuthoredCollisionPopulation);
void (0 as unknown as DepthOptionPopulation);
void (0 as unknown as SelectOptionPopulation);
void (0 as unknown as EmptyShorthandPopulation);
void (0 as unknown as EmptySelectOptionPopulation);
void (0 as unknown as BooleanPopulation);
void (0 as unknown as DisabledPopulation);
void (0 as unknown as ConditionalPage);
void (0 as unknown as UncountedPage);
void (0 as unknown as UncountedAccessPage);
void (0 as unknown as ExplicitlyCountedPage);
void (0 as unknown as ConditionallyCountedPage);

const repeatedPopulation = client.find("posts", post.id, {
	select: { sections: true, layout: true },
	populate: {
		"sections.reviewer": { select: { name: true } },
		"layout.quote.source": { select: { name: true } },
	},
});
void repeatedPopulation.then((document) => {
	const reviewer = document.sections?.[0]?.reviewer;
	if (reviewer && typeof reviewer !== "string") {
		void reviewer.name;
		// @ts-expect-error array population preserves the target select projection.
		void reviewer.bio;
	}
	const quote = document.layout?.[0];
	if (quote?.blockType === "quote") {
		const source = quote.source;
		if (source && typeof source !== "string") {
			void source.name;
			// @ts-expect-error block population preserves the target select projection.
			void source.bio;
		}
	}
});

const polymorphicPopulation = client.find("posts", post.id, {
	select: { subject: true },
	populate: { subject: { select: { title: true } } },
});
void polymorphicPopulation.then((document) => {
	if (document.subject?.relationTo === "posts" && typeof document.subject.id !== "string") {
		void document.subject.id.title;
	}
});

const allLocales: Promise<PostsAllLocales> = client.find("posts", post.id, { locale: "all" });
void allLocales;
void allLocales.then((document) => {
	void document.title?.en;
	const localizedAuthor = document.author?.en;
	if (localizedAuthor && typeof localizedAuthor !== "string") {
		void localizedAuthor.name?.fr;
		// @ts-expect-error populated all-locale targets keep locale-keyed localized fields.
		void localizedAuthor.name?.toUpperCase();
	}
	// @ts-expect-error all-locale reads return locale-keyed localized fields.
	void document.title?.toUpperCase();
});

const projectedAllLocales = client.find("posts", post.id, {
	locale: "all",
	select: { title: true, author: true },
	populate: { author: { select: { name: true } } },
});
void projectedAllLocales.then((document) => {
	void document.title?.en;
	// @ts-expect-error an all-locale select still omits unselected content fields.
	void document.status;
	const localizedAuthor = document.author?.fr;
	if (localizedAuthor && typeof localizedAuthor !== "string") {
		void localizedAuthor.name?.en;
		// @ts-expect-error all-locale populated targets retain their literal target projection.
		void localizedAuthor.bio;
	}
});

const localeNamedGroupPopulation = client.find("posts", post.id, {
	locale: "all",
	select: { localeNamed: true },
	populate: { "localeNamed.en": { select: { name: true } } },
});
void localeNamedGroupPopulation.then((document) => {
	const localizedNameCollision = document.localeNamed?.en;
	if (localizedNameCollision && typeof localizedNameCollision !== "string") {
		void localizedNameCollision.name?.en;
		// @ts-expect-error an ordinary child named like a locale is still target-select narrowed.
		void localizedNameCollision.bio;
	}
});

const nestedIssuePath: PostsValidationPath = "seo.reviewer";
void nestedIssuePath;
const localizedIssuePath: PostsValidationPath = "author.fr";
void localizedIssuePath;

// Generated factories select the application contract explicitly.
const automaticClient = createClient({ baseURL: "https://cms.example.test" });
const automaticPost: Promise<Posts> = automaticClient.find("posts", post.id);
void automaticPost;
void automaticClient.create("posts", create);

// Explicit configs remain available for tooling and programs that load more than one manifest.
const explicitClient = createRuntimeClient<RiduConfig>({
	baseURL: "https://cms.example.test",
});
void explicitClient.list("authors", { where: { name: { contains: "Ada" } } });

// The framework-owned admin widens documents only at its manifest-driven cross-collection boundary.
// An exact generated client must enter that boundary without an integration cast.
const adminClient: AdminClient = client;
void adminClient;

// @ts-expect-error generated clients reject collection slugs outside the generated manifest.
void automaticClient.list("comments");

void automaticClient.create("posts", { status: "draft" });
void automaticClient.create("posts", { title: null }, { draft: true });
void automaticClient.update("posts", post.id, { title: null }, { draft: true, revision: 1 });
void automaticClient.discardDraft("posts", post.id, { revision: 2 });
void automaticClient.find("posts", post.id, { draft: false });
// @ts-expect-error a published create still requires its complete input.
void automaticClient.create("posts", { status: "draft" }, { draft: false });
// @ts-expect-error ordinary updates cannot clear required fields.
void automaticClient.update("posts", post.id, { title: null });
// @ts-expect-error draft inputs still reject undeclared fields.
void automaticClient.create("posts", { unknown: true }, { draft: true });
// @ts-expect-error draft inputs still enforce block discriminators.
void automaticClient.create("posts", { layout: [{ blockType: "unknown" }] }, { draft: true });

// @ts-expect-error the generated fixture has no upload-enabled collection.
void automaticClient.upload("posts", new Blob());

void automaticClient.versions("posts", post.id);

// @ts-expect-error generated locale literals reject unknown content locales.
void automaticClient.find("posts", post.id, { locale: "de" });

// @ts-expect-error mutations target one locale and cannot use the all-locales projection.
void automaticClient.update("posts", post.id, update, { locale: "all" });

// @ts-expect-error create does not consume optimistic-concurrency revisions.
void automaticClient.create("posts", create, { revision: 1 });

// @ts-expect-error bulk operations cannot apply one document revision to multiple records.
void automaticClient.bulkUpdate("posts", [post.id], update, { revision: 1 });

// @ts-expect-error duplicate does not consume optimistic-concurrency revisions.
void automaticClient.duplicate("posts", post.id, {}, { revision: 1 });

// @ts-expect-error delete currently has no If-Match transport contract.
void automaticClient.delete("posts", post.id, { revision: 1 });

const revisionBearingMutation: MutationOptions<RiduConfig["locale"]> = { revision: 1 };
// @ts-expect-error a revision-bearing options variable cannot bypass a no-revision method contract.
void automaticClient.delete("posts", post.id, revisionBearingMutation);

const revisionOnlyMutation: RevisionOptions = { revision: 1 };
void automaticClient.schedulePublish("posts", post.id, new Date(), revisionOnlyMutation);
void automaticClient.scheduleUnpublish("posts", post.id, new Date(), revisionOnlyMutation);
void automaticClient.update("posts", post.id, update, revisionOnlyMutation);

// @ts-expect-error scheduled unpublish requires draft support, not only version history.
void automaticClient.scheduleUnpublish("history", "history_1", new Date(), revisionOnlyMutation);

const localizedRevisionMutation: MutationOptions<RiduConfig["locale"]> = {
	locale: "fr",
	revision: 1,
};
// @ts-expect-error scheduled publication has a revision contract but no locale semantics.
void automaticClient.schedulePublish("posts", post.id, new Date(), localizedRevisionMutation);

// @ts-expect-error unversioned collections cannot request draft creation.
void automaticClient.create("authors", { name: "Ada" }, { draft: true });

// @ts-expect-error version history without drafts cannot create or restore a draft.
void automaticClient.create("history", { event: "invalid" }, { draft: true });

// @ts-expect-error unpublish is available only for draft-enabled resources.
void automaticClient.unpublish("history", "history_1");

// @ts-expect-error validation paths preserve exact nested vocabulary.
const invalidIssuePath: PostsValidationPath = "seo.unknown";
void invalidIssuePath;

// @ts-expect-error title is required when a post is created.
const missingTitle: PostsCreate = { status: "draft" };
void missingTitle;

// @ts-expect-error stored timestamps are output-only.
const generatedFieldIsNotWritable: PostsCreate = { title: "No", createdAt: "now" };
void generatedFieldIsNotWritable;

// @ts-expect-error select values follow the REST top-level boolean projection grammar.
const invalidSelect: PostsSelect = { title: "yes" };
void invalidSelect;

// @ts-expect-error nested select objects are not accepted by the REST decoder.
const invalidNestedSelect: PostsSelect = { seo: { description: true } };
void invalidNestedSelect;

const invalidNestedWhere: PostsWhere = {
	// @ts-expect-error REST where traversal uses canonical dotted paths, not nested objects.
	seo: { description: { exists: true } },
};
void invalidNestedWhere;

// A polymorphic relationship is filtered by membership of { relationTo, id } references.
const subjectWhere: PostsWhere = {
	or: [
		{ subject: { in: [{ relationTo: "authors", id: author.id }] } },
		{ not: { subject: { in: [{ relationTo: "posts", id: post.id }] } } },
		{ subject: { exists: false } },
	],
};
void client.list("posts", { where: subjectWhere });
// @ts-expect-error a bare ID does not say which collection it belongs to.
const subjectByID: PostsWhere = { subject: { in: [author.id] } };
const subjectEquality: PostsWhere = {
	// @ts-expect-error references compare by membership, not scalar equality.
	subject: { equals: { relationTo: "authors", id: author.id } },
};
// @ts-expect-error relationTo names one of the relationship's target collections.
const subjectTarget: PostsWhere = { subject: { in: [{ relationTo: "media", id: author.id }] } };
void [subjectByID, subjectEquality, subjectTarget];

// Block row filters are composed from each block definition under the row's dotted path.
const blockWhere: PostsWhere = {
	"layout.quote.source": { in: [author.id] },
	or: [{ "layout.quote.blockName": { like: "Pull" } }, { layout: { exists: false } }],
};
void client.list("posts", { where: blockWhere });
// @ts-expect-error block row filters keep the block slug segment.
const sluglessBlockWhere: PostsWhere = { "layout.source": { exists: true } };
// @ts-expect-error block row filters keep each field's operators.
const invalidBlockOperator: PostsWhere = { "layout.quote.source": { equals: 1 } };
void sluglessBlockWhere;
void invalidBlockOperator;

const invalidPolymorphicPopulate: PostsPopulate = {
	// @ts-expect-error polymorphic population uses one flat target selection, not per-target maps.
	subject: { authors: { name: true }, posts: { title: true } },
};
void invalidPolymorphicPopulate;

const rawClient = createRuntimeClient({ baseURL: "https://cms.example.test" });
void rawClient.schema();
// @ts-expect-error raw resource calls require an explicit application contract.
void rawClient.find("posts", post.id);
