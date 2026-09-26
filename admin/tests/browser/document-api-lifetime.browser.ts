import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
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
		let { runtime, documentID = "one" } = $props();
		setAdminRuntime(runtime);
		setAdminI18n(runtime.i18n);
	</script>
	<TooltipProvider>
		<DocumentAPIView resourceSlug="posts" {documentID}
			fallbackValue={{}} prepared={{ value: { id: "one", title: "Prepared title" } }} />
	</TooltipProvider>
`;

it("does not start clipboard feedback after the API viewer unmounts", async () => {
	const write = Promise.withResolvers<void>();
	const writeText = vi.spyOn(navigator.clipboard, "writeText").mockReturnValue(write.promise);
	const runtime = new AdminRuntime({} as AdminClient);
	const screen = await render(Viewer, { runtime });
	try {
		await screen.getByRole("button", { name: "Copy URL", exact: true }).click();
		expect(writeText).toHaveBeenCalledOnce();
		await screen.unmount();

		const setTimeout = vi.spyOn(window, "setTimeout");
		try {
			write.resolve();
			await write.promise;
			expect(setTimeout).not.toHaveBeenCalled();
		} finally {
			setTimeout.mockRestore();
		}
	} finally {
		await screen.unmount();
		runtime.dispose();
		writeText.mockRestore();
	}
});

it("does not attribute a pending URL copy to a different document", async () => {
	const write = Promise.withResolvers<void>();
	const writeText = vi.spyOn(navigator.clipboard, "writeText").mockReturnValue(write.promise);
	const runtime = new AdminRuntime({} as AdminClient);
	const screen = await render(Viewer, { runtime });
	try {
		await screen.getByRole("button", { name: "Copy URL", exact: true }).click();
		await screen.rerender({ runtime, documentID: "two" });
		write.resolve();
		await write.promise;
		await expect
			.element(screen.getByRole("button", { name: "Copy URL", exact: true }))
			.toHaveAttribute("title", "Copy URL");
		expect(writeText.mock.lastCall?.[0]).toContain("/posts/one?");
	} finally {
		await screen.unmount();
		runtime.dispose();
		writeText.mockRestore();
	}
});
