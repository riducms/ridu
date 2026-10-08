import type { RichTextDocument } from "@riducms/sdk/richtext";

/**
 * A document with every node the core editor opens, as Lexical saves it, rendered on the server
 * and in the editor.
 */
export const article: RichTextDocument = {
	version: 1,
	root: {
		type: "root",
		children: [
			{
				type: "heading",
				tag: "h2",
				children: [
					{ type: "text", text: "Every " },
					{ type: "text", text: "core", format: 1 },
					{ type: "text", text: " node" },
				],
			},
			{
				type: "paragraph",
				children: [
					{ type: "text", text: "Bold", format: 1 },
					{ type: "text", text: ", italic", format: 2 },
					{ type: "text", text: ", inline code", format: 16 },
					{ type: "text", text: " and " },
					{
						type: "link",
						url: "https://example.com",
						children: [{ type: "text", text: "a link" }],
					},
					{ type: "text", text: "." },
				],
			},
			{
				type: "list",
				listType: "bullet",
				tag: "ul",
				children: [
					{ type: "listitem", value: 1, children: [{ type: "text", text: "A bullet" }] },
					{
						type: "listitem",
						value: 2,
						children: [
							{
								type: "list",
								listType: "number",
								tag: "ol",
								children: [
									{
										type: "listitem",
										value: 1,
										// Lexical saves a list item's nesting depth as its indent.
										indent: 1,
										children: [{ type: "text", text: "Nested" }],
									},
								],
							},
						],
					},
				],
			},
			{
				type: "list",
				listType: "check",
				tag: "ul",
				children: [
					{ type: "listitem", value: 1, checked: true, children: [{ type: "text", text: "Done" }] },
					{
						type: "listitem",
						value: 2,
						checked: false,
						children: [{ type: "text", text: "To do" }],
					},
				],
			},
			{ type: "quote", children: [{ type: "text", text: "A quote" }] },
			{ type: "code", children: [{ type: "text", text: "const answer = 42;" }] },
			{ type: "horizontalrule" },
			{ type: "paragraph", format: "center", children: [{ type: "text", text: "Centered" }] },
			{ type: "paragraph", indent: 1, children: [{ type: "text", text: "Indented" }] },
			{ type: "paragraph", children: [] },
			{
				type: "paragraph",
				children: [{ type: "text", text: "Ends with a line break" }, { type: "linebreak" }],
			},
		],
	},
};
