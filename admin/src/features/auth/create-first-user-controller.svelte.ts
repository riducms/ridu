import type { SchemaCollection } from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";

import { FormController, FormValidationError } from "@admin/core/forms/form-controller.svelte";
import { initialFormValues } from "@admin/core/forms/form-schema";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import { adminRoutePatterns } from "@admin/core/routing/admin-paths";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

type Navigate = (to: string, options?: { replace?: boolean }) => void;

interface CreateFirstUserControllerOptions {
	runtime: AdminRuntime;
	notifications: NotificationCenter;
	navigate: Navigate;
}

export class CreateFirstUserController {
	readonly form: FormController;
	password = $state("");
	passwordConfirmation = $state("");
	pending = $state(false);
	error = $state<string>();
	credentialIssue = $state<string>();
	#request?: AbortController;
	#generation = 0;

	constructor(readonly options: CreateFirstUserControllerOptions) {
		this.form = new FormController({}, options.runtime.i18n);
		this.form.reset(initialFormValues(this.fields));
		this.form.setAccess(undefined, "create");
		if (this.collection !== undefined) {
			this.form.setResource({ collection: this.collection.slug });
		}
	}

	get collection(): SchemaCollection | undefined {
		return this.options.runtime.authCollection;
	}

	get fields() {
		return this.collection?.fields ?? [];
	}

	get passwordHelp() {
		const minimum = this.collection?.authSettings?.passwordMinLength ?? 8;
		const maximum = this.collection?.authSettings?.passwordMaxBytes;
		return maximum === undefined
			? this.options.runtime.i18n.t("auth:passwordMinimum", {
					minimum: this.options.runtime.i18n.formatNumber(minimum),
				})
			: this.options.runtime.i18n.t("auth:passwordRange", {
					minimum: this.options.runtime.i18n.formatNumber(minimum),
					maximum: this.options.runtime.i18n.formatNumber(maximum),
				});
	}

	submit = async () => {
		if (this.pending) return undefined;
		const collection = this.collection;
		if (collection === undefined) {
			this.error = this.options.runtime.i18n.t("auth:configuredCollectionUnavailable");
			return undefined;
		}
		this.credentialIssue = this.#validatePassword(collection);
		if (this.credentialIssue !== undefined) return "password";

		const generation = ++this.#generation;
		const request = new AbortController();
		this.#request?.abort();
		this.#request = request;
		this.pending = true;
		this.error = undefined;
		let accountCreated = false;
		try {
			await this.form.submit(this.fields, true, (values) =>
				this.options.runtime.client.createAuthUser(collection.slug, values, this.password, {
					signal: request.signal,
				})
			);
			accountCreated = true;
			if (!this.#current(generation, request)) return undefined;

			const identity = this.form.get(collection.authSettings?.identityField ?? "email");
			if (typeof identity !== "string" || identity.trim() === "") {
				throw new Error(this.options.runtime.i18n.t("auth:firstAccountIdentityUnavailable"));
			}
			const session = await this.options.runtime.client.login(
				collection.slug,
				{ email: identity, password: this.password },
				{ signal: request.signal }
			);
			if (!this.#current(generation, request)) return undefined;
			const initialAccessAuthoritative = await this.options.runtime.refreshAccess();
			if (!this.#current(generation, request)) return undefined;
			if (
				initialAccessAuthoritative &&
				this.options.runtime.collectionOperations[collection.slug]?.admin !== true
			) {
				try {
					await this.options.runtime.client.logout({ signal: request.signal });
				} catch {
					// The setup route remains closed even if cookie cleanup fails.
				}
				throw new Error(this.options.runtime.i18n.t("auth:firstAccountNoAdminAccess"));
			}

			this.options.runtime.authBootstrapAvailable = false;
			this.options.runtime.session = session;
			await Promise.all([
				this.options.runtime.loadTheme(),
				this.options.runtime.loadContentLocale(),
				this.options.runtime.i18n.loadPreferences(),
			]);
			const preferredAccessAuthoritative = await this.options.runtime.refreshAccess(
				this.options.runtime.contentLocale
			);
			if (
				preferredAccessAuthoritative &&
				this.options.runtime.collectionOperations[collection.slug]?.admin !== true
			) {
				try {
					await this.options.runtime.client.logout({ signal: request.signal });
				} catch {
					// The setup route remains closed even if cookie cleanup fails.
				}
				this.options.runtime.session = undefined;
				throw new Error(this.options.runtime.i18n.t("auth:firstAccountNoAdminAccess"));
			}
			if (!this.#current(generation, request)) return undefined;
			this.options.navigate(adminRoutePatterns.home, { replace: true });
			return undefined;
		} catch (cause) {
			if (!this.#current(generation, request)) return undefined;
			if (accountCreated) {
				this.#continueToLogin(
					this.options.runtime.i18n.t("auth:accountCreated"),
					cause instanceof Error
						? this.options.runtime.i18n.t("auth:autoSignInFailed", { message: cause.message })
						: this.options.runtime.i18n.t("auth:accountReady")
				);
				return undefined;
			}
			if (cause instanceof FormValidationError) {
				this.error = cause.message;
				return this.form.issues[0]?.path;
			}
			if (cause instanceof RiduError) {
				const passwordIssue = cause.issues.find((issue) => issue.path === "password");
				if (passwordIssue !== undefined) {
					this.credentialIssue = passwordIssue.message;
					this.form.issues = this.form.issues.filter((issue) => issue.path !== "password");
					this.error = this.options.runtime.i18n.t("auth:correctInvalidFields");
					return "password";
				}
			}

			try {
				const bootstrap = await this.options.runtime.client.authBootstrap(collection.slug, {
					signal: request.signal,
				});
				if (!this.#current(generation, request)) return undefined;
				if (!bootstrap.available) {
					this.#continueToLogin(
						this.options.runtime.i18n.t("auth:setupCompleted"),
						this.options.runtime.i18n.t("auth:firstAccountExists")
					);
					return undefined;
				}
			} catch {
				if (!this.#current(generation, request)) return undefined;
			}
			this.error =
				cause instanceof Error
					? cause.message
					: this.options.runtime.i18n.t("auth:firstAccountCreateFailed");
			return this.form.issues[0]?.path;
		} finally {
			if (this.#current(generation, request)) {
				this.pending = false;
				this.#request = undefined;
			}
		}
	};

	destroy() {
		this.#generation += 1;
		this.#request?.abort();
		this.#request = undefined;
	}

	#continueToLogin(title: string, message: string) {
		this.options.notifications.error({ title, message });
		this.options.navigate(adminRoutePatterns.login, { replace: true });
		this.options.runtime.authBootstrapAvailable = false;
	}

	#current(generation: number, request: AbortController) {
		return generation === this.#generation && !request.signal.aborted;
	}

	#validatePassword(collection: SchemaCollection) {
		const settings = collection.authSettings;
		if (this.password === "") return this.options.runtime.i18n.t("auth:enterPassword");
		if (Array.from(this.password).length < (settings?.passwordMinLength ?? 8)) {
			return this.options.runtime.i18n.t("auth:passwordTooShort", {
				minimum: this.options.runtime.i18n.formatNumber(settings?.passwordMinLength ?? 8),
			});
		}
		if (new TextEncoder().encode(this.password).length > (settings?.passwordMaxBytes ?? 72)) {
			return this.options.runtime.i18n.t("auth:passwordTooLong", {
				maximum: this.options.runtime.i18n.formatNumber(settings?.passwordMaxBytes ?? 72),
			});
		}
		if (this.password !== this.passwordConfirmation) {
			return this.options.runtime.i18n.t("auth:passwordMismatch");
		}
		return undefined;
	}
}
