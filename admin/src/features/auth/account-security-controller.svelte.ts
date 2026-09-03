import type { APIKey, APIKeyInfo, AuthSessionInfo } from "@riducms/protocol";
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
	#generation = 0;

	constructor(
		private readonly client: AdminClient,
		readonly apiKeysEnabled: boolean,
		private readonly translate: AdminI18n["t"]
	) {}

	load = async () => {
		this.#request?.abort();
		const request = new AbortController();
		this.#request = request;
		const generation = ++this.#generation;
		this.status = "loading";
		this.error = undefined;
		try {
			const [sessions, apiKeys] = await Promise.all([
				this.client.sessions({ signal: request.signal }),
				this.apiKeysEnabled ? this.client.apiKeys({ signal: request.signal }) : Promise.resolve([]),
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
		return this.#run("session:" + id, async () => {
			await this.client.revokeSession(id);
			this.sessions = this.sessions.filter((session) => session.id !== id);
		});
	};

	logoutAll = async () => {
		return this.#run("logout-all", async () => {
			await this.client.logoutAll();
			this.sessions = [];
		});
	};

	createAPIKey = async (name: string, expiresAt?: string) => {
		return this.#run("api-key:create", async () => {
			const created = await this.client.createAPIKey({ name, expiresAt });
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
		});
	};

	dismissCreatedAPIKey = () => {
		this.createdAPIKey = undefined;
	};

	revokeAPIKey = async (id: string) => {
		return this.#run("api-key:" + id, async () => {
			await this.client.revokeAPIKey(id);
			this.apiKeys = this.apiKeys.filter((key) => key.id !== id);
			if (this.createdAPIKey?.id === id) this.createdAPIKey = undefined;
		});
	};

	changePassword = async (currentPassword: string, password: string) => {
		return this.#run("password", async () => {
			await this.client.changePassword(currentPassword, password);
		});
	};

	destroy() {
		this.#generation += 1;
		this.#request?.abort();
	}

	async #run(name: string, operation: () => Promise<void>) {
		if (this.operation !== undefined) return false;
		this.operation = name;
		this.error = undefined;
		try {
			await operation();
			return true;
		} catch (cause) {
			this.error = messageFrom(cause, this.translate("account:securityOperationFailed"));
			return false;
		} finally {
			this.operation = undefined;
		}
	}
}

function messageFrom(cause: unknown, fallback: string) {
	return cause instanceof RiduError || cause instanceof Error ? cause.message : fallback;
}
