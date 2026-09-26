import { isRecord } from "@riducms/protocol";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
import {
	getPreferenceWriteQueue,
	preferenceOwnerID,
} from "@admin/core/preferences/preference-write-queue";

const preferenceKey = "navigation";

/** Owns the sidebar preference for the shell's lifetime, including viewport changes. */
export function createNavigationPreference(
	runtime: Pick<AdminRuntime, "client" | "session" | "preparedPreferences">
) {
	let open = $state(readOpen(runtime.preparedPreferences[preferenceKey]) ?? true);
	// Profile/session snapshots can change without changing the actor who owns this preference.
	const ownerID = $derived(preferenceOwnerID(runtime.session));
	// Only the actor owning this initial snapshot may skip the first browser read.
	let preparedOwner = Object.hasOwn(runtime.preparedPreferences, preferenceKey)
		? preferenceOwnerID(runtime.session)
		: "";
	let generation = 0;

	$effect(() => {
		const owner = ownerID;
		if (owner === "") return;
		if (preparedOwner === owner) {
			preparedOwner = "";
			return;
		}
		const currentGeneration = ++generation;
		const request = new AbortController();
		runtime.client
			.preference<unknown>(preferenceKey, { signal: request.signal })
			.then((preference) => {
				const value = readOpen(preference);
				if (!request.signal.aborted && currentGeneration === generation && value !== undefined) {
					open = value;
				}
			})
			.catch(() => undefined);
		return () => request.abort();
	});

	function toggle() {
		generation += 1;
		const next = !open;
		open = next;
		const owner = preferenceOwnerID(runtime.session);
		if (owner === "") return;
		getPreferenceWriteQueue()
			.enqueue(
				preferenceKey,
				owner,
				() => preferenceOwnerID(runtime.session) === owner,
				() => runtime.client.setPreference(preferenceKey, { open: next })
			)
			.catch(() => undefined);
	}

	return {
		get open() {
			return open;
		},
		toggle,
	};
}

function readOpen(value: unknown) {
	return isRecord(value) && typeof value.open === "boolean" ? value.open : undefined;
}
