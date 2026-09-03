import { buildEditorState, type DefaultNodeTypes } from "@payloadcms/richtext-lexical";
import type { File, Payload } from "payload";

import { roles, slugs } from "./shared";

const fixtureImageBase64 =
	"iVBORw0KGgoAAAANSUhEUgAAABQAAAAUCAYAAACNiR0NAAAACXBIWXMAAAsTAAALEwEAmpwYAAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAOdEVYdFNvZnR3YXJlAEZpZ21hnrGWYwAAAQtJREFUeAGllIERgjAMRYMTOELdwBFwA90AJ4ENHAGcQDZwBEeADXCD+CvlLCGFgv/uH4W2r21oQrQgZt7DpbOhrXKgHO74J9vOaa0w6Qg3HJbty9YADVx4k1P4rCzyUMPgHS+VYGVspoDL0Vi8VMHO8AkqCQ0BB+V25xHgzgfuZsYX8Gsu+EmStHi8/W8a8AK3rm3g0sXMUIR2yqo1fEDzKsBR0OCRAa3wOMF3WqG5GH5jBGdo2h3X9C9QgC/uJ0j5oeklro2hDRrmaTtsli63y6ybf6VGuw/c/EllYb0CNcEEcOCHANsJNn9TnubwEz5SRDy0AiAXSWmtuC9hssAWvJDjS9AhvmUM6APCbObLQ83cDQAAAABJRU5ErkJggg==";

const fixtureImage = (name: string): File => {
	const data = Buffer.from(fixtureImageBase64, "base64");
	return { data, mimetype: "image/png", name, size: data.length };
};

const richDocument = (text: string) => buildEditorState<DefaultNodeTypes>({ text });

export const seed = async (payload: Payload): Promise<void> => {
	const existing = await payload.find({
		collection: slugs.users,
		limit: 1,
		overrideAccess: true,
		where: { email: { equals: "admin@riducms.test" } },
	});

	if (existing.totalDocs > 0) {
		await seedFormBuilder(payload);
		return;
	}

	const administrator = await payload.create({
		collection: slugs.users,
		overrideAccess: true,
		data: {
			email: "admin@riducms.test",
			password: "ridu-admin",
			name: "Payload Administrator",
			contactEmail: "admin@example.test",
			role: roles.administrator,
			privateNotes: "Can see protected fields and perform destructive operations.",
			profile: {
				availableForReview: true,
				bio: "Maintains the Payload side of the parity fixture.",
				location: "London",
				timezone: "Europe/London",
			},
		},
	});
	const editor = await payload.create({
		collection: slugs.users,
		overrideAccess: true,
		data: {
			email: "editor@riducms.test",
			password: "ridu-browser",
			name: "Payload Editor",
			contactEmail: "editor@example.test",
			role: roles.editor,
			profile: {
				availableForReview: true,
				bio: "Reviews and publishes the fixture content.",
				location: "Bristol",
				timezone: "Europe/London",
			},
		},
	});
	const contributor = await payload.create({
		collection: slugs.users,
		overrideAccess: true,
		data: {
			email: "demo@riducms.local",
			password: "ridu-demo",
			name: "Demo Author",
			contactEmail: "author@example.test",
			role: roles.contributor,
			profile: {
				availableForReview: false,
				bio: "Can edit only their own content.",
				location: "Cardiff",
				timezone: "Europe/London",
			},
		},
	});

	const news = await payload.create({
		collection: slugs.categories,
		overrideAccess: true,
		data: {
			name: "News",
			slug: "news",
			seo: { description: "Product and framework updates.", title: "Payload parity news" },
		},
	});
	const guides = await payload.create({
		collection: slugs.categories,
		overrideAccess: true,
		data: { name: "Guides", slug: "guides" },
	});

	const cover = await payload.create({
		collection: slugs.media,
		overrideAccess: true,
		file: fixtureImage("payload-parity-cover.png"),
		data: {
			alt: "Payload parity cover",
			caption: "A deterministic image reused by the Payload mirror.",
			credit: editor.id,
			kind: "illustration",
			tags: [{ label: "Payload" }],
		},
	});
	const fieldNotes = await payload.create({
		collection: slugs.media,
		overrideAccess: true,
		file: fixtureImage("payload-field-notes.png"),
		data: {
			alt: "Payload field notes cover",
			credit: contributor.id,
			kind: "screenshot",
		},
	});

	const draftPost = await payload.create({
		collection: slugs.posts,
		draft: true,
		overrideAccess: true,
		data: {
			author: contributor.id,
			category: guides.id,
			collaborators: [editor.id],
			content: richDocument(
				"Relationship selection should respect access rules and hydrate labels."
			),
			cover: fieldNotes.id,
			featured: false,
			layout: [
				{
					blockType: "callout",
					body: "This row proves blocks preserve a stable ID and discriminator.",
					tone: "note",
				},
			],
			links: [
				{
					label: "Payload parity roadmap",
					newWindow: false,
					url: "/docs/internals/payload-parity",
				},
			],
			metadata: { fixture: true, priority: 2 },
			readingMinutes: 4,
			slug: "relationship-field-notes",
			status: "draft",
			summary: "A contributor-owned draft used to verify filtered collection access.",
			title: "  Relationship field notes  ",
		},
	});

	const publishedPost = await payload.create({
		collection: slugs.posts,
		draft: false,
		overrideAccess: true,
		data: {
			_status: "published",
			author: editor.id,
			category: news.id,
			collaborators: [administrator.id, contributor.id],
			content: richDocument(
				"Payload keeps executable TypeScript configuration as the source of truth."
			),
			cover: cover.id,
			featured: true,
			gallery: [cover.id, fieldNotes.id],
			internalNotes: "This is deliberately redacted for non-administrators.",
			layout: [
				{ attribution: "Payload", blockType: "quote", quote: "Config is the product surface." },
				{ blockType: "related-posts", posts: [draftPost.id] },
			],
			publishDate: "2026-08-13T12:00:00.000Z",
			readingMinutes: 7,
			relatedPosts: [draftPost.id],
			seo: {
				canonicalURL: "https://example.test/welcome-to-payload",
				description: "See the same editorial model rendered by Payload.",
				socialImage: cover.id,
				title: "Payload admin fixture",
			},
			slug: "welcome-to-payload",
			status: "published",
			summary: "A broad browser contract for comparing Payload and Ridu.",
			title: "Welcome to Payload",
		},
	});

	const page = await payload.create({
		collection: slugs.pages,
		draft: false,
		overrideAccess: true,
		data: {
			_status: "published",
			layout: [
				{
					blockType: "hero",
					heading: "A TypeScript CMS with a React admin",
					image: cover.id,
					lede: "The behavioral reference for Ridu parity work.",
				},
				{
					blockType: "rich-text",
					body: richDocument("This rich-text field is nested inside a block."),
				},
				{ blockType: "featured-post", post: publishedPost.id },
			],
			navigation: { label: "About", showInFooter: true, showInHeader: true },
			slug: "about",
			template: "landing",
			title: "About Payload parity",
		},
	});

	await payload.update({
		id: publishedPost.id,
		collection: slugs.posts,
		overrideAccess: true,
		data: {
			relatedContent: { relationTo: slugs.pages, value: page.id },
		},
	});
	await seedFormBuilder(payload);

	await payload.create({
		collection: slugs.events,
		overrideAccess: true,
		data: {
			capacity: 24,
			contact: "events@example.test",
			endsAt: "2026-09-15T16:30:00.000Z",
			name: "Payload contributor workshop",
			online: false,
			registrationSettings: { reminders: [7, 1], waitlist: true },
			schedule: [
				{ speaker: editor.id, time: "2026-09-15T09:30:00.000Z", title: "Schema tour" },
				{ speaker: administrator.id, time: "2026-09-15T11:00:00.000Z", title: "Admin gap audit" },
			],
			startsAt: "2026-09-15T09:30:00.000Z",
			venue: "The contract test studio",
		},
	});

	await payload.create({
		collection: slugs.editorialNotes,
		overrideAccess: true,
		data: {
			confidentialDetails: "Field-level read access removes this value from editor responses.",
			note: "Editors can list this note but cannot see the protected details.",
			owner: administrator.id,
			title: "Administrator-only launch notes",
		},
	});
	await payload.create({
		collection: slugs.editorialNotes,
		overrideAccess: true,
		data: {
			note: "The contributor account sees this note through an owner predicate.",
			owner: contributor.id,
			title: "Contributor pitch",
		},
	});
	await payload.create({
		collection: slugs.redirects,
		overrideAccess: true,
		data: { enabled: true, from: "/old-about", to: "/about", type: "permanent" },
	});
	await payload.create({
		collection: slugs.payloadCapabilities,
		overrideAccess: true,
		data: {
			boundedRows: [{ label: "Array row limits and duplication controls" }],
			location: [-0.1276, 51.5072],
			minimum: 25,
			presentation: "article",
			sourceCode: "export const payloadOnly = true\n",
			title: "Payload reference admin surface",
		},
	});
	await payload.updateGlobal({
		slug: "site-settings",
		overrideAccess: true,
		data: {
			announcement: "Reference global used for side-by-side Ridu authoring review.",
			navigation: [{ label: "About", page: page.id }],
			siteName: "Payload parity reference",
		},
	});
};

const seedFormBuilder = async (payload: Payload): Promise<void> => {
	const existing = await payload.find({
		collection: "forms",
		limit: 1,
		overrideAccess: true,
		where: { title: { equals: "Contact Form" } },
	});
	if (existing.totalDocs > 0) return;

	const contactForm = await payload.create({
		collection: "forms",
		overrideAccess: true,
		data: {
			confirmationMessage: richDocument("Thanks — your message has been received."),
			confirmationType: "message",
			fields: [
				{ blockType: "text", label: "Name", name: "name", required: true, width: 50 },
				{ blockType: "email", label: "Email", name: "email", required: true, width: 50 },
				{
					blockType: "select",
					label: "Topic",
					name: "topic",
					options: [
						{ label: "Support", value: "support" },
						{ label: "Sales", value: "sales" },
					],
					placeholder: "Choose a topic",
				},
				{ blockType: "textarea", label: "Message", name: "message", required: true },
				{
					blockType: "message",
					message: richDocument("Attachments are optional. PNG images are accepted."),
				},
				{
					blockType: "upload",
					label: "Attachment",
					maxFileSize: 1_048_576,
					mimeTypes: [{ mimeType: "image/png" }],
					name: "attachment",
					uploadCollection: "media",
				},
			],
			submitButtonLabel: "Send message",
			title: "Contact Form",
		},
	});
	await payload.create({
		collection: "form-submissions",
		overrideAccess: true,
		data: {
			form: contactForm.id,
			submissionData: [
				{ field: "name", value: "Grace Hopper" },
				{ field: "email", value: "grace@example.test" },
				{ field: "topic", value: "support" },
				{ field: "message", value: "Please send the Payload form guide." },
			],
		},
	});
};
