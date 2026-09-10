import {
	createClient,
	type UnifiedArticles,
	type UnifiedArticlesAllLocales,
	type UnifiedArticlesCreate,
	type UnifiedArticlesUpdate,
	type UnifiedArticlesWhere,
} from "./generated/ridu.generated";

const create: UnifiedArticlesCreate = {
	title: "Unified contract",
	sections: [{ products: [{ sku: "SKU-A" }] }],
	content: [{ blockType: "card", accent: "red" }],
	author: "user",
	localizedTitle: "English",
	privateNote: "Read access does not prohibit a write",
};
const update: UnifiedArticlesUpdate = {
	sku: null,
	sections: [{ _key: "A", products: [{ _key: "B", sku: "SKU-B" }] }],
};
const selected: UnifiedArticles = { id: "id", createdAt: "", updatedAt: "" };
const output: UnifiedArticles = {
	...selected,
	sections: [{ _key: "A", products: [{ _key: "B" }] }],
	content: [{ blockType: "note", _key: "C" }],
	author: { id: "user", createdAt: "", updatedAt: "", name: "Ada" },
	summary: "Article: Unified contract",
};
const all: UnifiedArticlesAllLocales = {
	id: "id",
	createdAt: "",
	updatedAt: "",
	localizedTitle: { en: "English", fr: null },
	localizedMeta: { description: { en: "Nested" } },
};
const where: UnifiedArticlesWhere = {
	"sections.products.sku": { equals: "SKU-B" },
	presentationHidden: { equals: "Presentation is independent of access" },
};

// @ts-expect-error Required create title cannot be omitted.
const missing: UnifiedArticlesCreate = {};
// @ts-expect-error Required defaulted text permits omission, not null.
const nullDefault: UnifiedArticlesCreate = { title: "Title", defaulted: null };
// @ts-expect-error Computed output never appears in create input.
const computed: UnifiedArticlesCreate = { title: "Title", summary: "forged" };
// @ts-expect-error Reference input is an ID, while reads may populate.
const populated: UnifiedArticlesCreate = { title: "Title", author: { id: "user" } };
// @ts-expect-error A read-policy protected field cannot appear in Where.
const protectedQuery: UnifiedArticlesWhere = { privateNote: { equals: "secret" } };
// @ts-expect-error A persisted array row always carries its stable identity.
const missingKey: UnifiedArticles = { ...selected, sections: [{}] };
// @ts-expect-error Block discriminators are the finite authored cases.
const invalidBlock: UnifiedArticlesUpdate = { content: [{ blockType: "unknown" }] };

const client = createClient({ baseURL: "http://localhost" });
async function contract() {
	const created = await client.create("unified-articles", create);
	await client.update("unified-articles", created.id, update);
	const docs = await client.list("unified-articles", { where });
	return docs;
}
void [
	output,
	all,
	missing,
	nullDefault,
	computed,
	populated,
	protectedQuery,
	missingKey,
	invalidBlock,
	contract,
];
