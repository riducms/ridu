import {
	getNamedType,
	isLeafType,
	isListType,
	isNonNullType,
	isObjectType,
	type GraphQLField,
	type GraphQLObjectType,
	type GraphQLSchema,
} from "graphql";

/**
 * A first query that runs against this application's schema: the first collection list with a few
 * of its scalar fields. It falls back to `__typename` when the schema has no list query.
 */
export function starterQuery(schema: GraphQLSchema | undefined) {
	const header =
		"# Press Ctrl+Enter (⌘+Enter on a Mac) to run.\n" + "# Press Ctrl+Space for suggestions.\n";
	const list = schema === undefined ? undefined : firstListQuery(schema);
	if (list === undefined) return `${header}query {\n  __typename\n}\n`;
	const fields = scalarFields(list.document).slice(0, 3);
	const selection = fields.map((name) => `      ${name}\n`).join("");
	return `${header}query {\n  ${list.field.name}(limit: 10) {\n    docs {\n${selection}    }\n    totalDocs\n  }\n}\n`;
}

function firstListQuery(schema: GraphQLSchema) {
	const query = schema.getQueryType();
	if (query === null || query === undefined) return undefined;
	for (const field of Object.values(query.getFields())) {
		const page = getNamedType(field.type);
		if (!isObjectType(page) || field.args.some((argument) => isNonNullType(argument.type)))
			continue;
		const fields = page.getFields();
		const docs = fields.docs;
		if (docs === undefined || fields.totalDocs === undefined) continue;
		const document = getNamedType(docs.type);
		if (isObjectType(document) && isListType(unwrapNonNull(docs.type))) return { field, document };
	}
	return undefined;
}

function scalarFields(type: GraphQLObjectType) {
	return Object.values(type.getFields())
		.filter(
			(field: GraphQLField<unknown, unknown>) =>
				field.args.length === 0 && isLeafType(unwrapNonNull(field.type))
		)
		.map((field) => field.name);
}

function unwrapNonNull<T>(type: T) {
	return isNonNullType(type) ? type.ofType : type;
}
