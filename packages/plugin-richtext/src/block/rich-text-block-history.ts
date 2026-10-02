import { HISTORY_MERGE_TAG, HISTORY_PUSH_TAG } from "lexical";

/** Distinguish ordinary inline edits from Lexical's hydration and normalization merges. */
export const BLOCK_FIELD_CHANGE_TAG = "ridu-block-field-change";

/** Groups one focused embedded-field session into one Lexical undo entry. */
export class BlockFieldHistorySession {
	#merge = false;

	reset() {
		this.#merge = false;
	}

	nextTag(): typeof HISTORY_PUSH_TAG | typeof HISTORY_MERGE_TAG {
		const tag = this.#merge ? HISTORY_MERGE_TAG : HISTORY_PUSH_TAG;
		this.#merge = true;
		return tag;
	}
}
