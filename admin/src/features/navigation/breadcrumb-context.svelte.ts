import { createContext } from "svelte";

export class BreadcrumbState {
	document = $state.raw<{ pathname: string; label: string; version?: string }>();
}

const [getState, setState, hasState] = createContext<BreadcrumbState>();

export function provideBreadcrumbs() {
	return setState(new BreadcrumbState());
}

export function getBreadcrumbs() {
	return hasState() ? getState() : undefined;
}
