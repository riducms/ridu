import { getContext, setContext } from "svelte";

export interface FieldLayoutPresentation {
	readonly groupActions: boolean;
}

const key = Symbol("field-layout-presentation");

export function getFieldLayoutPresentation(): FieldLayoutPresentation | undefined {
	return getContext<FieldLayoutPresentation | undefined>(key);
}

export function setFieldLayoutPresentation(presentation: FieldLayoutPresentation) {
	setContext(key, presentation);
}
