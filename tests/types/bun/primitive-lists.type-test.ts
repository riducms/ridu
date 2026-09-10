import type {
	PrimitiveProductsCreate,
	PrimitiveProductsUpdate,
	PrimitiveProductsWhere,
	PrimitiveProductsAllLocales,
	PrimitiveProductsBodyBlocksBlockCardInput,
} from "../../contracts/primitivelists/generated/ridu.generated";

const create: PrimitiveProductsCreate = {
	title: "Oak chair",
	sellingPoints: ["Solid oak", "Solid oak"],
	availableSizes: [0, 8, 10],
	variants: [{ points: ["Natural finish"], sizes: [8] }],
	content: [{ blockType: "card", points: ["Made by hand"] }],
};
const update: PrimitiveProductsUpdate = { availableSizes: [], localizedPoints: null };
const where: PrimitiveProductsWhere = {
	sellingPoints: { in: ["Solid oak"] },
	availableSizes: { equals: null, exists: true },
};
const translations: Partial<PrimitiveProductsAllLocales> = {
	localizedPoints: { en: ["Oak"], fr: ["Chêne"] },
	localizedSizes: { en: [0], fr: null },
};
const embedded: PrimitiveProductsBodyBlocksBlockCardInput = {
	blockType: "card",
	points: ["Oak"],
	sizes: [0, 10],
};

// @ts-expect-error A positive MinRows requires a create list, even without Required.
const missing: PrimitiveProductsCreate = { title: "Chair" };
// @ts-expect-error Text lists cannot use a scalar string.
const scalar: PrimitiveProductsUpdate = { sellingPoints: "Oak" };
// @ts-expect-error Text and number lists are different logical types.
const mixed: PrimitiveProductsUpdate = { availableSizes: ["8"] };
// @ts-expect-error Primitive list elements cannot be null.
const nullableElement: PrimitiveProductsUpdate = { availableSizes: [null] };
// @ts-expect-error Positive MinRows forbids clearing the list to null.
const clearedRequired: PrimitiveProductsUpdate = { sellingPoints: null };
// @ts-expect-error Membership candidates must use the list's primitive type.
const wrongMembership: PrimitiveProductsWhere = { availableSizes: { in: ["8"] } };
// @ts-expect-error Equality is only supported for null, not complete lists.
const equality: PrimitiveProductsWhere = { availableSizes: { equals: [8] } };
// @ts-expect-error Primitive lists do not inherit scalar substring operators.
const substring: PrimitiveProductsWhere = { sellingPoints: { contains: "Oak" } };

void [create, update, where, translations, embedded, missing, scalar, mixed, nullableElement];
void [clearedRequired, wrongMembership, equality, substring];
