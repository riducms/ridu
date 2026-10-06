import { describe, expect, test } from "bun:test";

import type { SchemaBlockType, SchemaCollection, SchemaField } from "@riducms/protocol";
import { createAdminI18n } from "@riducms/translations";

import { PreferenceWriteQueue } from "@admin/core/preferences/preference-write-queue";
import { ListFilterFields } from "@admin/features/collections/list-filter-fields";
import {
	bulkEditableListFields,
	defaultListColumns,
	filterOperatorsFor,
	listColumnFields,
	listFilterComplete,
	normalizeWorkspacePreference,
	parseListFilters,
	parseListPageSize,
	parseReferenceCandidate,
	referenceCandidate,
	sortableField,
	type ListFilterOperator,
} from "@admin/features/collections/list-workspace";

import { bindBlockFields } from "./block-manifest";

const fields = [
	{ name: "title", path: "title", type: "text", admin: { label: "Title" } },
	{ name: "capacity", path: "capacity", type: "number", admin: { label: "Capacity" } },
	{ name: "online", path: "online", type: "checkbox", admin: { label: "Online" } },
	{ name: "status", path: "status", type: "select", admin: { label: "Status" } },
] as SchemaField[];

const filterFields = (candidates: readonly SchemaField[]) =>
	new ListFilterFields({ fields: candidates, metadata: [], i18n: createAdminI18n() });

describe("collection list workspace", () => {
	test("limits scalar bulk editing to values represented by the bulk editor", () => {
		const scalar: SchemaField = {
			...fields[3]!,
			id: "status",
			category: "scalar",
			required: false,
			unique: false,
			select: { options: [], hasMany: false },
		};
		expect(
			bulkEditableListFields([
				scalar,
				{
					...scalar,
					id: "tags",
					name: "tags",
					path: "tags",
					select: { options: [], hasMany: true },
				},
				{ ...scalar, id: "localized", name: "localized", path: "localized", localized: true },
				{
					...scalar,
					id: "readonly",
					name: "readonly",
					path: "readonly",
					admin: { label: "Readonly", readOnly: true },
				},
				{
					...scalar,
					id: "unique",
					name: "unique",
					path: "unique",
					unique: true,
				},
				{
					...scalar,
					id: "hidden",
					name: "hidden",
					path: "hidden",
					admin: { label: "Hidden", hidden: true },
				},
				{
					...scalar,
					id: "virtual",
					name: "virtual",
					path: "virtual",
					virtual: { valueType: "string" },
				},
				{
					...scalar,
					id: "conditional",
					name: "conditional",
					path: "conditional",
					admin: {
						label: "Conditional",
						condition: {
							kind: "predicate",
							predicate: {
								scope: "document",
								path: "status",
								operator: "equals",
								values: [{ type: "string", value: "published" }],
							},
						},
					},
				},
			]).map((field) => field.path)
		).toEqual(["status"]);
	});

	test("normalizes URL filters for the filter controls", () => {
		const filters = parseListFilters(
			JSON.stringify([
				[
					{ field: "title", operator: "like", value: "launch" },
					{ field: "capacity", operator: "greaterThanEqual", value: 50 },
					{ field: "online", operator: "equals", value: "true" },
					{ field: "missing", operator: "equals", value: "ignored" },
				],
			]),
			filterFields(fields)
		);
		expect(filters).toEqual([
			[
				{ field: "title", operator: "like", value: "launch" },
				{ field: "capacity", operator: "greaterThanEqual", value: "50" },
				{ field: "online", operator: "equals", value: "true" },
			],
		]);
	});

	test("offers operators that match each field's wire value", () => {
		expect(filterOperatorsFor(fields[0]!)).toContain("like");
		expect(filterOperatorsFor(fields[1]!)).toContain("greaterThan");
		expect(filterOperatorsFor(fields[2]!)).toEqual(["equals", "notEquals", "exists"]);
	});

	test("filters set-valued fields by membership rather than equality", () => {
		const reference = (name: string, relationship: SchemaField["relationship"]) =>
			({
				name,
				path: name,
				type: "relationship",
				admin: { label: name },
				relationship,
			}) as SchemaField;
		const tags = {
			...fields[3]!,
			name: "tags",
			path: "tags",
			select: { hasMany: true, options: [] },
		} as SchemaField;
		const authors = reference("authors", { hasMany: true, onDelete: "nullify" });
		const subject = reference("subject", {
			polymorphic: true,
			targets: [{ collectionId: "people", collectionSlug: "people" }],
			onDelete: "nullify",
		});
		const author = reference("author", { onDelete: "nullify" });
		const gallery = {
			name: "gallery",
			path: "gallery",
			type: "upload",
			admin: { label: "Gallery" },
			upload: {
				collectionId: "media",
				collectionSlug: "media",
				hasMany: true,
				onDelete: "nullify",
			},
		} as SchemaField;
		for (const field of [tags, authors, subject, gallery])
			expect(filterOperatorsFor(field)).toEqual(["in", "notIn", "exists"]);
		expect(filterOperatorsFor(author)).toEqual(["equals", "notEquals", "exists"]);

		const lookup = filterFields([tags, authors, subject]);
		expect(
			parseListFilters(
				JSON.stringify([
					[
						{ field: "tags", operator: "in", value: ["news", "tech"] },
						{ field: "authors", operator: "notIn", value: "ada" },
						{ field: "subject", operator: "in", value: ["people:ada"] },
						{ field: "authors", operator: "equals", value: "ada" },
					],
				]),
				lookup
			)
		).toEqual([
			[
				{ field: "tags", operator: "in", value: ["news", "tech"] },
				{ field: "authors", operator: "notIn", value: ["ada"] },
				{ field: "subject", operator: "in", value: ["people:ada"] },
			],
		]);
		expect(parseReferenceCandidate(referenceCandidate("people", "a:b"))).toEqual({
			relationTo: "people",
			id: "a:b",
		});
		const draft = (value: string | string[], operator: ListFilterOperator = "in") => ({
			field: "subject",
			operator,
			value,
		});
		expect(listFilterComplete(draft(["people:ada"]), subject)).toBe(true);
		expect(listFilterComplete(draft(["people:"]), subject)).toBe(false);
		expect(listFilterComplete(draft([]), authors)).toBe(false);
		expect(listFilterComplete(draft("true", "exists"), authors)).toBe(true);
	});

	test("offers group leaves as columns and repeated leaves as filters", () => {
		const hero = {
			slug: "hero",
			labels: { singular: "Hero", plural: "Heroes" },
			fields: [{ name: "heading", path: "heading", type: "text", admin: { label: "Heading" } }],
		} as SchemaBlockType;
		const candidates = bindBlockFields([hero], [
			...fields,
			{
				name: "seo",
				path: "seo",
				type: "group",
				admin: { label: "SEO" },
				nested: {
					fields: [
						{
							name: "description",
							path: "seo.description",
							type: "textarea",
							admin: { label: "Description" },
						},
					],
				},
			},
			{
				name: "links",
				path: "links",
				type: "array",
				admin: { label: "Links" },
				nested: {
					fields: [{ name: "label", path: "links.label", type: "text", admin: { label: "Label" } }],
				},
			},
			{
				name: "layout",
				path: "layout",
				type: "blocks",
				admin: { label: "Layout" },
				blocks: { blockReferences: ["hero"] },
			},
			{
				name: "content",
				path: "content",
				type: "json",
				admin: { label: "Content" },
				plugin: { key: "richtext", config: {} },
			},
		] as SchemaField[]);
		expect(
			listColumnFields({ fields: candidates } as SchemaCollection).map((field) => field.path)
		).toEqual([
			"title",
			"capacity",
			"online",
			"status",
			"seo.description",
			"links",
			"layout",
			"content",
		]);
		const filters = filterFields(candidates);
		expect(filters.level("")!.entries.map((entry) => [entry.kind, entry.path])).toEqual([
			["field", "title"],
			["field", "capacity"],
			["field", "online"],
			["field", "status"],
			["group", "seo"],
			["array", "links"],
			["blocks", "layout"],
		]);
		for (const path of ["seo.description", "links.label", "layout.hero.heading"])
			expect(filters.resolve(path)?.field.path).toBe(path);
		expect(filters.resolve("layout.hero.heading")?.trail).toEqual(["Layout", "Hero", "Heading"]);
		expect(filters.resolve("content")).toBeUndefined();
	});

	test("normalizes persisted columns and bounded page sizes", () => {
		const collection = {
			admin: { defaultColumns: ["title", "status", "capacity"] },
		} as SchemaCollection;
		expect(
			defaultListColumns(
				collection,
				fields.slice(1).map((field) => ({ path: field.path, label: field.admin.label, field }))
			)
		).toEqual(["status", "capacity"]);
		expect(parseListPageSize("100")).toBe(100);
		expect(parseListPageSize("1000")).toBe(10);
		expect(
			normalizeWorkspacePreference(
				{
					columns: [
						{ path: "capacity", active: true },
						{ path: "missing", active: true },
					],
					limit: 50,
				},
				fields,
				{
					columns: [],
					limit: 25,
				}
			)
		).toEqual({
			columns: [
				{ path: "capacity", active: true },
				{ path: "title", active: false },
				{ path: "online", active: false },
				{ path: "status", active: false },
			],
			limit: 50,
		});
	});

	test("orders preference writes by stored key", async () => {
		const queue = new PreferenceWriteQueue();
		const calls: string[] = [];
		let releaseFirst!: () => void;
		let markFirstStarted!: () => void;
		const firstRelease = new Promise<void>((resolve) => (releaseFirst = resolve));
		const firstStarted = new Promise<void>((resolve) => (markFirstStarted = resolve));
		const first = queue.enqueue(
			"posts",
			"users:editor",
			() => true,
			async () => {
				calls.push("first");
				markFirstStarted();
				await firstRelease;
				return "first";
			}
		);
		await firstStarted;
		const second = queue.enqueue(
			"posts",
			"users:editor",
			() => true,
			async () => {
				calls.push("second");
				return "second";
			}
		);
		await Promise.resolve();
		expect(calls).toEqual(["first"]);
		releaseFirst();
		expect(await first).toEqual({ dispatched: true, value: "first" });
		expect(await second).toEqual({ dispatched: true, value: "second" });
		expect(calls).toEqual(["first", "second"]);
	});

	test("drops queued writes when the originating session no longer owns them", async () => {
		const queue = new PreferenceWriteQueue();
		let sessionID = "editor-session";
		let writes = 0;
		let releaseFirst!: () => void;
		let markFirstStarted!: () => void;
		const firstRelease = new Promise<void>((resolve) => (releaseFirst = resolve));
		const firstStarted = new Promise<void>((resolve) => (markFirstStarted = resolve));
		const ownsWrite = () => sessionID === "editor-session";
		const first = queue.enqueue("posts", "users:editor", ownsWrite, async () => {
			writes += 1;
			markFirstStarted();
			await firstRelease;
			return "first";
		});
		await firstStarted;
		const second = queue.enqueue("posts", "users:editor", ownsWrite, async () => {
			writes += 1;
			return "second";
		});
		sessionID = "administrator-session";
		releaseFirst();
		expect(await first).toEqual({ dispatched: true, value: "first" });
		expect(await second).toEqual({ dispatched: false });
		expect(writes).toBe(1);
	});

	test("lets same-session writes finish after their route owner unmounts", async () => {
		const queue = new PreferenceWriteQueue();
		let sessionID = "editor-session";
		const writes: string[] = [];
		const unsubscribe = queue.subscribe("posts", () => undefined);
		let releaseFirst!: () => void;
		let markFirstStarted!: () => void;
		const firstRelease = new Promise<void>((resolve) => (releaseFirst = resolve));
		const firstStarted = new Promise<void>((resolve) => (markFirstStarted = resolve));
		const ownsSession = () => sessionID === "editor-session";
		const first = queue.enqueue("posts", "users:editor", ownsSession, async () => {
			writes.push("first");
			markFirstStarted();
			await firstRelease;
			return "first";
		});
		await firstStarted;
		const second = queue.enqueue("posts", "users:editor", ownsSession, async () => {
			writes.push("second");
			return "second";
		});
		unsubscribe();
		releaseFirst();
		expect(await first).toEqual({ dispatched: true, value: "first" });
		expect(await second).toEqual({ dispatched: true, value: "second" });
		expect(writes).toEqual(["first", "second"]);
		sessionID = "administrator-session";
	});

	test("settles an owner's writes and does not replay historical values", async () => {
		const queue = new PreferenceWriteQueue();
		const observed: { pending: boolean; hasValue: boolean; value?: unknown }[] = [];
		let release!: () => void;
		const wait = new Promise<void>((resolve) => (release = resolve));
		const unsubscribe = queue.subscribe("posts", (state) => observed.push({ ...state }));
		const write = queue.enqueue(
			"posts",
			"users:editor",
			() => true,
			async () => {
				await wait;
				return "saved";
			}
		);
		let settled = false;
		const ownerSettled = queue.settleOwner("users:editor").then(() => (settled = true));
		await Promise.resolve();
		expect(settled).toBe(false);
		release();
		expect(await write).toEqual({ dispatched: true, value: "saved" });
		await ownerSettled;
		expect(observed.at(-1)).toEqual({ pending: false, hasValue: true, value: "saved" });
		unsubscribe();
		const remounted: { pending: boolean; hasValue: boolean; value?: unknown }[] = [];
		queue.subscribe("posts", (state) => remounted.push({ ...state }))();
		expect(remounted).toEqual([{ pending: false, hasValue: false, value: undefined }]);
	});

	test("serializes two sessions that persist preferences for the same actor", async () => {
		const queue = new PreferenceWriteQueue();
		let liveSessionID = "session-a";
		const calls: string[] = [];
		let releaseFirst!: () => void;
		let markFirstStarted!: () => void;
		const firstRelease = new Promise<void>((resolve) => (releaseFirst = resolve));
		const firstStarted = new Promise<void>((resolve) => (markFirstStarted = resolve));
		const first = queue.enqueue(
			"users:editor:posts",
			"users:editor",
			() => liveSessionID === "session-a",
			async () => {
				calls.push("session-a");
				markFirstStarted();
				await firstRelease;
				return "first";
			}
		);
		await firstStarted;
		liveSessionID = "session-b";
		const second = queue.enqueue(
			"users:editor:posts",
			"users:editor",
			() => liveSessionID === "session-b",
			async () => {
				calls.push("session-b");
				return "second";
			}
		);
		await Promise.resolve();
		expect(calls).toEqual(["session-a"]);
		releaseFirst();
		expect(await first).toEqual({ dispatched: true, value: "first" });
		expect(await second).toEqual({ dispatched: true, value: "second" });
		expect(calls).toEqual(["session-a", "session-b"]);
	});
});

test("attached read protection excludes queries while retaining readable list columns", () => {
	const protectedTitle = { ...fields[0]!, queryRestricted: true };
	const group = {
		...fields[0]!,
		name: "meta",
		path: "meta",
		type: "group",
		queryRestricted: true,
		nested: { fields: [{ ...fields[0]!, path: "meta.title" }] },
	} as SchemaField;
	const filters = filterFields([protectedTitle, group, fields[1]!]);
	expect(filters.level("")!.entries.map((entry) => entry.path)).toEqual(["capacity"]);
	expect(filters.resolve("meta.title")).toBeUndefined();
	expect(filters.level("meta")).toBeUndefined();
	const columns = listColumnFields({ fields: [protectedTitle, group] } as SchemaCollection);
	expect(columns.map((field) => field.path)).toEqual(["title", "meta.title"]);
	expect(columns.every((field) => !sortableField(field))).toBe(true);
});
