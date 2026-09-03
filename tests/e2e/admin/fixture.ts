import { expect, test as base } from "@playwright/test";

export { expect };
export type { Page } from "@playwright/test";

export const test = base.extend<{ resetAdminFixture: void }>({
	resetAdminFixture: [
		async ({ request }, use) => {
			const resetToken = process.env.RIDU_BROWSER_RESET_TOKEN;
			if (!resetToken) {
				throw new Error("RIDU_BROWSER_RESET_TOKEN is required for the admin fixture reset");
			}
			const response = await request.post("/__ridu-test/reset", {
				headers: { "X-Ridu-Test-Reset-Token": resetToken },
			});
			if (!response.ok()) {
				throw new Error(`admin fixture reset failed with ${response.status()}`);
			}
			await use();
		},
		{ auto: true },
	],
});
