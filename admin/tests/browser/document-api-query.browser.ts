import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { SCHEMA_MANIFEST_VERSION } from "@riducms/protocol";
import type { AdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const Viewer = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { TooltipProvider } from "@riducms/ui";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import DocumentAPIView from "../../src/features/documents/document-api-view.svelte";
		let { runtime } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	<TooltipProvider>
		<DocumentAPIView resourceSlug="posts" documentID="one"
			contentLocale="en" fallbackValue={{}} prepared={{ value: { id: "one", title: "Editor draft" } }} />
	</TooltipProvider>
`;

function createRuntime() {
	const runtime = new AdminRuntime({} as AdminClient);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: {
			name: "Test",
			admin: { userCollectionId: "users", userCollectionSlug: "users" },
			localization: {
				defaultLocale: "en",
				fallback: true,
				locales: [
					{ code: "en", label: "English" },
					{ code: "fr", label: "French" },
				],
			},
		},
		collections: [],
		plugins: [],
	};
	runtime.contentLocale = "en";
	return runtime;
}

it("queries locale, depth and anonymous responses without altering the editor locale", async () => {
	const fetch = vi.spyOn(window, "fetch").mockImplementation(async (_url, init) => {
		if (init?.credentials === "omit")
			return new Response(
				JSON.stringify({
					error: {
						code: "access_denied",
						status: 403,
						message: "Read denied",
						issues: [],
					},
				}),
				{ status: 403 }
			);
		return new Response(
			JSON.stringify({ doc: { title: "API result", author: { name: "Author" } } })
		);
	});
	const writeText = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
	const runtime = createRuntime();
	const screen = await render(Viewer, { runtime });
	const latestURL = () => new URL(String(fetch.mock.lastCall?.[0]));
	try {
		await expect.element(screen.getByText('"Editor draft"', { exact: true })).toBeVisible();
		expect(fetch).not.toHaveBeenCalled();
		await screen.getByRole("button", { name: "Copy URL" }).click();
		await expect.element(screen.getByRole("status").filter({ hasText: "Copied" })).toBeVisible();
		await screen.getByRole("button", { name: "Run request" }).click();
		await expect.element(screen.getByText('"API result"', { exact: true })).toBeVisible();

		await screen.getByRole("spinbutton", { name: "Depth" }).fill("0");
		await expect.poll(() => latestURL().searchParams.get("depth")).toBe("0");
		await screen.getByRole("button", { name: "Locale", exact: true }).click();
		await screen.getByRole("option", { name: "French", exact: true }).click();
		await expect.poll(() => latestURL().searchParams.get("locale")).toBe("fr");
		expect(runtime.contentLocale).toBe("en");

		await screen.getByRole("button", { name: "author", exact: true }).click();
		const requests = fetch.mock.calls.length;
		await screen.getByRole("button", { name: "Expand JSON view" }).click();
		await expect.element(screen.getByLabelText("Depth", { exact: true })).not.toBeVisible();
		await expect
			.element(screen.getByRole("button", { name: "author", exact: true }))
			.toHaveAttribute("aria-expanded", "false");
		await screen.getByRole("button", { name: "Exit expanded JSON view" }).click();
		expect(fetch.mock.calls).toHaveLength(requests);

		await screen.getByRole("checkbox", { name: "Authenticated", exact: true }).click();
		await expect
			.element(screen.getByRole("alert").filter({ hasText: "Read denied" }))
			.toBeVisible();
		await expect.element(screen.getByText('"Read denied"', { exact: true })).toBeVisible();
	} finally {
		await screen.unmount();
		runtime.dispose();
		fetch.mockRestore();
		writeText.mockRestore();
	}
});

it.each([{ message: "Spoofed" }, { error: { message: "Spoofed" } }])(
	"falls back to HTTP status for a non-canonical error body",
	async (body) => {
		const fetch = vi
			.spyOn(window, "fetch")
			.mockResolvedValue(
				new Response(JSON.stringify(body), { status: 502, statusText: "Bad Gateway" })
			);
		const runtime = createRuntime();
		const screen = await render(Viewer, { runtime });
		try {
			await screen.getByRole("button", { name: "Run request" }).click();
			const alert = screen.getByRole("alert");
			await expect.element(alert).toHaveTextContent("502 Bad Gateway");
			await expect.element(alert).not.toHaveTextContent("Spoofed");
		} finally {
			await screen.unmount();
			runtime.dispose();
			fetch.mockRestore();
		}
	}
);

it("aborts and ignores a superseded request on the retained route", async () => {
	const first = Promise.withResolvers<Response>();
	const second = Promise.withResolvers<Response>();
	let firstSignal: AbortSignal | null | undefined;
	const fetch = vi
		.spyOn(window, "fetch")
		.mockImplementationOnce((_url, init) => {
			firstSignal = init?.signal;
			return first.promise;
		})
		.mockImplementationOnce(() => second.promise);
	const runtime = createRuntime();
	const screen = await render(Viewer, { runtime });
	try {
		await screen.getByRole("button", { name: "Run request" }).click();
		await expect.poll(() => fetch).toHaveBeenCalledOnce();
		await screen.getByRole("spinbutton", { name: "Depth" }).fill("1");
		await expect.poll(() => fetch).toHaveBeenCalledTimes(2);
		expect(firstSignal?.aborted).toBe(true);

		second.resolve(new Response(JSON.stringify({ doc: { title: "Latest response" } })));
		await expect.element(screen.getByText('"Latest response"', { exact: true })).toBeVisible();

		first.resolve(new Response(JSON.stringify({ doc: { title: "Stale response" } })));
		await first.promise;
		await expect
			.element(screen.getByText('"Stale response"', { exact: true }))
			.not.toBeInTheDocument();
		await expect.element(screen.getByText('"Latest response"', { exact: true })).toBeVisible();
	} finally {
		first.resolve(new Response());
		second.resolve(new Response());
		await screen.unmount();
		runtime.dispose();
		fetch.mockRestore();
	}
});
