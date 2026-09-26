import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { expect, it, vi } from "vitest";
import { render } from "vitest-browser-svelte";

const NavigationPreference = svelte`
	<script>
		import { createNavigationPreference } from "../../src/features/navigation/navigation-preference.svelte";
		let { client, session, preparedPreferences = {} } = $props();
		const preference = createNavigationPreference({
			client,
			preparedPreferences,
			get session() { return session; },
		});
	</script>
	<button onclick={preference.toggle} aria-expanded={preference.open}>Navigation</button>
`;

it("a late sidebar preference read cannot overwrite the author's toggle", async () => {
	const response = Promise.withResolvers<{ open: boolean }>();
	const preference = vi.fn(() => response.promise);
	const setPreference = vi.fn(async () => undefined);
	const screen = await render(NavigationPreference, {
		client: { preference, setPreference },
		session: { collection: "users", user: { id: "sidebar-toggle" } },
	});
	try {
		await expect.poll(() => preference.mock.calls.length).toBe(1);
		const trigger = screen.getByRole("button", { name: "Navigation" });
		await trigger.click();
		await expect.element(trigger).toHaveAttribute("aria-expanded", "false");
		await expect.poll(() => setPreference.mock.calls.length).toBe(1);
		expect(setPreference).toHaveBeenCalledWith("navigation", { open: false });
		response.resolve({ open: true });
		await response.promise;
		await expect.element(trigger).toHaveAttribute("aria-expanded", "false");
	} finally {
		await screen.unmount();
	}
});

it("a same-actor session replacement cannot reload an older preference over a pending toggle", async () => {
	const write = Promise.withResolvers<{ open: boolean }>();
	const client = {
		preference: vi.fn(async () => ({ open: true })),
		setPreference: vi.fn(() => write.promise),
	};
	const screen = await render(NavigationPreference, {
		client,
		session: {
			id: "initial-session",
			collection: "users",
			user: { id: "sidebar-same-actor", name: "Original profile" },
		},
		preparedPreferences: { navigation: { open: true } },
	});
	const trigger = screen.getByRole("button", { name: "Navigation" });
	try {
		await expect.element(trigger).toHaveAttribute("aria-expanded", "true");
		expect(client.preference).not.toHaveBeenCalled();
		await trigger.click();
		await expect.element(trigger).toHaveAttribute("aria-expanded", "false");
		await expect.poll(() => client.setPreference.mock.calls.length).toBe(1);
		expect(client.setPreference).toHaveBeenCalledWith("navigation", { open: false });

		await screen.rerender({
			session: {
				id: "refreshed-session",
				collection: "users",
				user: { id: "sidebar-same-actor", name: "Updated profile" },
			},
		});
		expect(client.preference).not.toHaveBeenCalled();
		await expect.element(trigger).toHaveAttribute("aria-expanded", "false");

		write.resolve({ open: false });
		await write.promise;
		await expect.element(trigger).toHaveAttribute("aria-expanded", "false");
	} finally {
		write.resolve({ open: false });
		await write.promise;
		await screen.unmount();
	}
});

it("sidebar seeds and pending reads stay scoped to their actor and shell lifetime", async () => {
	const responses = [
		Promise.withResolvers<{ open: boolean }>(),
		Promise.withResolvers<{ open: boolean }>(),
	];
	const signals: AbortSignal[] = [];
	const client = {
		preference: vi.fn((_key: string, { signal }: { signal: AbortSignal }) => {
			signals.push(signal);
			return responses[signals.length - 1]!.promise;
		}),
		setPreference: vi.fn(async () => undefined),
	};
	const session = (id: string) => ({ collection: "users", user: { id } });
	const screen = await render(NavigationPreference, {
		client,
		session: session("sidebar-initial"),
		preparedPreferences: { navigation: { open: false } },
	});
	const trigger = screen.getByRole("button", { name: "Navigation" });
	try {
		await expect.element(trigger).toHaveAttribute("aria-expanded", "false");
		expect(client.preference).not.toHaveBeenCalled();
		await screen.rerender({ session: session("sidebar-second") });
		await expect.poll(() => client.preference.mock.calls.length).toBe(1);
		await screen.rerender({ session: session("sidebar-third") });
		await expect.poll(() => client.preference.mock.calls.length).toBe(2);
		expect(signals[0]?.aborted).toBe(true);
		responses[0]!.resolve({ open: true });
		await responses[0]!.promise;
		await expect.element(trigger).toHaveAttribute("aria-expanded", "false");
	} finally {
		await screen.unmount();
	}
	expect(signals[1]?.aborted).toBe(true);
	responses[1]!.resolve({ open: true });
});
