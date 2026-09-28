import {
	isEnumType,
	isInputObjectType,
	isInterfaceType,
	isListType,
	isNonNullType,
	isObjectType,
	isUnionType,
	astFromValue,
	print,
	type GraphQLArgument,
	type GraphQLField,
	type GraphQLInputField,
	type GraphQLSchema,
	type GraphQLType,
} from "graphql";

/** A type as written in SDL, such as `[Post!]!`, with the named type it links to. */
export interface TypeReference {
	label: string;
	name: string;
}

export interface DocArgument {
	name: string;
	type: TypeReference;
	description?: string;
	defaultValue?: string;
}

export interface DocField {
	name: string;
	type: TypeReference;
	description?: string;
	deprecation?: string;
	args: DocArgument[];
}

export type DocTypeKind = "object" | "interface" | "input" | "enum" | "union" | "scalar";

export interface DocType {
	name: string;
	kind: DocTypeKind;
	description?: string;
	fields: DocField[];
	values: { name: string; description?: string }[];
	members: TypeReference[];
}

export interface DocRoot {
	operation: "query" | "mutation" | "subscription";
	type: string;
}

/** The entry points shown when the explorer opens. */
export function schemaRoots(schema: GraphQLSchema): DocRoot[] {
	const roots: DocRoot[] = [];
	const query = schema.getQueryType();
	const mutation = schema.getMutationType();
	const subscription = schema.getSubscriptionType();
	if (query) roots.push({ operation: "query", type: query.name });
	if (mutation) roots.push({ operation: "mutation", type: mutation.name });
	if (subscription) roots.push({ operation: "subscription", type: subscription.name });
	return roots;
}

export function describeType(schema: GraphQLSchema, name: string): DocType | undefined {
	const type = schema.getType(name);
	if (type === undefined || type === null) return undefined;
	const description = type.description ?? undefined;
	const base = { name: type.name, ...(description ? { description } : {}) };
	if (isObjectType(type) || isInterfaceType(type))
		return {
			...base,
			kind: isObjectType(type) ? "object" : "interface",
			fields: Object.values(type.getFields()).map(describeField),
			values: [],
			members: [],
		};
	if (isInputObjectType(type))
		return {
			...base,
			kind: "input",
			fields: Object.values(type.getFields()).map(describeInputField),
			values: [],
			members: [],
		};
	if (isEnumType(type))
		return {
			...base,
			kind: "enum",
			fields: [],
			values: type.getValues().map((value) => ({
				name: value.name,
				...(value.description ? { description: value.description } : {}),
			})),
			members: [],
		};
	if (isUnionType(type))
		return {
			...base,
			kind: "union",
			fields: [],
			values: [],
			members: type.getTypes().map((member) => ({ label: member.name, name: member.name })),
		};
	return { ...base, kind: "scalar", fields: [], values: [], members: [] };
}

/** Types and fields whose name contains the search text, for the explorer's search box. */
export function searchSchema(schema: GraphQLSchema, text: string, limit = 50) {
	const needle = text.trim().toLowerCase();
	if (needle === "") return [];
	const matches: { type: string; field?: string }[] = [];
	for (const type of Object.values(schema.getTypeMap())) {
		if (type.name.startsWith("__")) continue;
		if (type.name.toLowerCase().includes(needle)) matches.push({ type: type.name });
		if (isObjectType(type) || isInterfaceType(type) || isInputObjectType(type))
			for (const field of Object.values(type.getFields()))
				if (field.name.toLowerCase().includes(needle))
					matches.push({ type: type.name, field: field.name });
		if (matches.length >= limit) break;
	}
	return matches.slice(0, limit);
}

export function typeReference(type: GraphQLType): TypeReference {
	if (isNonNullType(type)) {
		const inner = typeReference(type.ofType);
		return { label: `${inner.label}!`, name: inner.name };
	}
	if (isListType(type)) {
		const inner = typeReference(type.ofType);
		return { label: `[${inner.label}]`, name: inner.name };
	}
	return { label: type.name, name: type.name };
}

function describeField(field: GraphQLField<unknown, unknown>): DocField {
	return {
		name: field.name,
		type: typeReference(field.type),
		...(field.description ? { description: field.description } : {}),
		...(field.deprecationReason ? { deprecation: field.deprecationReason } : {}),
		args: field.args.map(describeArgument),
	};
}

function describeInputField(field: GraphQLInputField): DocField {
	return {
		name: field.name,
		type: typeReference(field.type),
		...(field.description ? { description: field.description } : {}),
		args: [],
	};
}

function describeArgument(argument: GraphQLArgument): DocArgument {
	const defaultValue =
		argument.defaultValue === undefined
			? undefined
			: astFromValue(argument.defaultValue, argument.type);
	return {
		name: argument.name,
		type: typeReference(argument.type),
		...(argument.description ? { description: argument.description } : {}),
		...(defaultValue === undefined || defaultValue === null
			? {}
			: { defaultValue: print(defaultValue) }),
	};
}
