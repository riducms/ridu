export type PreferenceWriteResult<Value> =
	{ dispatched: false } | { dispatched: true; value: Value };

export interface PreferenceWriteState {
	pending: boolean;
	hasValue: boolean;
	value?: unknown;
}

export function preferenceOwnerID(
	session: { collection: string; user: { id: string } } | undefined
) {
	return session === undefined ? "" : JSON.stringify([session.collection, session.user.id]);
}

type PreferenceWriteListener = (state: PreferenceWriteState) => void;

export class PreferenceWriteQueue {
	#queues = new Map<string, Promise<unknown>>();
	#pending = new Map<string, number>();
	#latest = new Map<string, unknown>();
	#listeners = new Map<string, Set<PreferenceWriteListener>>();
	#ownerGenerations = new Map<string, number>();
	#ownerWrites = new Map<string, Set<Promise<unknown>>>();

	enqueue<Value>(
		key: string,
		owner: string,
		canDispatch: () => boolean,
		write: () => Promise<Value>
	): Promise<PreferenceWriteResult<Value>> {
		const previous = this.#queues.get(key) ?? Promise.resolve();
		const ownerGeneration = this.#ownerGenerations.get(owner) ?? 0;
		this.#pending.set(key, (this.#pending.get(key) ?? 0) + 1);
		this.#emit(key);
		const queued = previous
			.catch(() => undefined)
			.then(async (): Promise<PreferenceWriteResult<Value>> => {
				if ((this.#ownerGenerations.get(owner) ?? 0) !== ownerGeneration || !canDispatch()) {
					return { dispatched: false };
				}
				const value = await write();
				this.#latest.set(key, value);
				return { dispatched: true, value };
			});
		this.#queues.set(key, queued);
		const ownerWrites = this.#ownerWrites.get(owner) ?? new Set<Promise<unknown>>();
		ownerWrites.add(queued);
		this.#ownerWrites.set(owner, ownerWrites);
		const settle = () => {
			ownerWrites.delete(queued);
			if (ownerWrites.size === 0) this.#ownerWrites.delete(owner);
			if (this.#queues.get(key) === queued) this.#queues.delete(key);
			const pending = (this.#pending.get(key) ?? 1) - 1;
			if (pending === 0) this.#pending.delete(key);
			else this.#pending.set(key, pending);
			this.#emit(key);
			if (pending === 0) this.#latest.delete(key);
		};
		void queued.then(settle, settle);
		return queued;
	}

	isPending(key: string) {
		return (this.#pending.get(key) ?? 0) > 0;
	}

	subscribe(key: string, listener: PreferenceWriteListener) {
		const listeners = this.#listeners.get(key) ?? new Set<PreferenceWriteListener>();
		listeners.add(listener);
		this.#listeners.set(key, listeners);
		listener(this.#state(key));
		return () => {
			listeners.delete(listener);
			if (listeners.size === 0) this.#listeners.delete(key);
		};
	}

	async settleOwner(owner: string) {
		while ((this.#ownerWrites.get(owner)?.size ?? 0) > 0) {
			await Promise.allSettled([...(this.#ownerWrites.get(owner) ?? [])]);
		}
	}

	invalidateOwner(owner: string) {
		this.#ownerGenerations.set(owner, (this.#ownerGenerations.get(owner) ?? 0) + 1);
	}

	#emit(key: string) {
		const state = this.#state(key);
		for (const listener of this.#listeners.get(key) ?? []) listener(state);
	}

	#state(key: string): PreferenceWriteState {
		return {
			pending: this.isPending(key),
			hasValue: this.#latest.has(key),
			value: this.#latest.get(key),
		};
	}
}

const preferenceWrites = new PreferenceWriteQueue();

export function getPreferenceWriteQueue() {
	return preferenceWrites;
}
