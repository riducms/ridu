import { createMemoryRouter } from "@hvniel/svelte-router";
import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import {
	SCHEMA_MANIFEST_VERSION,
	type AuthActionEnvelope,
	type SchemaCollection,
} from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";
import { tick } from "svelte";
import { expect, it } from "vitest";
import { render } from "vitest-browser-svelte";

import type { AdminClient } from "@admin/core/api/admin-client";
import { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import ResetPasswordRoute from "@admin/features/auth/reset-password-route.svelte";
import VerifyEmailRoute from "@admin/features/auth/verify-email-route.svelte";

const Provider = svelte`
	<script>
		import { RouterProvider } from "@hvniel/svelte-router";
		import { setAdminRuntime } from "../../src/core/runtime/admin-runtime.svelte";
		let { router, runtime } = $props();
		setAdminRuntime(runtime);
	</script>
	<RouterProvider {router} />
`;
const Away = svelte`
	<button>Another page</button>
	<input id="reset-password" aria-label="Unrelated password" />
`;
const users: SchemaCollection = {
	id: "users",
	slug: "users",
	labels: { singular: "User", plural: "Users" },
	admin: {},
	capabilities: { auth: true, upload: false, versions: false, trash: false, locking: false },
	fields: [],
};
const routes = {
	verify: {
		path: "/verify-email",
		Component: VerifyEmailRoute,
		submit: "Verify email",
		pending: "Verifying…",
		complete: "Your email address has been verified.",
	},
	reset: {
		path: "/reset-password",
		Component: ResetPasswordRoute,
		submit: "Reset password",
		pending: "Resetting password…",
		complete: "Your password has been reset.",
	},
} as const;
type AuthRoute = keyof typeof routes;

async function tokenRoute(kind: AuthRoute) {
	const requests: {
		token: string;
		password?: string;
		signal?: AbortSignal;
		response: ReturnType<typeof Promise.withResolvers<AuthActionEnvelope>>;
	}[] = [];
	function action(token: string, signal?: AbortSignal, password?: string) {
		const response = Promise.withResolvers<AuthActionEnvelope>();
		requests.push({ token, password, signal, response });
		// Deliberately ignore abort to prove the route also rejects stale completions.
		return response.promise;
	}
	const runtime = new AdminRuntime({
		auth: {
			verifyEmail: (input: { token: string }, options?: { signal?: AbortSignal }) =>
				action(input.token, options?.signal),
			resetPassword: (
				input: { token: string; password: string },
				options?: { signal?: AbortSignal }
			) => action(input.token, options?.signal, input.password),
		},
	} as unknown as AdminClient);
	runtime.manifest = {
		version: SCHEMA_MANIFEST_VERSION,
		application: {
			name: "Auth token lifetime",
			admin: { userCollectionId: users.id, userCollectionSlug: users.slug },
		},
		collections: [users],
		plugins: [],
	};
	const route = routes[kind];
	const router = createMemoryRouter(
		[
			{ path: route.path, Component: route.Component },
			{ path: "/away", Component: Away },
		],
		{ initialEntries: [`${route.path}?token=A`] }
	);
	const screen = await render(Provider, { router, runtime });
	return {
		screen,
		router,
		requests,
		async submit(password = "First-password-123") {
			if (kind === "reset") {
				await screen.getByLabelText("New password", { exact: true }).fill(password);
				await screen.getByLabelText("Confirm password", { exact: true }).fill(password);
			}
			await screen.getByRole("button", { name: route.submit, exact: true }).click();
		},
		async dispose() {
			await screen.unmount();
			router.dispose();
			runtime.dispose();
			for (const request of requests) request.response.resolve({ success: true });
		},
	};
}

function lateFailure() {
	return new RiduError({
		code: "validation",
		status: 422,
		message: "Previous token failed",
		issues: [{ code: "invalid", path: "password", message: "Previous password failed" }],
	});
}

for (const kind of ["verify", "reset"] as const) {
	const route = routes[kind];
	it.each(["success", "failure"] as const)(
		`keeps the current ${kind} token pending when the previous token completes with %s`,
		async (outcome) => {
			const fixture = await tokenRoute(kind);
			try {
				await fixture.submit();
				const first = fixture.requests[0]!;
				await fixture.router.navigate(`${route.path}?token=B`);
				expect(first.signal?.aborted).toBe(true);
				if (kind === "reset") {
					await expect
						.element(fixture.screen.getByLabelText("New password", { exact: true }))
						.toHaveValue("");
					await expect
						.element(fixture.screen.getByLabelText("Confirm password", { exact: true }))
						.toHaveValue("");
				}
				await fixture.submit("Second-password-456");
				const second = fixture.requests[1]!;
				expect(second.token).toBe("B");
				if (outcome === "success") first.response.resolve({ success: true });
				else first.response.reject(lateFailure());
				await first.response.promise.catch(() => undefined);
				await tick();

				await expect
					.element(fixture.screen.getByRole("button", { name: route.pending, exact: true }))
					.toBeDisabled();
				await expect
					.element(fixture.screen.getByText(route.complete, { exact: true }))
					.not.toBeInTheDocument();
				await expect
					.element(fixture.screen.getByText(/Previous (token|password) failed/))
					.not.toBeInTheDocument();
				expect(second.signal?.aborted).toBe(false);
				if (kind === "reset")
					await expect
						.element(fixture.screen.getByLabelText("New password", { exact: true }))
						.toHaveValue("Second-password-456");
				second.response.resolve({ success: true });
				await expect
					.element(fixture.screen.getByText(route.complete, { exact: true }))
					.toBeVisible();
			} finally {
				await fixture.dispose();
			}
		}
	);

	it(`resets a completed ${kind} screen when the retained route receives another token`, async () => {
		const fixture = await tokenRoute(kind);
		try {
			await fixture.submit();
			fixture.requests[0]!.response.resolve({ success: true });
			await expect.element(fixture.screen.getByText(route.complete, { exact: true })).toBeVisible();
			await fixture.router.navigate(`${route.path}?token=B`);
			await expect
				.element(fixture.screen.getByText(route.complete, { exact: true }))
				.not.toBeInTheDocument();
			await fixture.submit("Second-password-456");
			expect(fixture.requests.map((request) => request.token)).toEqual(["A", "B"]);
		} finally {
			await fixture.dispose();
		}
	});

	it(`preserves ${kind} work through unrelated query changes for the same token`, async () => {
		const fixture = await tokenRoute(kind);
		try {
			await fixture.submit();
			const request = fixture.requests[0]!;
			await fixture.router.navigate(`${route.path}?source=email&token=A`);
			expect(request.signal?.aborted).toBe(false);
			await expect
				.element(fixture.screen.getByRole("button", { name: route.pending, exact: true }))
				.toBeDisabled();
			request.response.resolve({ success: true });
			await expect.element(fixture.screen.getByText(route.complete, { exact: true })).toBeVisible();
			await fixture.router.navigate(`${route.path}?token=A&source=other`);
			await expect.element(fixture.screen.getByText(route.complete, { exact: true })).toBeVisible();
			expect(fixture.requests).toHaveLength(1);
		} finally {
			await fixture.dispose();
		}
	});

	it(`aborts ${kind} work on unmount and ignores its late failure without stealing focus`, async () => {
		const fixture = await tokenRoute(kind);
		try {
			await fixture.submit();
			const request = fixture.requests[0]!;
			await fixture.router.navigate("/away");
			expect(request.signal?.aborted).toBe(true);
			await fixture.screen.getByRole("button", { name: "Another page" }).click();
			request.response.reject(lateFailure());
			await request.response.promise.catch(() => undefined);
			await tick();
			await expect
				.element(fixture.screen.getByRole("button", { name: "Another page" }))
				.toHaveFocus();
			await expect
				.element(fixture.screen.getByText(/Previous (token|password) failed/))
				.not.toBeInTheDocument();
		} finally {
			await fixture.dispose();
		}
	});
}
