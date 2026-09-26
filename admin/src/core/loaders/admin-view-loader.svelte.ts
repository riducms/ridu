import { RiduError, type AdminLoader } from "@riducms/sdk";
import type { AdminReadResultV1 } from "@riducms/protocol";
import type { AdminClient } from "@admin/core/api/admin-client";

interface AdminViewLoaderOptions {
	client: Pick<AdminClient, "adminLoad">;
	loader: AdminLoader<never, unknown>;
	get route(): string;
	get prepared(): AdminReadResultV1<unknown> | undefined;
}

/** One mounted view owns its data and supersedable refresh. */
export class AdminViewLoader {
	#data = $state.raw<unknown>();
	#ready = $state(false);
	#refreshing = $state(false);
	#error = $state<string>();
	#request?: AbortController;
	#route?: string;

	constructor(private readonly options: AdminViewLoaderOptions) {
		// Adopt router data before a retained panel can render the destination query.
		$effect.pre(() => {
			const route = options.route;
			if (route === this.#route) return;
			this.#route = route;
			this.#request?.abort();
			this.#request = undefined;
			this.#ready = false;
			this.#refreshing = false;
			this.#error = undefined;
			const prepared = options.prepared;
			if (prepared !== undefined) {
				if (prepared.error !== undefined) this.#error = prepared.error.message;
				else {
					this.#data = options.loader.decode(prepared.value);
					this.#ready = true;
				}
			} else this.refresh();
		});
		$effect(() => () => this.#request?.abort());
	}

	get data() {
		return this.#data;
	}

	get ready() {
		return this.#ready;
	}

	get refreshing() {
		return this.#refreshing;
	}

	get error() {
		return this.#error;
	}

	get settled() {
		return this.#ready || this.#error !== undefined;
	}

	refresh = async () => {
		const route = this.options.route;
		this.#request?.abort();
		const request = new AbortController();
		this.#request = request;
		this.#refreshing = true;
		this.#error = undefined;
		try {
			const data = await this.options.client.adminLoad(this.options.loader.key, route, {
				signal: request.signal,
			});
			// Aborting cannot undo a response that already completed; only the current owner may
			// decode and publish it.
			if (request.signal.aborted) return;
			this.#data = this.options.loader.decode(data);
			this.#ready = true;
		} catch (cause) {
			if (request.signal.aborted) return;
			this.#error = cause instanceof RiduError ? cause.message : "Unable to load view data.";
		} finally {
			if (this.#request === request) {
				this.#request = undefined;
				this.#refreshing = false;
			}
		}
	};
}
