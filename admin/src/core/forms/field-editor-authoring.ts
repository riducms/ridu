import type { FieldAuthoringHost } from "@riducms/plugin";
import type { FieldEditorProps } from "@riducms/plugin/editor";
import { cloneFormValue } from "@admin/core/forms/form-schema";

type EditorAuthoring = FieldEditorProps["authoring"];

/** Stable callable identities, with invocation and async completion bound to one editor mount. */
export function guardEditorAuthoring(
	authoring: () => FieldAuthoringHost,
	assertActive: () => void
): EditorAuthoring {
	const referenceBrowser: EditorAuthoring["referenceBrowser"] = (...args) => {
		assertActive();
		return authoring().referenceBrowser(...args);
	};
	const findDocument: EditorAuthoring["findDocument"] = (...args) => {
		assertActive();
		return authoring()
			.findDocument(...args)
			.then((document) => {
				assertActive();
				return document;
			});
	};
	return {
		get collections() {
			assertActive();
			return cloneFormValue(authoring().collections) as EditorAuthoring["collections"];
		},
		get documentRevision() {
			assertActive();
			return authoring().documentRevision;
		},
		get locale() {
			assertActive();
			return authoring().locale;
		},
		get referenceBrowser() {
			assertActive();
			return referenceBrowser;
		},
		get findDocument() {
			assertActive();
			return findDocument;
		},
	};
}
