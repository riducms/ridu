import type { SchemaField } from "@riducms/protocol";
import type { FormController } from "@admin/core/forms/form-controller.svelte";
import { cloneFormValue } from "@admin/core/forms/form-schema";

/** Follow a mounted embedded item through reorders and report only its ordinary field writes. */
export function observeEmbeddedSchemaForm(
	form: FormController,
	field: SchemaField,
	treeKey: string,
	identity: string,
	onChange: (payload: Record<string, unknown>) => void,
	canNotify: () => boolean
) {
	const initial = form.embeddedOccurrence(field, treeKey, identity);
	if (initial === undefined) return () => {};
	const mountKey = form.rowMountKey(initial.payload);
	let previous = JSON.stringify(initial.payload);

	return form.observe(field.path, (_value, changedPath) => {
		const current = form.embeddedOccurrence(field, treeKey, identity);
		if (current === undefined || form.rowMountKey(current.payload) !== mountKey) return;
		const next = JSON.stringify(current.payload);
		if (next === previous) return;
		previous = next;
		if (
			(changedPath !== current.path && !changedPath.startsWith(`${current.path}.`)) ||
			!canNotify()
		)
			return;
		onChange(cloneFormValue(current.payload) as Record<string, unknown>);
	});
}
