import type { Access, CollectionConfig, Field, Where } from "payload";

import {
	allowAuthenticated,
	allowEveryone,
	allowFieldRoles,
	allowOwnedOrRoles,
	allowRoles,
	allowSelfOrRoles,
	hasRole,
	roles,
	slugs,
	staffManagedPublicRead,
} from "./shared";

const adminComponent = (exportName: string) => `/admin-components#${exportName}`;

const seoTab = (): Field => ({
	type: "tabs",
	tabs: [
		{
			fields: [
				{
					type: "row",
					fields: [
						{ name: "title", type: "text", admin: { width: "50%" } },
						{ name: "canonicalURL", type: "text", admin: { width: "50%" } },
					],
				},
				{ name: "description", type: "textarea" },
				{ name: "socialImage", type: "upload", relationTo: slugs.media },
			],
			label: "SEO",
			name: "seo",
		},
	],
});

export const Users: CollectionConfig = {
	slug: slugs.users,
	labels: { plural: "Users", singular: "User" },
	admin: {
		group: "Access control",
		useAsTitle: "name",
	},
	auth: {
		tokenExpiration: 8 * 60 * 60,
	},
	access: {
		admin: ({ req }) => Boolean(req.user),
		create: allowRoles(roles.administrator, roles.editor),
		delete: allowRoles(roles.administrator),
		read: allowAuthenticated,
		update: allowSelfOrRoles(roles.administrator, roles.editor),
	},
	fields: [
		{
			name: "name",
			type: "text",
			required: true,
			admin: { description: "The name shown on authored content." },
		},
		{
			type: "row",
			fields: [
				{
					name: "role",
					type: "select",
					access: {
						create: ({ req, siblingData }) => {
							return (
								hasRole(req.user, roles.administrator) ||
								(hasRole(req.user, roles.editor) && siblingData?.role === roles.contributor)
							);
						},
						update: ({ req }) => hasRole(req.user, roles.administrator),
					},
					admin: { width: "50%" },
					defaultValue: roles.contributor,
					options: [
						{ label: "Administrator", value: roles.administrator },
						{ label: "Editor", value: roles.editor },
						{ label: "Contributor", value: roles.contributor },
					],
					required: true,
				},
				{
					name: "contactEmail",
					type: "email",
					label: "Public contact email",
					admin: { width: "50%" },
				},
			],
		},
		{
			type: "tabs",
			tabs: [
				{
					fields: [
						{
							name: "bio",
							type: "textarea",
							admin: { description: "Short biography displayed on author pages." },
						},
						{
							type: "row",
							fields: [
								{ name: "location", type: "text", admin: { width: "50%" } },
								{
									name: "timezone",
									type: "text",
									defaultValue: "Europe/London",
									admin: { width: "50%" },
								},
							],
						},
						{
							name: "availableForReview",
							type: "checkbox",
							label: "Available for review",
							defaultValue: true,
						},
					],
					label: "Profile",
					name: "profile",
				},
				{
					fields: [
						{
							name: "privateNotes",
							type: "textarea",
							access: {
								create: allowFieldRoles(roles.administrator),
								read: allowFieldRoles(roles.administrator),
								update: allowFieldRoles(roles.administrator),
							},
							admin: { description: "Visible only to administrators." },
						},
					],
					label: "Security",
				},
			],
		},
	],
};

export const Media: CollectionConfig = {
	slug: slugs.media,
	labels: { plural: "Media", singular: "Asset" },
	admin: { group: "Content", useAsTitle: "filename" },
	upload: {
		imageSizes: [
			{ name: "card", width: 640, height: 360, fit: "cover" },
			{ name: "thumbnail", width: 160, height: 160, fit: "cover" },
		],
		mimeTypes: ["image/png", "image/jpeg"],
		staticDir: "media",
	},
	access: {
		create: allowRoles(roles.administrator, roles.editor),
		delete: allowRoles(roles.administrator),
		read: allowEveryone,
		update: allowRoles(roles.administrator, roles.editor),
	},
	fields: [
		{
			name: "alt",
			type: "text",
			label: "Alt text",
			required: true,
			admin: { description: "Describe the image for people who cannot see it." },
		},
		{ name: "caption", type: "textarea" },
		{
			type: "row",
			fields: [
				{ name: "credit", type: "relationship", relationTo: slugs.users, admin: { width: "50%" } },
				{
					name: "kind",
					type: "select",
					admin: { width: "50%" },
					defaultValue: "image",
					options: ["image", "illustration", "screenshot"],
				},
			],
		},
		{
			name: "tags",
			type: "array",
			fields: [{ name: "label", type: "text", required: true }],
		},
	],
};

export const Categories: CollectionConfig = {
	slug: slugs.categories,
	labels: { plural: "Categories", singular: "Category" },
	admin: { group: "Taxonomy", useAsTitle: "name" },
	access: staffManagedPublicRead,
	fields: [
		{ name: "name", type: "text", required: true },
		{ name: "slug", type: "text", required: true, unique: true },
		seoTab(),
		{
			name: "relatedPosts",
			type: "join",
			collection: slugs.posts,
			on: "category",
			admin: {
				description:
					"Payload can expose the inverse side of a relationship without storing IDs here.",
			},
		},
	],
};

const postRead: Access = ({ req }) => {
	const published: Where = { status: { equals: "published" } };

	if (hasRole(req.user, roles.administrator, roles.editor)) {
		return true;
	}

	if (req.user) {
		return {
			or: [published, { author: { equals: req.user.id } }],
		};
	}

	return published;
};

export const Posts: CollectionConfig = {
	slug: slugs.posts,
	labels: { plural: "Posts", singular: "Post" },
	admin: {
		components: {
			beforeList: [adminComponent("PostsListFrame")],
			edit: {
				beforeDocumentControls: [
					adminComponent("PostDocumentFrame"),
					adminComponent("PostReviewAction"),
				],
			},
			views: {
				edit: {
					insights: {
						Component: adminComponent("PostInsightsView"),
						path: "/insights",
						tab: { label: "Insights", order: 150 },
					},
				},
			},
		},
		defaultColumns: ["title", "status", "author", "category", "updatedAt"],
		group: "Content",
		livePreview: {
			url: ({ data }) =>
				`http://localhost:3000/preview?collection=posts&slug=${encodeURIComponent(String(data?.slug ?? "draft"))}`,
		},
		useAsTitle: "title",
	},
	access: {
		create: allowAuthenticated,
		delete: allowOwnedOrRoles("author", roles.administrator),
		read: postRead,
		readVersions: postRead,
		update: allowOwnedOrRoles("author", roles.administrator, roles.editor),
	},
	versions: {
		drafts: { autosave: { interval: 15_000 }, schedulePublish: true },
		maxPerDoc: 20,
	},
	hooks: {
		beforeValidate: [
			({ data, operation, req }) => {
				if (data && req.user && (operation === "create" || operation === "update")) {
					data.lastEditedBy = req.user.id;
				}
				return data;
			},
		],
	},
	fields: [
		{
			name: "title",
			type: "text",
			required: true,
			hooks: {
				beforeValidate: [({ value }) => (typeof value === "string" ? value.trim() : value)],
			},
		},
		{ name: "slug", type: "text", unique: true },
		{ name: "summary", type: "textarea", required: true },
		{
			type: "row",
			fields: [
				{ name: "author", type: "relationship", relationTo: slugs.users, admin: { width: "50%" } },
				{
					name: "category",
					type: "relationship",
					relationTo: slugs.categories,
					admin: { width: "50%" },
				},
			],
		},
		{
			type: "row",
			fields: [
				{
					name: "status",
					type: "select",
					access: {
						create: ({ req, siblingData }) => {
							return (
								hasRole(req.user, roles.administrator, roles.editor) ||
								siblingData?.status === "draft"
							);
						},
						update: allowFieldRoles(roles.administrator, roles.editor),
					},
					admin: { width: "33%" },
					defaultValue: "draft",
					options: ["draft", "published"],
				},
				{ name: "featured", type: "checkbox", defaultValue: false, admin: { width: "33%" } },
				{
					name: "readingMinutes",
					type: "number",
					label: "Reading time (minutes)",
					defaultValue: 5,
					admin: {
						components: { Cell: adminComponent("ReadingTimeCell") },
						width: "33%",
					},
				},
			],
		},
		{
			name: "publishDate",
			type: "date",
			label: "Publication date",
			admin: {
				condition: (_, siblingData) => siblingData?.status === "published",
				date: { pickerAppearance: "dayAndTime" },
			},
		},
		{ name: "cover", type: "upload", relationTo: slugs.media },
		{ name: "gallery", type: "upload", relationTo: slugs.media, hasMany: true },
		{ name: "content", type: "richText" },
		{ name: "collaborators", type: "relationship", relationTo: slugs.users, hasMany: true },
		{ name: "relatedPosts", type: "relationship", relationTo: slugs.posts, hasMany: true },
		{ name: "relatedContent", type: "relationship", relationTo: [slugs.posts, slugs.pages] },
		seoTab(),
		{
			type: "tabs",
			tabs: [
				{
					label: "Related",
					fields: [
						{
							name: "links",
							type: "array",
							fields: [
								{
									type: "row",
									fields: [
										{ name: "label", type: "text", required: true, admin: { width: "42%" } },
										{ name: "url", type: "text", required: true, admin: { width: "58%" } },
									],
								},
								{
									name: "newWindow",
									type: "checkbox",
									label: "Open in a new window",
									defaultValue: false,
								},
							],
						},
					],
				},
				{
					label: "Layout",
					fields: [
						{
							name: "layout",
							type: "blocks",
							blocks: [
								{
									slug: "callout",
									labels: { plural: "Callouts", singular: "Callout" },
									fields: [
										{
											name: "tone",
											type: "select",
											defaultValue: "note",
											options: ["note", "warning", "success"],
										},
										{ name: "body", type: "textarea", required: true },
									],
								},
								{
									slug: "quote",
									fields: [
										{ name: "quote", type: "textarea", required: true },
										{ name: "attribution", type: "text" },
									],
								},
								{
									slug: "related-posts",
									labels: { plural: "Related post blocks", singular: "Related posts" },
									fields: [
										{
											name: "posts",
											type: "relationship",
											relationTo: slugs.posts,
											hasMany: true,
											required: true,
										},
									],
								},
							],
						},
					],
				},
				{
					label: "Advanced",
					fields: [
						{ name: "metadata", type: "json" },
						{
							name: "internalNotes",
							type: "textarea",
							access: {
								create: allowFieldRoles(roles.administrator, roles.editor),
								read: allowFieldRoles(roles.administrator),
								update: allowFieldRoles(roles.administrator, roles.editor),
							},
						},
						{
							name: "lastEditedBy",
							type: "relationship",
							relationTo: slugs.users,
							admin: { readOnly: true },
						},
					],
				},
			],
		},
	],
};

export const Pages: CollectionConfig = {
	slug: slugs.pages,
	labels: { plural: "Pages", singular: "Page" },
	admin: { group: "Editorial", useAsTitle: "title" },
	access: {
		create: allowRoles(roles.administrator, roles.editor),
		delete: allowRoles(roles.administrator),
		read: allowEveryone,
		update: allowRoles(roles.administrator, roles.editor),
	},
	versions: { drafts: { autosave: { interval: 30_000 } }, maxPerDoc: 10 },
	fields: [
		{ name: "title", type: "text", required: true },
		{ name: "slug", type: "text", required: true, unique: true },
		{
			name: "template",
			type: "select",
			defaultValue: "default",
			options: ["default", "landing", "legal"],
		},
		{
			name: "layout",
			type: "blocks",
			required: true,
			blocks: [
				{
					slug: "hero",
					fields: [
						{ name: "heading", type: "text", required: true },
						{ name: "lede", type: "textarea" },
						{ name: "image", type: "upload", relationTo: slugs.media },
					],
				},
				{ slug: "rich-text", fields: [{ name: "body", type: "richText", required: true }] },
				{
					slug: "featured-post",
					fields: [{ name: "post", type: "relationship", relationTo: slugs.posts, required: true }],
				},
			],
		},
		{
			type: "tabs",
			tabs: [
				{
					label: "Navigation",
					name: "navigation",
					fields: [
						{ name: "label", type: "text" },
						{
							name: "showInHeader",
							type: "checkbox",
							label: "Show in header",
							defaultValue: false,
						},
						{
							name: "showInFooter",
							type: "checkbox",
							label: "Show in footer",
							defaultValue: false,
						},
					],
				},
			],
		},
	],
};

export const Events: CollectionConfig = {
	slug: slugs.events,
	labels: { plural: "Events", singular: "Event" },
	admin: { group: "Engagement", useAsTitle: "name" },
	access: staffManagedPublicRead,
	fields: [
		{ name: "name", type: "text", required: true },
		{
			type: "row",
			fields: [
				{
					name: "startsAt",
					type: "date",
					required: true,
					admin: { width: "50%", date: { pickerAppearance: "dayAndTime" } },
				},
				{
					name: "endsAt",
					type: "date",
					admin: { width: "50%", date: { pickerAppearance: "dayAndTime" } },
				},
			],
		},
		{
			type: "row",
			fields: [
				{ name: "capacity", type: "number", defaultValue: 50, admin: { width: "50%" } },
				{ name: "online", type: "checkbox", defaultValue: false, admin: { width: "50%" } },
			],
		},
		{ name: "venue", type: "text", admin: { condition: (_, siblingData) => !siblingData?.online } },
		{
			name: "meetingURL",
			type: "text",
			label: "Meeting URL",
			admin: { condition: (_, siblingData) => Boolean(siblingData?.online) },
		},
		{ name: "contact", type: "email", label: "Contact email", required: true },
		{
			name: "schedule",
			type: "array",
			fields: [
				{
					name: "time",
					type: "date",
					required: true,
					admin: { date: { pickerAppearance: "dayAndTime" } },
				},
				{ name: "title", type: "text", required: true },
				{ name: "speaker", type: "relationship", relationTo: slugs.users },
			],
		},
		{ name: "registrationSettings", type: "json" },
	],
};

export const EditorialNotes: CollectionConfig = {
	slug: slugs.editorialNotes,
	labels: { plural: "Editorial notes", singular: "Editorial note" },
	admin: { group: "Workflow", useAsTitle: "title" },
	access: {
		create: allowAuthenticated,
		delete: allowOwnedOrRoles("owner", roles.administrator),
		read: allowOwnedOrRoles("owner", roles.administrator, roles.editor),
		update: allowOwnedOrRoles("owner", roles.administrator, roles.editor),
	},
	fields: [
		{ name: "title", type: "text", required: true },
		{ name: "owner", type: "relationship", relationTo: slugs.users, required: true },
		{ name: "note", type: "textarea", required: true },
		{
			name: "confidentialDetails",
			type: "textarea",
			access: {
				create: allowFieldRoles(roles.administrator),
				read: allowFieldRoles(roles.administrator),
				update: allowFieldRoles(roles.administrator),
			},
		},
	],
};

export const Redirects: CollectionConfig = {
	slug: slugs.redirects,
	labels: { plural: "Redirects", singular: "Redirect" },
	admin: { group: "Operations", useAsTitle: "from" },
	access: {
		create: allowRoles(roles.administrator, roles.editor),
		delete: allowRoles(roles.administrator),
		read: allowRoles(roles.administrator, roles.editor),
		update: allowRoles(roles.administrator, roles.editor),
	},
	fields: [
		{ name: "from", type: "text", label: "From path", required: true, unique: true },
		{ name: "to", type: "text", label: "Destination", required: true },
		{
			name: "type",
			type: "select",
			defaultValue: "permanent",
			options: ["permanent", "temporary"],
		},
		{ name: "enabled", type: "checkbox", defaultValue: true },
	],
};

export const PayloadCapabilities: CollectionConfig = {
	slug: slugs.payloadCapabilities,
	labels: { plural: "Payload reference surface", singular: "Payload reference document" },
	admin: {
		description:
			"A stable Payload field and collection reference used for side-by-side Ridu review.",
		group: "Comparison reference",
		useAsTitle: "title",
	},
	lockDocuments: { duration: 120 },
	trash: true,
	fields: [
		{ name: "title", type: "text", required: true, localized: true },
		{
			name: "presentation",
			type: "radio",
			options: ["article", "gallery", "video"],
			admin: {
				description: "Top-level fields can be placed in Payload's document sidebar.",
				position: "sidebar",
			},
		},
		{
			name: "location",
			type: "point",
			admin: {
				description: "Complex field types can use the sidebar position too.",
				position: "sidebar",
			},
		},
		{ name: "sourceCode", type: "code", admin: { language: "typescript" } },
		{
			type: "collapsible",
			label: "Payload collapsible layout",
			admin: { initCollapsed: true },
			fields: [
				{ name: "minimum", type: "number", min: 0, max: 100 },
				{
					name: "boundedRows",
					type: "array",
					minRows: 1,
					maxRows: 3,
					fields: [{ name: "label", type: "text", required: true }],
				},
			],
		},
		{
			name: "computedLabel",
			type: "text",
			virtual: true,
			hooks: { afterRead: [({ data }) => `Computed from ${String(data?.title ?? "untitled")}`] },
			admin: {
				description: "Virtual and read-only fields can share the sidebar with editable fields.",
				position: "sidebar",
				readOnly: true,
			},
		},
	],
};

export const collections: CollectionConfig[] = [
	Users,
	Media,
	Categories,
	Posts,
	Pages,
	Events,
	EditorialNotes,
	Redirects,
	PayloadCapabilities,
];
