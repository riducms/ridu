import type { Locator, Page } from "@playwright/test";
import { createClient } from "@riducms/sdk";
import { documentRecoveryIssue } from "@riducms/sdk/richtext";
import type { AdminConfig } from "../../../admin/src/core/api/admin-client";
import { expect, test } from "./fixture";
import { documentSaveButton, loginAsEditor, observePageErrors } from "./helpers";

const rejectedEdit = /This edit can.t be saved/;

async function createPublishedPost(
	client: Awaited<ReturnType<typeof editorClient>>,
	title: string,
	content: unknown
) {
	const draft = await client.create(
		"posts",
		{ title, summary: title, content },
		{
			locale: "en",
		}
	);
	return client.publish("posts", draft.id, {
		revision: draft._revision as number,
		locale: "en",
	});
}

// Reproduce the original failure: a list start that serializes as null.
async function rejectNextExport(editor: Locator) {
	await editor.evaluate((root) => {
		type ListNode = { __type: string; setStart(start: number): void };
		const lexical = (
			root as HTMLElement & {
				__lexicalEditor: {
					update(change: () => void): void;
					getEditorState(): { _nodeMap: Map<string, ListNode> };
				};
			}
		).__lexicalEditor;
		lexical.update(() => {
			for (const node of lexical.getEditorState()._nodeMap.values())
				if (node.__type === "list") node.setStart(Number.NaN);
		});
	});
}

async function editorClient(page: Page, baseURL: string) {
	await loginAsEditor(page);
	const cookies = await page.context().cookies(baseURL);
	return createClient<AdminConfig>({
		baseURL,
		headers: { Cookie: cookies.map(({ name, value }) => `${name}=${value}`).join("; ") },
	});
}

// Every node type the posts content field allows, as sparse as an API client may write it.
function sparseContent(mediaID: string, postID: string) {
	const text = (value: string) => ({ type: "text", text: value });
	const item = (value: string) => ({ type: "listitem", children: [text(value)] });
	return {
		version: 1,
		root: {
			type: "root",
			children: [
				{ type: "heading", tag: "h2", children: [text("Heading")] },
				{
					type: "paragraph",
					children: [
						text("Before"),
						{ type: "linebreak" },
						{ type: "link", url: "https://example.com", children: [text("a link")] },
					],
				},
				{ type: "quote", children: [text("Quoted")] },
				{ type: "list", listType: "bullet", children: [item("Bullet")] },
				{ type: "list", listType: "number", children: [item("First"), item("Second")] },
				{
					type: "list",
					listType: "check",
					children: [
						{ ...item("Done"), checked: true },
						{ ...item("Open"), checked: false },
					],
				},
				{ type: "code", children: [text("const answer = 42;")] },
				{ type: "horizontalrule" },
				{ type: "upload", relationTo: "media", id: mediaID },
				{ type: "relationship", relationTo: "posts", id: postID },
			],
		},
	};
}

function numberedList(text: string) {
	return {
		version: 1,
		root: {
			type: "root",
			children: [
				{
					type: "list",
					listType: "number",
					children: [{ type: "listitem", children: [{ type: "text", text }] }],
				},
			],
		},
	};
}

function nodeTypes(value: unknown): string[] {
	if (Array.isArray(value)) return value.flatMap(nodeTypes);
	if (typeof value !== "object" || value === null) return [];
	const node = value as { type?: unknown; children?: unknown; root?: unknown };
	return [
		...(typeof node.type === "string" ? [node.type] : []),
		...nodeTypes(node.children),
		...nodeTypes(node.root),
	];
}

test("sparse rich text written through the API stays editable", async ({ page, adminServer }) => {
	const errors = observePageErrors(page);
	const client = await editorClient(page, adminServer.url);
	const media = await client.list("media", {
		where: { filename: { equals: "ridu-cover.png" } },
		limit: 1,
	});
	const related = await client.list("posts", { limit: 1 });
	const content = sparseContent(String(media.docs[0]!.id), String(related.docs[0]!.id));
	expect(documentRecoveryIssue(content)).toBeUndefined();
	const created = await createPublishedPost(client, "Sparse rich text", content);

	await page.goto(`/admin/collections/posts/${created.id}?locale=en`);
	const editor = page.getByRole("textbox", { name: "Content — English" });
	// As in the report, the article is published: only a recorded edit enables publishing.
	const publish = page.getByRole("button", { name: "Publish changes", exact: true });
	await expect(publish).toBeDisabled();
	await editor.getByText("Second", { exact: true }).click({ clickCount: 3 });
	await page.keyboard.type("Second item");
	await expect(publish).toBeEnabled();
	await publish.click();

	await expect
		.poll(async () =>
			JSON.stringify((await client.find("posts", created.id, { locale: "en" })).content)
		)
		.toContain("Second item");
	const saved = (await client.find("posts", created.id, { locale: "en" })).content;
	expect(documentRecoveryIssue(saved)).toBeUndefined();
	expect(new Set(nodeTypes(saved))).toEqual(new Set(nodeTypes(content)));
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("an editor export the server would reject is reported on the field", async ({
	page,
	adminServer,
}) => {
	const errors = observePageErrors(page);
	const client = await editorClient(page, adminServer.url);
	const content = numberedList("Item");
	const created = await createPublishedPost(client, "Rejected rich-text edit", content);
	await page.goto(`/admin/collections/posts/${created.id}?locale=en`);
	const editor = page.getByRole("textbox", { name: "Content — English" });
	await expect(editor.getByText("Item", { exact: true })).toBeVisible();

	await rejectNextExport(editor);
	await expect(page.getByText(rejectedEdit).first()).toBeVisible();
	await expect(editor).toHaveAttribute("aria-invalid", "true");

	// The form holds the rejected edit: publishing fails with it and saves nothing...
	await page.getByRole("button", { name: "Publish changes", exact: true }).click();
	await expect(page.getByText(rejectedEdit).first()).toBeVisible();
	await expect(editor.getByText("Item", { exact: true })).toBeVisible();
	expect((await client.find("posts", created.id, { locale: "en" })).content).toEqual(content);
	// ...and leaving the document asks first.
	await page
		.getByRole("navigation", { name: "Admin navigation" })
		.getByRole("link", { name: "Editorial notes", exact: true })
		.click();
	const leave = page.getByRole("dialog");
	await expect(leave.getByRole("heading", { name: "Leave without saving?" })).toBeVisible();
	await leave.getByRole("button", { name: "Keep editing" }).click();

	// Undoing the change restores a valid document and clears the report.
	await editor.press("ControlOrMeta+z");
	await expect(page.getByText(rejectedEdit)).toHaveCount(0);
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});

test("pasting a group discards a rejected edit it contains", async ({ page, adminServer }) => {
	const errors = observePageErrors(page);
	const client = await editorClient(page, adminServer.url);
	const body = numberedList("Item");
	const created = await client.create("editor-options", {
		title: "Replaced rich text",
		excerpt: { body },
	});
	await page.goto(`/admin/collections/editor-options/${created.id}`);
	const excerpt = page.locator('[data-field-path="excerpt"]');
	const editor = page.getByRole("textbox", { name: "Excerpt body" });
	await expect(editor.getByText("Item", { exact: true })).toBeVisible();

	// The rejected edit changes only the editor, so the copy still equals the form value.
	await excerpt.getByRole("button", { name: "Open Excerpt actions" }).click();
	await page.getByRole("menuitem", { name: "Copy field" }).click();
	await rejectNextExport(editor);
	await editor.getByText("Item", { exact: true }).click({ clickCount: 3 });
	await page.keyboard.type("Rejected");
	await expect(editor.getByText("Rejected", { exact: true })).toBeVisible();
	await expect(page.getByText(rejectedEdit).first()).toBeVisible();
	await excerpt.getByRole("button", { name: "Open Excerpt actions" }).click();
	await page.getByRole("menuitem", { name: "Paste field" }).click();
	await expect(editor.getByText("Item", { exact: true })).toBeVisible();
	await expect(editor.getByText("Rejected", { exact: true })).toHaveCount(0);
	await expect(page.getByText(rejectedEdit)).toHaveCount(0);

	// Nothing from the discarded editor blocks saving.
	await page.getByRole("textbox", { name: "Title", exact: true }).fill("Saved after paste");
	await documentSaveButton(page).click();
	await expect
		.poll(async () => (await client.find("editor-options", created.id)).title)
		.toBe("Saved after paste");
	expect((await client.find("editor-options", created.id)).excerpt).toEqual({ body });
	await expect(page.getByText(rejectedEdit)).toHaveCount(0);
	expect(errors.pageErrors).toEqual([]);
	expect(errors.consoleErrors).toEqual([]);
});
