import { fail } from "@sveltejs/kit";
import { RiduError } from "@riducms/sdk";
import { documentRecoveryIssue, type RichTextDocument } from "@riducms/sdk/richtext";

import { ridu } from "#lib/server/ridu.js";

import type { Actions, PageServerLoad } from "./$types";

export const load: PageServerLoad = async ({ params }) => ({
	post: await ridu.find("posts", params.id),
});

function isRichTextDocument(value: unknown): value is RichTextDocument {
	return documentRecoveryIssue(value) === undefined;
}

export const actions: Actions = {
	default: async ({ params, request }) => {
		const body: unknown = JSON.parse(String((await request.formData()).get("body")));
		if (!isRichTextDocument(body)) return fail(400, { issues: ["The body isn't a document."] });
		try {
			await ridu.update("posts", params.id, { body });
		} catch (error) {
			if (!(error instanceof RiduError)) throw error;
			return fail(error.status, { issues: error.issues.map((issue) => issue.message) });
		}
	},
};
