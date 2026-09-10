import { describe, expect, it } from "bun:test";
import { createClient, RiduError, type RiduConfigShape } from "../src";

interface Config extends RiduConfigShape {
	locales: "en" | "fr";
	collections: {
		products: {
			auth: false;
			upload: false;
			versions: false;
			trash: false;
			output: { id: string; sku: string };
			create: { sku: string };
			update: { sku?: string };
			where: Record<never, never>;
			select: Record<never, never>;
			populate: Record<never, never>;
		};
	};
	globals: {
		settings: {
			versions: false;
			output: { id: string };
			update: Record<never, never>;
			select: Record<never, never>;
			populate: Record<never, never>;
		};
	};
}

describe("advisory validation transport", () => {
	it("sends raw incomplete input through normal scoped transport and cancellation", async () => {
		let captured: Request | undefined;
		const abort = new AbortController();
		const client = createClient<Config>({
			baseURL: "https://cms.example.test",
			headers: { "x-app": "catalog" },
			fetch: async (request) => {
				captured = request as Request;
				return Response.json({
					evaluations: [
						{
							path: "sku",
							target: "stable-token",
							status: "checked",
							issues: [
								{
									code: "supplier_sku",
									path: "sku",
									message: "Use supplier prefix",
									target: "stable-token",
									locale: "fr",
								},
							],
						},
					],
				});
			},
		});
		const input = { id: "p1", data: { sku: "A-1", supplier: null }, fields: ["sku"] };
		const result = await client.collectionLiveValidation("products", input, {
			locale: "fr",
			signal: abort.signal,
		});
		expect(captured?.url).toBe(
			"https://cms.example.test/api/collections/products/validate?locale=fr"
		);
		expect(captured?.method).toBe("POST");
		expect(captured?.signal).toBe(abort.signal);
		expect(captured?.headers.get("x-app")).toBe("catalog");
		expect(await captured?.json()).toEqual(input);
		expect(result.evaluations[0]?.issues[0]?.code).toBe("supplier_sku");
		await client.globalLiveValidation("settings", { data: {}, fields: ["name"] });
		expect(captured?.url).toBe("https://cms.example.test/api/globals/settings/validate");
	});
	it("retains skipped checks and validates successful envelopes at runtime", async () => {
		let payload: unknown = { evaluations: [{ path: "sku", status: "skipped", issues: [] }] };
		const client = createClient<Config>({
			baseURL: "https://cms.example.test",
			fetch: async () => Response.json(payload),
		});
		expect(
			(await client.collectionLiveValidation("products", { data: { sku: {} }, fields: ["sku"] }))
				.evaluations[0]?.status
		).toBe("skipped");
		for (const malformed of [
			{},
			{ evaluations: [{ path: "sku", status: "ok", issues: [] }] },
			{ evaluations: [{ path: "sku", status: "checked", issues: [{ message: "unstructured" }] }] },
			{
				evaluations: [
					{
						path: "sku",
						status: "skipped",
						issues: [{ code: "invalid", path: "sku", message: "bad" }],
					},
				],
			},
		]) {
			payload = malformed;
			await expect(
				client.collectionLiveValidation("products", { data: {}, fields: ["sku"] })
			).rejects.toBeInstanceOf(RiduError);
		}
	});
	it("does not convert operational failure into successful validation", async () => {
		const client = createClient<Config>({
			baseURL: "https://cms.example.test",
			fetch: async () =>
				Response.json(
					{
						error: {
							code: "internal",
							status: 500,
							message: "internal server error",
							issues: [],
						},
					},
					{ status: 500 }
				),
		});
		await expect(
			client.collectionLiveValidation("products", { data: {}, fields: ["sku"] })
		).rejects.toMatchObject({ code: "internal", status: 500 });
	});
});
