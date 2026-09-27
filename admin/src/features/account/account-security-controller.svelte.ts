import type { APIKey, APIKeyInfo, AuthSessionInfo, AdminSecurityDataV1 } from "@riducms/protocol";
import { RiduError } from "@riducms/sdk";
import type { AdminI18n } from "@riducms/translations";

import type { AdminClient } from "@admin/core/api/admin-client";

export type AccountSecurityStatus = "idle" | "loading" | "ready" | "error";

export class AccountSecurityController {
	status = $state<AccountSecurityStatus>("idle");
	error = $state<string>();
	operation = $state<string>();
	sessions = $state.raw<readonly AuthSessionInfo[]>([]);
	apiKeys = $state.raw<readonly APIKeyInfo[]>([]);
	createdAPIKey = $state.raw<APIKey>();
	#request?: AbortController;
	#operationRequest?: AbortController;
	#generation = 0;

	constructor(
		private readonly client: AdminClient,
		readonly apiKeysEnabled: boolean,
		private readonly translate: AdminI18n["t"]
	) {}

	load = async (initial?: AdminSecurityDataV1) => {
		this.#request?.abort();
		this.#operationRequest?.abort();
		this.#operationRequest = undefined;
		this.operation = undefined;
		this.sessions = [];
		this.apiKeys = [];
		this.createdAPIKey = undefined;
		if (initial !== undefined) {
			this.#generation += 1;
			this.#request = undefined;
			this.error = initial.sessions.error?.message ?? initial.apiKeys.error?.message;
			this.sessions = initial.sessions.value ?? [];
			this.apiKeys = initial.apiKeys.value ?? [];
			this.status = this.error === undefined ? "ready" : "error";
			return;
		}
		const request = new AbortController();
		this.#request = request;
		const generation = ++this.#generation;
		this.status = "loading";
		this.error = undefined;
		try {
			const [sessions, apiKeys] = await Promise.all([
				this.client.auth.sessions({ signal: request.signal }),
				this.apiKeysEnabled
					? this.client.auth.apiKeys({ signal: request.signal })
					: Promise.resolve([]),
			]);
			if (request.signal.aborted || generation !== this.#generation) return;
			this.sessions = sessions;
			this.apiKeys = apiKeys;
			this.status = "ready";
		} catch (cause) {
			if (request.signal.aborted || generation !== this.#generation) return;
			this.error = messageFrom(cause, this.translate("account:securityLoadFailed"));
			this.status = "error";
		}
	};

	revokeSession = async (id: string) => {
		return this.#run(
			"session:" + id,
			(signal) => this.client.auth.revokeSession(id, { signal }),
			() => (this.sessions = this.sessions.filter((session) => session.id !== id))
		);
	};

	logoutAll = async () => {
		return this.#run(
			"logout-all",
			(signal) => this.client.auth.logoutAll({ signal }),
			() => (this.sessions = [])
		);
	};

	createAPIKey = async (name: string, expiresAt?: string) => {
		return this.#run(
			"api-key:create",
			(signal) => this.client.auth.createAPIKey({ name, expiresAt }, { signal }),
			(created) => {
				this.createdAPIKey = created;
				this.apiKeys = [
					{
						id: created.id,
						name: created.name,
						createdAt: created.createdAt,
						expiresAt: created.expiresAt,
					},
					...this.apiKeys,
				];
			}
		);
	};

	dismissCreatedAPIKey = () => {
		this.createdAPIKey = undefined;
	};

	revokeAPIKey = async (id: string) => {
		return this.#run(
			"api-key:" + id,
			(signal) => this.client.auth.revokeAPIKey(id, { signal }),
			() => {
				this.apiKeys = this.apiKeys.filter((key) => key.id !== id);
				if (this.createdAPIKey?.id === id) this.createdAPIKey = undefined;
			}
		);
	};

	changePassword = async (currentPassword: string, password: string) => {
		return this.#run("password", (signal) =>
			this.client.auth.changePassword({ currentPassword, password }, { signal })
		);
	};

	destroy() {
		this.#generation += 1;
		this.#request?.abort();
		this.#operationRequest?.abort();
	}

	async #run<Result>(
		name: string,
		operation: (signal: AbortSignal) => Promise<Result>,
		apply?: (result: Result) => void
	) {
		if (this.operation !== undefined) return false;
		const request = new AbortController();
		const generation = this.#generation;
		this.#operationRequest = request;
		this.operation = name;
		this.error = undefined;
		try {
			const result = await operation(request.signal);
			if (request.signal.aborted || generation !== this.#generation) return false;
			apply?.(result);
			return true;
		} catch (cause) {
			if (request.signal.aborted || generation !== this.#generation) return false;
			this.error = messageFrom(cause, this.translate("account:securityOperationFailed"));
			return false;
		} finally {
			if (this.#operationRequest === request) {
				this.#operationRequest = undefined;
				this.operation = undefined;
			}
		}
	}
}

function messageFrom(cause: unknown, fallback: string) {
	return cause instanceof RiduError || cause instanceof Error ? cause.message : fallback;
}
