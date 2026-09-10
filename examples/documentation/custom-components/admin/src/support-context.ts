import { createContext } from 'svelte';

// Both functions share the same typed Svelte context.
export const [getSupport, setSupport] = createContext<{
	email: string;
}>();
