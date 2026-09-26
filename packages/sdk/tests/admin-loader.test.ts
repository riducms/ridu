import { expect, test } from "bun:test";
import { createAdminLoader } from "../src/admin-loader";
import { createClient } from "../src";

test("loader refresh preserves source pathname and query inside one route parameter", async () => {
	let captured!: Request;
	const controller = new AbortController();
	const client = createClient({
		baseURL: "https://cms.example",
		fetch: async (request) => {
			captured = request as Request;
			return Response.json({ count: 2 });
		},
	});
	const route = "/collections/posts/a%2Fb?locale=fr&q=a%26b&route=application-input";
	await client.adminLoad("report", route, { signal: controller.signal });
	expect(new URL(captured.url).searchParams.get("route")).toBe(route);
	expect(captured.signal).toBe(controller.signal);
});

const loader = createAdminLoader<
	{ restaurant?: string; limit?: number },
	{ count: number; labels: string[] | null }
>({
	key: "restaurant-dashboard",
	input: { kind: "object", fields: { restaurant: { kind: "string" }, limit: { kind: "number" } } },
	output: {
		kind: "object",
		fields: {
			count: { kind: "number" },
			labels: { kind: "array", nullable: true, element: { kind: "string" } },
		},
	},
});

test("generated loader queries encode input and validate complete JSON output", () => {
	expect(loader.query({ restaurant: "a&b", limit: 10 })).toBe("restaurant=a%26b&limit=10");
	expect(loader.decode({ count: 2, labels: ["A"] })).toEqual({ count: 2, labels: ["A"] });
	expect(loader.decode({ count: 0, labels: null })).toEqual({ count: 0, labels: null });
	for (const value of [
		{ count: "2", labels: [] },
		{ count: 2 },
		null,
		{ count: NaN, labels: [] },
		{ count: 2, labels: [3] },
	]) {
		expect(() => loader.decode(value)).toThrow("Invalid data");
	}
});
