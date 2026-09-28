import { describe, expect, it, mock } from "bun:test";
import { buildSchema, parse } from "graphql";

// Registry tests keep the page inert; the admin browser suite renders the real component.
mock.module("../src/playground-route.svelte", () => ({ default: () => ({}) }));
const { graphqlAdminPlugin } = await import("../src");
const { playgroundLoader } = await import("../src/loader");
const { executeOperation, operationNameAt, parseVariables } = await import("../src/operation");
const { starterQuery } = await import("../src/starter-query");
const { describeType, schemaRoots, searchSchema } = await import("../src/schema-docs");

const schema = buildSchema(`
	"""A published article."""
	type Post { id: ID! title: String! score: Float tags: [String!] author: User }
	type User { id: ID! email: String! }
	type PostsPage { docs: [Post!]! totalDocs: Int! }
	enum Status { DRAFT PUBLISHED }
	union Result = Post | User
	input PostWhere { title: String }
		type Query {
			Post(id: ID!): Post
			Posts(limit: Int = 10, status: Status = DRAFT, where: PostWhere): PostsPage!
	}
	type Mutation { createPost(title: String!): Post }
`);

describe("GraphQL admin contract", () => {
	it("pairs the playground route with the Go loader contract", () => {
		expect(graphqlAdminPlugin.key).toBe("graphql");
		expect(graphqlAdminPlugin.pairingVersion).toBe(1);
		const [route] = graphqlAdminPlugin.routes;
		expect(route?.path).toBe("graphql");
		expect(route?.navigation?.labelKey).toBe("plugin.graphql:navigation");
		expect(route?.loader).toBe(playgroundLoader);
		// Must stay byte-identical to the manifest the Go plugin produces; see playground_test.go.
		expect(JSON.stringify(playgroundLoader.contract)).toBe(
			'{"key":"graphql-playground","input":{"kind":"object"},"output":{"kind":"object","fields":{"endpoint":{"kind":"string"},"schema":{"kind":"string"}}}}'
		);
	});
});

describe("GraphQL operations", () => {
	it("accepts empty or object variables only", () => {
		expect(parseVariables("  ")).toEqual({ ok: true });
		expect(parseVariables('{"limit": 5}')).toEqual({ ok: true, value: { limit: 5 } });
		expect(parseVariables("[1]")).toEqual({ ok: false });
		expect(parseVariables("{limit: 5}")).toEqual({ ok: false });
	});

	it("names the operation under the cursor only when several exist", () => {
		const document = "query First { a }\nquery Second { b }";
		const adjacent = "query First { a }query Second { b }";
		expect(operationNameAt("query Only { a }", 3)).toBeUndefined();
		expect(operationNameAt(document, 3)).toBe("First");
		expect(operationNameAt(document, document.indexOf("Second"))).toBe("Second");
		expect(operationNameAt(adjacent, adjacent.indexOf("query Second"))).toBe("Second");
		expect(operationNameAt(adjacent, adjacent.length)).toBe("Second");
		expect(operationNameAt("query {", 0)).toBeUndefined();
	});

	it("sends the operation with same-origin admin credentials", async () => {
		let sent: { input: unknown; init: RequestInit | undefined } | undefined;
		const fetchOperation = (async (input: unknown, init?: RequestInit) => {
			sent = { input, init };
			return new Response('{"data":{"__typename":"Query"}}', { status: 200 });
		}) as unknown as typeof fetch;
		const response = await executeOperation(
			"/api/graphql",
			{ query: "{ __typename }", variables: { a: 1 } },
			new AbortController().signal,
			fetchOperation
		);
		expect(sent?.input).toBe("/api/graphql");
		expect(sent?.init?.method).toBe("POST");
		expect(sent?.init?.credentials).toBe("same-origin");
		expect(JSON.parse(String(sent?.init?.body))).toEqual({
			query: "{ __typename }",
			variables: { a: 1 },
		});
		expect(response.status).toBe(200);
		expect(response.body).toBe('{\n  "data": {\n    "__typename": "Query"\n  }\n}');
	});
});

describe("GraphQL starter and docs", () => {
	it("starts with the first collection list and its scalar fields", () => {
		const query = starterQuery(schema);
		expect(query).toContain("Posts(limit: 10) {");
		expect(query).toContain("      id\n      title\n      score\n");
		expect(query).toContain("totalDocs");
		expect(() => parse(query)).not.toThrow();
		expect(starterQuery(undefined)).toContain("__typename");
	});

	it("describes roots, fields, arguments, enums, and unions", () => {
		expect(schemaRoots(schema)).toEqual([
			{ operation: "query", type: "Query" },
			{ operation: "mutation", type: "Mutation" },
		]);
		const query = describeType(schema, "Query");
		const posts = query?.fields.find((field) => field.name === "Posts");
		expect(posts?.type).toEqual({ label: "PostsPage!", name: "PostsPage" });
		expect(posts?.args).toEqual([
			{ name: "limit", type: { label: "Int", name: "Int" }, defaultValue: "10" },
			{ name: "status", type: { label: "Status", name: "Status" }, defaultValue: "DRAFT" },
			{ name: "where", type: { label: "PostWhere", name: "PostWhere" } },
		]);
		expect(describeType(schema, "Post")?.description).toBe("A published article.");
		expect(
			describeType(schema, "Post")?.fields.find((field) => field.name === "tags")?.type
		).toEqual({
			label: "[String!]",
			name: "String",
		});
		expect(describeType(schema, "Status")?.values.map((value) => value.name)).toEqual([
			"DRAFT",
			"PUBLISHED",
		]);
		expect(describeType(schema, "Result")?.members.map((member) => member.name)).toEqual([
			"Post",
			"User",
		]);
		expect(describeType(schema, "PostWhere")?.kind).toBe("input");
		expect(describeType(schema, "Missing")).toBeUndefined();
	});

	it("searches type and field names", () => {
		expect(searchSchema(schema, "email")).toEqual([{ type: "User", field: "email" }]);
		expect(searchSchema(schema, "post").map((match) => match.type)).toContain("PostWhere");
		expect(searchSchema(schema, " ")).toEqual([]);
	});
});
