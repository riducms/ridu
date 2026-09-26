import { expect, test } from "./fixture";

for (const theme of ["light", "dark"] as const) {
	test(
		theme === "light"
			? "auth controls preserve geometry, keyboard behavior, application CSS, and light palettes"
			: "auth controls apply dark palettes to normal, invalid, pending, and failure states",
		async ({ page }) => {
			const checksThemeIndependentBehavior = theme === "light";
			await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
			await page.setViewportSize({ width: 1280, height: 720 });
			await page.goto("/admin/login");
			await expect(page.getByText("Custom login wrapper")).toBeVisible();
			const email = page.getByLabel("Email address");
			const password = page.getByRole("textbox", { name: "Password", exact: true });
			const submit = page.getByRole("button", { name: "Sign in", exact: true });
			if (checksThemeIndependentBehavior) {
				await expect(email).toHaveCSS("height", "40px");
				await expect(email).toHaveCSS("border-radius", "3px");
				await expect(submit).toHaveCSS("height", "40px");
				await expect(page.locator(".ridu-auth__content")).toHaveCSS("width", "480px");
			}
			await expect(email).toHaveCSS(
				"background-color",
				theme === "light" ? "rgb(255, 255, 255)" : "rgb(34, 34, 34)"
			);
			const restingEmail = checksThemeIndependentBehavior ? await email.boundingBox() : undefined;
			await submit.click();
			const emailError = page.locator("#ridu-login-email-error");
			const passwordError = page.locator("#ridu-login-password-error");
			await expect(emailError).toHaveText("This field is required.");
			await expect(passwordError).toHaveText("This field is required.");
			if (checksThemeIndependentBehavior) {
				await expect(email).toBeFocused();
				await expect(email).toHaveAttribute("aria-errormessage", "ridu-login-email-error");
				await expect(email).toHaveAttribute("aria-invalid", "true");
				await expect(email).toHaveCSS("outline-style", "none");
			}
			await expect(email).toHaveCSS(
				"background-color",
				theme === "light" ? "rgb(250, 241, 240)" : "rgb(64, 32, 29)"
			);
			await expect(email).toHaveCSS(
				"border-top-color",
				theme === "light" ? "rgb(218, 75, 72)" : "rgb(182, 54, 54)"
			);
			await expect(emailError).toHaveCSS(
				"background-color",
				theme === "light" ? "rgb(253, 154, 146)" : "rgb(144, 44, 43)"
			);
			if (checksThemeIndependentBehavior) {
				const bubble = await emailError.boundingBox();
				expect(bubble!.y + bubble!.height).toBeCloseTo(restingEmail!.y - 6, 0);
				expect(await email.boundingBox()).toEqual(restingEmail);
				await email.fill("not-an-email");
				await expect(emailError).toHaveText("Enter a valid email address.");
				await submit.click();
				await expect(emailError).toHaveText("Enter a valid email address.");
				await page.setViewportSize({ width: 320, height: 640 });
				await expect(emailError).toBeVisible();
				expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320);
				await page.setViewportSize({ width: 1280, height: 720 });
			}

			await email.fill("editor@riducms.test");
			if (checksThemeIndependentBehavior) {
				await email.press("Tab");
				await expect(password).toBeFocused();
				await expect(password).toHaveCSS("outline-style", "none");
			} else {
				await password.focus();
			}
			await password.fill("wrong-password");
			if (checksThemeIndependentBehavior) {
				await expect(emailError).toHaveCount(0);
				await expect(passwordError).toHaveCount(0);
			}
			await password.hover();
			await expect(password).toHaveCSS(
				"border-top-color",
				theme === "light" ? "rgb(154, 154, 154)" : "rgb(141, 141, 141)"
			);
			if (checksThemeIndependentBehavior) {
				await password.press("Tab");
				const reveal = page.getByRole("button", { name: "Show password", exact: true });
				await expect(reveal).toBeFocused();
				await reveal.press("Space");
				await expect(password).toHaveAttribute("type", "text");
				await expect(password).toHaveValue("wrong-password");
				await page.getByRole("button", { name: "Hide password", exact: true }).press("Space");
				await expect(password).toHaveAttribute("type", "password");
			}

			let finishLogin = () => {};
			const waiting = new Promise<void>((resolve) => {
				finishLogin = resolve;
			});
			await page.route("**/api/auth/users/login", async (route) => {
				await waiting;
				await route.continue();
			});
			await submit.click();
			const pending = page.getByRole("button", { name: "Signing in…" });
			try {
				await expect(pending).toBeDisabled();
				await expect(pending).toHaveAttribute("aria-busy", "true");
				if (checksThemeIndependentBehavior) {
					await expect(password).toHaveValue("wrong-password");
					await expect(password).toBeDisabled();
					await expect(email).toBeDisabled();
					await expect(pending).toHaveCSS("opacity", "1");
				}
				await expect(pending).toHaveCSS(
					"background-color",
					theme === "light" ? "rgb(208, 208, 208)" : "rgb(74, 74, 74)"
				);
			} finally {
				finishLogin();
			}
			await expect(page.getByLabel("Notifications alt+T")).toContainText(
				"invalid email or password"
			);
			if (checksThemeIndependentBehavior) await expect(submit).toBeEnabled();
			const toast = page
				.locator("[data-sonner-toast]")
				.filter({ hasText: "invalid email or password" });
			if (checksThemeIndependentBehavior) {
				await expect(toast).toHaveCSS("border-radius", "4px");
				await expect(toast).toHaveCSS("padding", "16px");
			}
			await expect(toast).toHaveCSS(
				"background-color",
				theme === "light" ? "rgb(252, 229, 227)" : "rgb(64, 32, 29)"
			);
			if (checksThemeIndependentBehavior) {
				await expect(page.locator(".ridu-auth [role=alert]")).toHaveCount(0);

				await page.setViewportSize({ width: 320, height: 640 });
				await expect(email).toHaveCSS("font-size", "12px");
				expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320);
				await page.evaluate(() => document.documentElement.classList.add("auth-theme-fixture"));
				await expect(email).toHaveCSS("height", "48px");
				await expect(email).toHaveCSS("border-radius", "9px");
				await expect(email).toHaveCSS("border-inline-start-width", "4px");
				await expect(email).toHaveCSS("font-size", "15px");
				await expect(submit).toHaveCSS("height", "48px");
				// The compiled admin CSS lets an application utility override the semantic Button size.
				await submit.evaluate((button) => button.classList.add("h-7"));
				await expect(submit).toHaveCSS("height", "28px");
			}
		}
	);
}

test("unauthenticated admin routes preserve their destination through UI sign-in", async ({
	page,
}) => {
	await page.goto("/admin/collections/posts");
	await expect(page).toHaveURL(/\/admin\/login\?redirect=%2Fcollections%2Fposts$/);
	await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
	await page.getByLabel("Email address").fill("editor@riducms.test");
	await page.getByRole("textbox", { name: "Password", exact: true }).fill("ridu-browser");
	await page.getByRole("button", { name: "Sign in", exact: true }).click();
	await expect(page).toHaveURL(/\/admin\/collections\/posts(?:\?locale=en)?$/);
	await expect(page.getByRole("heading", { name: "Posts", exact: true })).toBeVisible();
});

test("recovery and verification expose ready, missing-token, invalid, and completion states", async ({
	page,
}) => {
	await page.goto("/admin/forgot-password");
	await expect(page.getByRole("heading", { name: "Forgot password?" })).toHaveCSS(
		"font-size",
		"32px"
	);
	await page.getByRole("button", { name: /Send .* link/ }).click();
	await expect(page.locator(".ridu-field-error-tooltip")).toHaveText("This field is required.");
	await page.getByLabel("Email address").fill("missing-user@example.test");
	await page.getByRole("button", { name: "Send reset link" }).click();
	await expect(page.getByRole("heading", { name: "Email sent" })).toBeVisible();
	await page.getByRole("link", { name: "Back to login" }).click();
	await expect(page).toHaveURL(/\/admin\/login$/);

	await page.goto("/admin/reset-password");
	await expect(page.getByRole("alert")).toContainText("token");
	await expect(page.getByRole("link", { name: "Request another link" })).toBeVisible();
	await page.goto("/admin/reset-password?token=invalid");
	await page.getByRole("textbox", { name: "New password", exact: true }).fill("a-new-password");
	await page.getByRole("textbox", { name: "Confirm password", exact: true }).fill("does-not-match");
	await page.getByRole("button", { name: "Reset password", exact: true }).click();
	await expect(page.locator("#reset-confirm-error")).toHaveText("The passwords do not match.");
	await expect(page.getByRole("textbox", { name: "Confirm password", exact: true })).toBeFocused();
	await expect(
		page.getByRole("textbox", { name: "Confirm password", exact: true })
	).toHaveAttribute("aria-errormessage", "reset-confirm-error");
	await page.getByRole("textbox", { name: "Confirm password", exact: true }).fill("a-new-password");
	await page.getByRole("button", { name: "Reset password", exact: true }).click();
	await expect(page.getByRole("alert")).not.toContainText("The passwords do not match.");
	await expect(page.getByRole("alert")).toBeVisible();

	// The real Go password policy returns a field issue before rejecting this invalid token.
	const oversizedPassword = "🌿".repeat(20);
	await page.getByRole("textbox", { name: "New password", exact: true }).fill(oversizedPassword);
	await page
		.getByRole("textbox", { name: "Confirm password", exact: true })
		.fill(oversizedPassword);
	await page.getByRole("button", { name: "Reset password", exact: true }).click();
	await expect(page.locator("#reset-password-error")).toHaveText(
		"password must not exceed 72 bytes"
	);
	await expect(page.getByRole("textbox", { name: "New password", exact: true })).toBeFocused();
	await page
		.getByRole("textbox", { name: "New password", exact: true })
		.fill("a-stronger-password");
	await expect(page.locator("#reset-password-error")).toHaveCount(0);
	await page
		.getByRole("textbox", { name: "Confirm password", exact: true })
		.fill("a-stronger-password");

	// Success envelopes exercise UI-only terminal states without minting secrets or enabling
	// email verification for every unrelated browser fixture account.
	await page.route("**/api/auth/users/reset-password", (route) =>
		route.fulfill({ json: { success: true } })
	);
	await page.getByRole("button", { name: "Reset password", exact: true }).click();
	await expect(page.getByRole("link", { name: "Back to login" })).toBeVisible();
	await expect(page.getByRole("textbox", { name: "New password", exact: true })).toHaveCount(0);
	await page.goto("/admin/verify-email");
	await expect(page.getByRole("alert")).toContainText("token");
	await page.goto("/admin/request-verification");
	await page.route("**/api/auth/users/request-verification", (route) =>
		route.fulfill({ json: { success: true } })
	);
	await page.getByRole("button", { name: /Send .* link/ }).click();
	await expect(page.locator(".ridu-field-error-tooltip")).toHaveText("This field is required.");
	await page.getByLabel("Email address").fill("missing-user@example.test");
	await page.getByRole("button", { name: "Send verification link" }).click();
	await expect(page.getByRole("link", { name: "Back to login" })).toBeVisible();
	await page.goto("/admin/verify-email?token=fixture");
	await page.route("**/api/auth/users/verify", (route) =>
		route.fulfill({ json: { success: true } })
	);
	await page.getByRole("button", { name: "Verify email", exact: true }).click();
	await expect(page.getByRole("link", { name: "Back to login" })).toBeVisible();
});
