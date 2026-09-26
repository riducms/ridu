import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { tick } from "svelte";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import type { AdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const Harness = svelte`
	<script>
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		import DevelopmentTools from "../../src/features/development/development-tools.svelte";
		let { runtime } = $props();
		setAdminRuntime(runtime);
	</script>
	<DevelopmentTools />
`;

it("cancels a schema update notice when the tools unmount", async () => {
	const runtime = new AdminRuntime({} as AdminClient);
	const schedule = vi.spyOn(window, "setTimeout");
	const cancel = vi.spyOn(window, "clearTimeout");
	const screen = await render(Harness, { runtime });
	try {
		runtime.refreshingManifest = true;
		await tick();
		runtime.manifestRefreshError = "Refresh failed";
		runtime.refreshingManifest = false;
		await expect.element(screen.getByRole("alert")).toBeVisible();

		const timer =
			schedule.mock.results[schedule.mock.calls.findIndex(([, delay]) => delay === 7_000)]?.value;
		expect(timer).toBeDefined();
		await screen.unmount();
		expect(cancel).toHaveBeenCalledWith(timer);
	} finally {
		await screen.unmount();
		runtime.dispose();
		schedule.mockRestore();
		cancel.mockRestore();
	}
});
