import { beforeEach, expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { createAdminLoader } from "@riducms/sdk";
import AdminViewHost from "@admin/core/loaders/admin-view-host.svelte";

const context = vi.hoisted(() => ({
	adminLoad: vi.fn(),
	register: vi.fn<(page: { ready: () => boolean }) => void>(),
}));
vi.mock("@admin/core/runtime/admin-runtime.svelte", () => ({
	getAdminRuntime: () => ({
		client: { adminLoad: context.adminLoad },
		i18n: { t: () => "Retry" },
	}),
}));
vi.mock("@admin/core/bootstrap/admin-bootstrap", () => ({
	getAdminBootstrapCoordinator: () => ({ loaderData: () => undefined }),
	normalizedURLIdentity: () => "/report",
}));
vi.mock("@hvniel/svelte-router", () => ({
	useLocation: () => ({ current: { pathname: "/report", search: "" } }),
}));
vi.mock("@admin/core/routing/admin-scroll.svelte", () => ({
	registerAdminScrollPage: context.register,
}));

const component = svelte`<p>Complete view</p>`;
const loader = createAdminLoader<{}, string>({
	key: "report",
	input: { kind: "object" },
	output: { kind: "string" },
});
beforeEach(() => vi.clearAllMocks());

it.each(["success", "error"])("route ownership waits for fallback %s", async (outcome) => {
	let resolve!: (data: unknown) => void;
	let reject!: (cause: unknown) => void;
	context.adminLoad.mockReturnValue(
		new Promise((yes, no) => {
			resolve = yes;
			reject = no;
		})
	);
	await render(AdminViewHost, {
		props: { view: { component, loader }, props: {}, ownsRoute: true },
	});
	expect(context.register).toHaveBeenCalledOnce();
	const page = context.register.mock.calls[0]![0];
	expect(page.ready()).toBe(false);
	if (outcome === "success") resolve("complete");
	else reject(new Error("failed read"));
	await expect.poll(page.ready).toBe(true);
});

it("dashboard panels do not become scroll owners", async () => {
	context.adminLoad.mockResolvedValue("complete");
	await render(AdminViewHost, { props: { view: { component, loader }, props: {} } });
	expect(context.register).not.toHaveBeenCalled();
});
