import type { CompileOptions } from "svelte/compiler";

type CSSHash = NonNullable<CompileOptions["cssHash"]>;

export const contentCSSHash: CSSHash = ({ css, hash }) => `svelte-${hash(css)}`;
