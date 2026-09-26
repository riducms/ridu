import { createContext } from "svelte";

const [getDepth, setDepth, hasDepth] = createContext<number>();

/** Nested authoring keeps a visible edge of each parent drawer. */
export function provideDrawerDepth() {
	return setDepth((hasDepth() ? getDepth() : 0) + 1);
}
