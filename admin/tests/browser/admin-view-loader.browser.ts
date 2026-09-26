import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { createAdminLoader, RiduError } from "@riducms/sdk";
import type { AdminViewLoader } from "@admin/core/loaders/admin-view-loader.svelte";

const loader = createAdminLoader<{}, { count: number }>({
	key: "counts",
	input: { kind: "object" },
	output: { kind: "object", fields: { count: { kind: "number" } } },
});
const Owner = svelte`
 <script>
  import { AdminViewLoader } from '../../src/core/loaders/admin-view-loader.svelte';
  let {client, loader, route, prepared, capture} = $props();
  const controller = new AdminViewLoader({client, loader, get route(){return route}, get prepared(){return prepared}});
  capture(controller);
 </script>
 {#if controller.ready}<p>{controller.data.count}</p>{:else}<span data-ridu-loading-surface="test"></span>{/if}
 {#if controller.error}<p role="alert">{controller.error}</p>{/if}
`;

it("starts ready from a seed and adopts retained route data without initial reads", async () => {
	const adminLoad = vi.fn(async () => ({ count: 3 }));
	let controller!: AdminViewLoader;
	const props = {
		client: { adminLoad },
		loader,
		route: "/collections/posts/one?q=same",
		prepared: { value: { count: 1 } },
		capture: (value: AdminViewLoader) => {
			controller = value;
		},
	};
	const screen = await render(Owner, props);
	expect(controller.ready).toBe(true);
	expect(adminLoad).not.toHaveBeenCalled();
	await screen.rerender({
		...props,
		route: "/collections/posts/two?q=same",
		prepared: { value: { count: 2 } },
	});
	expect(controller.data).toEqual({ count: 2 });
	expect(adminLoad).not.toHaveBeenCalled();
	await controller.refresh();
	expect(controller.data).toEqual({ count: 3 });
	expect(adminLoad).toHaveBeenCalledTimes(1);
});

it("keeps committed data during refresh and ignores an aborted result after route navigation", async () => {
	let release!: (data: unknown) => void;
	let signal!: AbortSignal;
	const adminLoad = vi.fn((_key: string, _query: string, options: { signal: AbortSignal }) => {
		signal = options.signal;
		return new Promise<unknown>((resolve) => {
			release = resolve;
		});
	});
	let controller!: AdminViewLoader;
	const props = {
		client: { adminLoad },
		loader,
		route: "?q=one",
		prepared: { value: { count: 1 } },
		capture: (value: AdminViewLoader) => {
			controller = value;
		},
	};
	const screen = await render(Owner, props);
	const pending = controller.refresh();
	expect(controller.ready).toBe(true);
	expect(controller.refreshing).toBe(true);
	await screen.rerender({ ...props, route: "?q=two", prepared: { value: { count: 2 } } });
	expect(signal.aborted).toBe(true);
	release({ count: 99 });
	await pending;
	expect(controller.data).toEqual({ count: 2 });
	expect(controller.refreshing).toBe(false);
});

it("retries errors and cancels owned reads on unmount", async () => {
	const failure = new RiduError({
		code: "access_denied",
		status: 403,
		message: "No dashboard access",
		issues: [],
	});
	let rejectInitial!: (cause: unknown) => void;
	const initial = new Promise<unknown>((_resolve, reject) => {
		rejectInitial = reject;
	});
	const adminLoad = vi.fn().mockReturnValueOnce(initial).mockResolvedValueOnce({ count: 4 });
	let controller!: AdminViewLoader;
	const screen = await render(Owner, {
		client: { adminLoad },
		loader,
		route: "",
		prepared: undefined,
		capture: (value: AdminViewLoader) => {
			controller = value;
		},
	});
	expect(controller.settled).toBe(false);
	rejectInitial(failure);
	await expect.poll(() => controller.error).toBe("No dashboard access");
	expect(controller.settled).toBe(true);
	await controller.refresh();
	expect(controller.data).toEqual({ count: 4 });
	expect(controller.settled).toBe(true);
	let release!: (data: unknown) => void;
	adminLoad.mockImplementationOnce(
		() =>
			new Promise((resolve) => {
				release = resolve;
			})
	);
	const pending = controller.refresh();
	const signal = adminLoad.mock.calls.at(-1)![2].signal as AbortSignal;
	await screen.unmount();
	expect(signal.aborted).toBe(true);
	release({ count: 5 });
	await pending;
	expect(controller.data).toEqual({ count: 4 });
});
