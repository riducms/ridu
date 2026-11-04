import { describe, expect, it, mock } from "bun:test";
import { RiduError } from "@riducms/sdk";

// Registry tests keep the page inert; the consent requests are tested as plain functions below.
mock.module("../src/lib/consent-route.svelte", () => ({ default: () => ({}) }));
const { oauthAdminPlugin } = await import("../src/lib");
const { checkConsent, decodeAuthorization, decodeRedirect, sendDecision } =
	await import("../src/lib/consent");

const request = {
	client: { id: "client-1", name: "Claude" },
	redirectHost: "claude.ai",
	loopback: false,
	actAs: [
		{ id: "ada", label: "Ada" },
		{ id: "agent", label: "Claude agent" },
	],
};

function fakeClient(answers: Record<string, unknown>) {
	const calls: { path: string; body: unknown }[] = [];
	return {
		calls,
		client: {
			async requestPlugin<Result>(plugin: string, path: string, body: unknown): Promise<Result> {
				expect(plugin).toBe("oauth");
				calls.push({ path, body });
				const answer = answers[path];
				if (answer instanceof Error) throw answer;
				return answer as Result;
			},
		},
	};
}

describe("OAuth admin contract", () => {
	it("pairs the consent route with the Go plugin", () => {
		expect(oauthAdminPlugin.key).toBe("oauth");
		expect(oauthAdminPlugin.pairingVersion).toBe(1);
		const [route] = oauthAdminPlugin.routes;
		expect(route?.path).toBe("oauth/authorize");
		// The consent page is reached only from a client's link, never from navigation.
		expect(route !== undefined && "navigation" in route).toBe(false);
	});

	it("decodes requests and refusals and rejects anything else", () => {
		expect(decodeAuthorization(request)).toEqual(request);
		expect(
			decodeAuthorization({
				error: "invalid_request",
				errorDescription: "PKCE is required.",
				redirect: "https://claude.ai/cb?error=invalid_request",
			})
		).toEqual({
			error: "invalid_request",
			errorDescription: "PKCE is required.",
			redirect: "https://claude.ai/cb?error=invalid_request",
		});
		for (const value of [null, {}, { ...request, actAs: [] }, { ...request, loopback: "no" }])
			expect(() => decodeAuthorization(value)).toThrow();
		expect(decodeRedirect({ redirect: "http://127.0.0.1:33418/cb?code=x" })).toBe(
			"http://127.0.0.1:33418/cb?code=x"
		);
		for (const value of [{ redirect: "javascript:alert(1)" }, { redirect: 1 }, null])
			expect(() => decodeRedirect(value)).toThrow();
	});
});

describe("consent requests", () => {
	it("checks the request, then sends the chosen user and returns the app's address", async () => {
		const { client, calls } = fakeClient({
			authorization: request,
			decision: { redirect: "https://claude.ai/cb?code=abc&state=s" },
		});
		expect(await checkConsent(client, "?client_id=client-1")).toEqual({ kind: "ready", request });
		expect(await sendDecision(client, "?client_id=client-1", true, "agent")).toBe(
			"https://claude.ai/cb?code=abc&state=s"
		);
		expect(calls).toEqual([
			{ path: "authorization", body: { query: "?client_id=client-1" } },
			{ path: "decision", body: { query: "?client_id=client-1", allow: true, actAs: "agent" } },
		]);
	});

	it("shows refusals, with a way back to the app only when the app may hear why", async () => {
		const refused = fakeClient({
			authorization: {
				error: "invalid_request",
				errorDescription: "PKCE is required.",
				redirect: "https://claude.ai/cb?error=x",
			},
		});
		expect(await checkConsent(refused.client, "?client_id=c")).toEqual({
			kind: "refused",
			message: "PKCE is required.",
			redirect: "https://claude.ai/cb?error=x",
		});
		const forged = fakeClient({
			authorization: new RiduError({
				code: "bad_request",
				status: 400,
				message: "This app isn't registered.",
				issues: [],
			}),
		});
		expect(await checkConsent(forged.client, "?client_id=missing")).toEqual({
			kind: "refused",
			message: "This app isn't registered.",
		});
		const offline = fakeClient({ authorization: new Error("offline") });
		expect(await checkConsent(offline.client, "?client_id=c")).toEqual({
			kind: "failed",
			message: "offline",
		});
	});

	it("refuses a decision without a web address to return to", async () => {
		const { client } = fakeClient({ decision: { redirect: "javascript:alert(1)" } });
		await expect(sendDecision(client, "?client_id=c", true, "ada")).rejects.toThrow();
	});
});
