import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { createMemoryRouter } from "@hvniel/svelte-router";
import AdminRouteError from "@admin/app/admin-route-error.svelte";

const Provider = svelte`
	<script>
		import { RouterProvider } from "@hvniel/svelte-router";
		import { setAdminI18n } from "@riducms/plugin";
		import { createAdminI18n } from "@riducms/translations";
		let { router } = $props();
		setAdminI18n(createAdminI18n());
	</script>
	<RouterProvider {router} />
`;
const Descendants = svelte`
	<script>
		import { Route, Routes } from "@hvniel/svelte-router";
	</script>
	<Routes>
		<Route path="broken">
			{#snippet element()}{JSON.parse("broken-route")}{/snippet}
		</Route>
		<Route index>{#snippet element()}<h1>Recovered dashboard</h1>{/snippet}</Route>
	</Routes>
`;

it("contains a declarative descendant failure and recovers through the admin dashboard action", async () => {
	const diagnostic = vi.spyOn(console, "error").mockImplementation(() => {});
	const router = createMemoryRouter(
		[{ path: "*", Component: Descendants, ErrorBoundary: AdminRouteError }],
		{ basename: "/console", initialEntries: ["/console/broken"] }
	);
	const screen = await render(Provider, { router });
	try {
		await expect
			.element(screen.getByRole("heading", { name: "Something went wrong." }))
			.toBeVisible();
		await expect.element(screen.getByRole("button", { name: "Reload page" })).toBeVisible();
		expect(screen.getByRole("alert").element().textContent).not.toContain("broken-route");
		const dashboard = screen.getByRole("link", { name: "Return to the dashboard" });
		await expect.element(dashboard).toHaveAttribute("href", "/console");
		await dashboard.click();
		await expect
			.element(screen.getByRole("heading", { name: "Recovered dashboard" }))
			.toBeVisible();
		expect(diagnostic).toHaveBeenCalledWith("Ridu admin route failed", expect.any(SyntaxError));
	} finally {
		await screen.unmount();
		router.dispose();
		diagnostic.mockRestore();
	}
});
