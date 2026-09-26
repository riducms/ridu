import { useHref, useLocation, useScrollRestoration } from "@hvniel/svelte-router";
import { createContext, tick } from "svelte";
import { SvelteMap } from "svelte/reactivity";

interface AdminScrollPage {
	ready: () => boolean;
	target?: () => HTMLElement | null;
}

const [getAdminScroll, setAdminScroll] = createContext<{
	readonly page: AdminScrollPage | undefined;
	register: (page: AdminScrollPage) => () => void;
}>();

/** Provide before route definitions so every retained/replaced route inherits one identity. */
export function createAdminScroll() {
	// A keyed mutation lets outgoing cleanup remove only its own entry. Replacing
	// a rune array from teardown can overwrite registrations made in the incoming batch.
	const pages = new SvelteMap<symbol, AdminScrollPage>();
	setAdminScroll({
		get page() {
			return Array.from(pages.values()).at(-1);
		},
		register(next) {
			const owner = Symbol();
			pages.set(owner, next);
			return () => {
				pages.delete(owner);
			};
		},
	});
}

/** The shell owns restoration; a route supplies its content readiness and optional primary pane. */
export function useAdminScrollRestoration(
	target: () => Window | HTMLElement | null,
	waitForPage: () => boolean
) {
	const location = useLocation();
	const root = useHref("/");
	const scroll = getAdminScroll();
	const page = $derived(scroll.page);
	let renderedLocation = $state.raw<ReturnType<typeof useLocation>["current"]>();

	$effect(() => {
		const current = location.current;
		const ready = page?.ready() ?? !waitForPage();
		renderedLocation = undefined;
		let active = true;
		// Controllers synchronize the new URL in effects. Wait for those effects and
		// their DOM flush before accepting readiness left over from the outgoing page.
		tick().then(() => {
			if (active && ready) renderedLocation = current;
		});
		return () => {
			active = false;
		};
	});

	useScrollRestoration({
		storageKey: `ridu-admin-scroll:${root.current}`,
		target: () => {
			const shell = target();
			return shell === window || page?.target === undefined ? shell : page.target();
		},
		ready: () => renderedLocation === location.current && (page?.ready() ?? !waitForPage()),
	});
}

export function registerAdminScrollPage(page: AdminScrollPage) {
	const scroll = getAdminScroll();
	// Register before the shell's restoration effect; release only this route's ownership.
	$effect.pre(() => scroll.register(page));
}
