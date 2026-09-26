import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { AdminReadResultV1, AdminDocumentV1 } from "@riducms/protocol";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import type { AdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const Viewer = svelte`
	<script>
		import { setAdminI18n } from "@riducms/plugin";
		import { TooltipProvider } from "@riducms/ui";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import DocumentAPIView from "../../src/features/documents/document-api-view.svelte";
		let { runtime, id, locale, prepared } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	<TooltipProvider>
		<DocumentAPIView resourceSlug="posts" documentID={id}
			contentLocale={locale} fallbackValue={{}} {prepared} />
	</TooltipProvider>
`;

it("starts from one document result and ignores a superseded API request after a retained transition", async () => {
	const response = Promise.withResolvers<Response>();
	const fetch = vi.spyOn(window, "fetch").mockImplementation(() => response.promise);
	const runtime = new AdminRuntime({} as AdminClient);
	const prepared: AdminReadResultV1<AdminDocumentV1> = {
		value: { id: "one", title: "First prepared title" },
	};
	const props = { runtime, id: "one", locale: "en", prepared };
	const screen = await render(Viewer, props);
	try {
		await expect.element(screen.getByText('"First prepared title"', { exact: true })).toBeVisible();
		expect(fetch).not.toHaveBeenCalled();
		await screen.getByRole("button", { name: "Run request", exact: true }).click();
		expect(fetch).toHaveBeenCalledOnce();
		const signal = (fetch.mock.calls[0]?.[1] as RequestInit).signal;
		await screen.rerender({
			...props,
			id: "two",
			prepared: { value: { id: "two", title: "Second prepared title" } },
		});
		await expect
			.element(screen.getByText('"Second prepared title"', { exact: true }))
			.toBeVisible();
		expect(signal?.aborted).toBe(true);
		response.resolve(
			new Response(JSON.stringify({ doc: { id: "one", title: "Stale API title" } }), {
				status: 200,
			})
		);
		await new Promise((resolve) => window.setTimeout(resolve, 0));
		await expect
			.element(screen.getByText('"Second prepared title"', { exact: true }))
			.toBeVisible();
		expect(fetch).toHaveBeenCalledOnce();
	} finally {
		await screen.unmount();
		runtime.dispose();
		fetch.mockRestore();
	}
});
