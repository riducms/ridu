import type { DocumentLockEnvelope } from "@riducms/protocol";

import type { FormController } from "@admin/core/forms/form-controller.svelte";
import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

interface DocumentLockControllerOptions {
	runtime: AdminRuntime;
	notifications: NotificationCenter;
	form: FormController;
}

interface DocumentLockOwner {
	collection: string;
	documentID: string;
	durationSeconds: number;
}

export class DocumentLockController {
	#current = $state.raw<DocumentLockEnvelope>();
	#operation = $state(false);
	#error = $state<string>();
	#owner?: DocumentLockOwner;
	#request?: AbortController;
	#refreshTimer?: number;

	constructor(private readonly options: DocumentLockControllerOptions) {}

	get operation() {
		return this.#operation;
	}
	get error() {
		return this.#error;
	}

	get lockedByAnotherEditor() {
		return this.#current?.lock !== null && this.#current?.owned === false;
	}

	get ownerLabel() {
		return this.#current?.lock?.ownerLabel;
	}

	get updatedAt() {
		return this.#current?.lock?.updatedAt;
	}

	get canTakeOver() {
		return this.lockedByAnotherEditor && this.#current?.canTakeOver === true;
	}

	async acquire(collection: string, documentID: string, durationSeconds = 120) {
		let owner = this.#owner;
		if (owner?.collection !== collection || owner.documentID !== documentID) {
			this.release();
			owner = { collection, documentID, durationSeconds };
			this.#owner = owner;
		} else {
			owner.durationSeconds = durationSeconds;
		}
		this.options.form.writeBlocked = true;
		this.#error = undefined;
		this.#operation = true;
		try {
			await this.#requestLock(owner, false);
		} catch (cause) {
			if (this.#owner === owner) {
				this.#error =
					cause instanceof Error
						? cause.message
						: this.options.runtime.i18n.t("documents:loadFailed");
				this.options.form.writeBlocked = true;
			}
		} finally {
			if (this.#owner === owner) this.#operation = false;
		}
	}

	retry = async () => {
		const owner = this.#owner;
		if (owner === undefined || this.#operation) return;
		await this.acquire(owner.collection, owner.documentID, owner.durationSeconds);
	};

	takeOver = async () => {
		const owner = this.#owner;
		if (owner === undefined || !this.canTakeOver || this.#operation) return false;
		this.#operation = true;
		try {
			const state = await this.#requestLock(owner, true);
			if (state === undefined) return false;
			this.options.notifications.success({
				title: this.options.runtime.i18n.t("documents:lockTakenOver"),
			});
			return state.owned;
		} catch (cause) {
			if (this.#owner !== owner) return false;
			this.options.notifications.error({
				title: this.options.runtime.i18n.t("documents:lockTakeoverFailed"),
				message: cause instanceof Error ? cause.message : undefined,
			});
			return false;
		} finally {
			if (this.#owner === owner) this.#operation = false;
		}
	};

	releaseUnless(collection: string, documentID: string | undefined) {
		if (
			documentID === undefined ||
			this.#owner?.collection !== collection ||
			this.#owner.documentID !== documentID
		) {
			this.release();
		}
	}

	pause() {
		this.#request?.abort();
		this.#request = undefined;
		this.#operation = false;
		if (this.#refreshTimer !== undefined) {
			window.clearInterval(this.#refreshTimer);
			this.#refreshTimer = undefined;
		}
	}

	release() {
		this.pause();
		const owner = this.#owner;
		const owned = this.#current?.owned === true;
		this.#owner = undefined;
		this.#current = undefined;
		this.#error = undefined;
		this.options.form.writeBlocked = false;
		if (owner !== undefined && owned) {
			this.options.runtime.client
				.releaseDocumentLock(owner.collection, owner.documentID, { keepalive: true })
				.catch(() => {});
		}
	}

	async #requestLock(owner: DocumentLockOwner, takeOver: boolean) {
		this.#request?.abort();
		const request = new AbortController();
		this.#request = request;
		try {
			const state = await this.options.runtime.client.acquireDocumentLock(
				owner.collection,
				owner.documentID,
				takeOver,
				{ signal: request.signal }
			);
			if (request.signal.aborted || this.#owner !== owner) return;
			this.#apply(state);
			return state;
		} catch (cause) {
			if (request.signal.aborted || this.#owner !== owner) return;
			throw cause;
		} finally {
			if (this.#request === request) this.#request = undefined;
		}
	}

	#apply(state: DocumentLockEnvelope) {
		this.#current = state;
		this.options.form.writeBlocked = state.lock !== null && !state.owned;
		if (!state.owned && this.#refreshTimer !== undefined) {
			window.clearInterval(this.#refreshTimer);
			this.#refreshTimer = undefined;
		} else if (state.owned && this.#refreshTimer === undefined && this.#owner !== undefined) {
			this.#startRefresh(this.#owner);
		}
	}

	#startRefresh(owner: DocumentLockOwner) {
		const interval = Math.max(5, Math.floor(owner.durationSeconds / 3));
		this.#refreshTimer = window.setInterval(async () => {
			if (this.#owner !== owner || this.#request !== undefined) return;
			try {
				await this.#requestLock(owner, false);
			} catch {
				// Retry on the next tick; saves still enforce the server's current lock.
			}
		}, interval * 1_000);
	}
}
