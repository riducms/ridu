import { HISTORY_MERGE_TAG, HISTORY_PUSH_TAG } from "lexical";

/** Groups one focused name-input session into one Lexical undo entry. */
export class BlockNameHistorySession {
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
