import { describe, expect, it } from "bun:test";
import { createEmptyHistoryState, registerHistory } from "@lexical/history";
import { $getRoot, REDO_COMMAND, UNDO_COMMAND, createEditor, type EditorState } from "lexical";
import { BlockNode, createBlockNode, updateBlockName } from "../src/block/rich-text-block-node";
import { BlockNameHistorySession } from "../src/block/rich-text-block-name";

function fields(editorState: EditorState) {
	return editorState.read(() =>
		$getRoot()
			.getChildren()
			.map((node) => {
				if (!(node instanceof BlockNode)) throw new Error("Expected a block node");
				return node.getFields();
			})
	);
}

describe("rich-text block names", () => {
	it("patches only the latest matching card and preserves its identity and siblings", () => {
		const editor = createEditor({ namespace: "block-name-target", nodes: [BlockNode] });
		let firstKey = "";
		editor.update(
			() => {
				const first = createBlockNode({
					_key: "first",
					blockType: "callout",
					name: "Before",
					nested: { retained: true },
				});
				firstKey = first.getKey();
				$getRoot().append(
					first,
					createBlockNode({ _key: "second", blockType: "callout", name: "Sibling" })
				);
			},
			{ discrete: true }
		);

		const session = new BlockNameHistorySession();
		editor.update(
			() => {
				updateBlockName({
					nodeKey: firstKey,
					identity: "first",
					nameField: "name",
					change: { field: "name", value: "After" },
				});
			},
			{ tag: session.nextTag(), discrete: true }
		);
		expect(fields(editor.getEditorState())).toEqual([
			{
				_key: "first",
				blockType: "callout",
				name: "After",
				nested: { retained: true },
			},
			{ _key: "second", blockType: "callout", name: "Sibling" },
		]);

		editor.update(
			() => {
				updateBlockName({
					nodeKey: firstKey,
					identity: "stale",
					nameField: "name",
					change: { field: "name", value: "Wrong identity" },
				});
				updateBlockName({
					nodeKey: firstKey,
					identity: "first",
					nameField: "name",
					change: { field: "other", value: "Wrong field" },
				});
			},
			{ discrete: true }
		);
		expect(fields(editor.getEditorState())[0]?.name).toBe("After");
	});

	it("groups each simulated focus session into one undo entry and supports redo", async () => {
		const editor = createEditor({ namespace: "block-name-history", nodes: [BlockNode] });
		const history = createEmptyHistoryState();
		const unregister = registerHistory(editor, history, 300);
		let nodeKey = "";
		editor.update(
			() => {
				const node = createBlockNode({ _key: "one", blockType: "callout", name: "Before" });
				nodeKey = node.getKey();
				$getRoot().append(node);
			},
			{ discrete: true }
		);
		const session = new BlockNameHistorySession();
		const update = (value: string) =>
			editor.update(
				() => {
					updateBlockName({
						nodeKey,
						identity: "one",
						nameField: "name",
						change: { field: "name", value },
					});
				},
				{ tag: session.nextTag(), discrete: true }
			);

		session.reset();
		update("F");
		update("Fi");
		update("First focus");
		expect(fields(editor.getEditorState())[0]?.name).toBe("First focus");
		expect(history.undoStack.length).toBe(1);
		editor.dispatchCommand(UNDO_COMMAND, undefined);
		await Promise.resolve();
		expect(fields(editor.getEditorState())[0]?.name).toBe("Before");
		session.reset();
		editor.dispatchCommand(REDO_COMMAND, undefined);
		await Promise.resolve();
		expect(fields(editor.getEditorState())[0]?.name).toBe("First focus");

		session.reset();
		update("Second focus");
		editor.dispatchCommand(UNDO_COMMAND, undefined);
		await Promise.resolve();
		expect(fields(editor.getEditorState())[0]?.name).toBe("First focus");
		unregister();
	});
});
