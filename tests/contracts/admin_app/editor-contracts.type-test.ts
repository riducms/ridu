import { defineAdmin } from "@riducms/plugin/admin";
import { defineFieldEditor } from "@riducms/plugin/editor";
import LocalTextEditor from "./local-text-editor.svelte";

// These assertions compile against actual Svelte-generated component props.
function editorCompilerContract() {
	const registered = defineFieldEditor({
		type: "text",
		component: LocalTextEditor,
		decodeConfig: () => ({ capture: false }),
	});
	// @ts-expect-error registration erasure cannot make text props compatible with number
	defineFieldEditor({ type: "number", component: registered.component });
	// @ts-expect-error the original Svelte component still requires decoded config
	defineFieldEditor({ type: "text", component: registered.component });
	defineAdmin({
		fields: {
			"app:example": defineFieldEditor({
				type: "text",
				component: LocalTextEditor,
				decodeConfig: (_value: unknown) => ({ capture: false }),
			}),
		},
	});
	defineFieldEditor({
		type: "number",
		// @ts-expect-error the Svelte component consumes text, never number
		component: LocalTextEditor,
		decodeConfig: (_value: unknown) => ({ capture: false }),
	});
	defineFieldEditor({
		type: "text",
		// @ts-expect-error the decoder result must match the component's config prop
		component: LocalTextEditor,
		decodeConfig: (_value: unknown) => ({ palette: [] }),
	});
	// @ts-expect-error non-optional configuration cannot be supplied by an unchecked generic
	defineFieldEditor({ type: "text", component: LocalTextEditor });
	// @ts-expect-error explicitly naming a config type still requires its decoder
	defineFieldEditor<"text", { capture: boolean }>({ type: "text", component: LocalTextEditor });
	// @ts-expect-error bottom types cannot erase the actual Svelte component's config requirement
	defineFieldEditor<"text", never>({ type: "text", component: LocalTextEditor });
}
void editorCompilerContract;
