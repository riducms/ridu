import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import type { AdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const Harness = svelte`
 <script>
  import { setAdminI18n } from "@riducms/plugin";
  import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
  import LivePreviewPanel from "../../src/features/documents/live-preview-panel.svelte";
  let { runtime } = $props();
  setAdminRuntime(runtime);
  setAdminI18n(runtime.i18n);
 </script>
 <LivePreviewPanel collection="posts" documentID="one" resource="collection" values={{}}
  preview={{ url: "/preview", breakpoints: [
   { name: "custom", label: "Named custom", width: 600, height: 800 },
   { name: "responsive", label: "Named responsive", width: 900, height: 1100 }
  ] }} />
`;

it("keeps configured breakpoint names separate from responsive and manually sized modes", async () => {
	const client = {
		createPreviewToken: vi.fn(() => new Promise(() => {})),
	} as unknown as AdminClient;
	const screen = await render(Harness, { runtime: new AdminRuntime(client) });
	const viewport = screen.getByRole("button", { name: "Preview viewport", exact: true });
	const width = screen.getByRole("spinbutton", { name: "Preview width" });
	const height = screen.getByRole("spinbutton", { name: "Preview height" });

	await viewport.click();
	await screen.getByRole("option", { name: "Named custom", exact: true }).click();
	await expect.element(width).toHaveValue(600);
	await width.fill("420");
	await expect.element(width).toHaveValue(420);
	await expect.element(height).toHaveValue(800);
	await expect.element(viewport).toHaveTextContent("Custom");

	await viewport.click();
	await screen.getByRole("option", { name: "Named responsive", exact: true }).click();
	await expect.element(width).toHaveValue(900);
	await expect.element(height).toHaveValue(1100);
	await height.fill("700");
	await expect.element(width).toHaveValue(900);
	await expect.element(height).toHaveValue(700);
	await expect.element(viewport).toHaveTextContent("Custom");
	await screen.unmount();
});
