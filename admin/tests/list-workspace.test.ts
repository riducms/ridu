import { describe, expect, test } from "bun:test";

import type { SchemaCollection, SchemaField } from "@riducms/protocol";

import { PreferenceWriteQueue } from "@admin/core/preferences/preference-write-queue";
import {
	buildListFilterWhere,
	defaultListColumns,
	filterableFields,
	filterOperatorsFor,
	listColumnFields,
	normalizeWorkspacePreference,
	parseListFilters,
	parseListPageSize,
	sortableField,
} from "@admin/features/collections/list-workspace";

const fields = [
	{ name: "title", path: "title", type: "text", admin: { label: "Title" } },
	{ name: "capacity", path: "capacity", type: "number", admin: { label: "Capacity" } },
	{ name: "online", path: "online", type: "checkbox", admin: { label: "Online" } },
	{ name: "status", path: "status", type: "select", admin: { label: "Status" } },
] as SchemaField[];

describe("collection list workspace", () => {
	test("decodes typed filters into the existing REST where shape", () => {
		const filters = parseListFilters(
			JSON.stringify([
				{ field: "title", operator: "like", value: "launch" },
				{ field: "capacity", operator: "greaterThanEqual", value: 50 },
				{ field: "online", operator: "equals", value: "true" },
				{ field: "missing", operator: "equals", value: "ignored" },
			]),
			fields
		);
		expect(buildListFilterWhere(filters, fields)).toEqual([
			{ title: { like: "launch" } },
			{ capacity: { greaterThanEqual: 50 } },
			{ online: { equals: true } },
		]);
	});

	test("offers operators that match each field's wire value", () => {
		expect(filterOperatorsFor(fields[0]!)).toContain("like");
		expect(filterOperatorsFor(fields[1]!)).toContain("greaterThan");
		expect(filterOperatorsFor(fields[2]!)).toEqual(["equals", "notEquals", "exists"]);
	});

	test("offers group leaves as columns and repeated leaves as filters", () => {
		const candidates = [
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
				blocks: {
					types: [
						{
							slug: "hero",
							labels: { singular: "Hero", plural: "Heroes" },
							fields: [
								{
									name: "heading",
									path: "layout.hero.heading",
									type: "text",
									admin: { label: "Heading" },
								},
							],
						},
					],
				},
			},
			{
				name: "content",
				path: "content",
				type: "json",
				admin: { label: "Content" },
				plugin: { key: "richtext", config: {} },
			},
		] as SchemaField[];
		expect(
			listColumnFields({ fields: candidates } as SchemaCollection, "title").map(
				(field) => field.path
			)
		).toEqual(["capacity", "online", "status", "seo.description", "links", "layout", "content"]);
		expect(filterableFields(candidates).map((field) => field.path)).toEqual([
			"title",
			"capacity",
			"online",
			"status",
			"seo.description",
			"links.label",
			"layout.hero.heading",
		]);
		expect(filterableFields(candidates).map((field) => field.admin.label)).toContain(
			"Layout > Hero > Heading"
		);
	});

	test("normalizes persisted columns and bounded page sizes", () => {
		const collection = {
			admin: { defaultColumns: ["title", "status", "capacity"] },
		} as SchemaCollection;
		expect(defaultListColumns(collection, fields.slice(1))).toEqual(["status", "capacity"]);
		expect(parseListPageSize("100")).toBe(100);
		expect(parseListPageSize("1000")).toBe(25);
		expect(
			normalizeWorkspacePreference(
				{
					columns: ["capacity", "missing"],
					showStatus: false,
					showID: true,
					showCreated: true,
					showUpdated: true,
					limit: 50,
				},
				fields,
				{
					columns: [],
					showStatus: true,
					showID: false,
					showCreated: false,
					showUpdated: true,
					limit: 25,
				}
			)
		).toEqual({
			columns: ["capacity"],
			showStatus: false,
			showID: true,
			showCreated: true,
			showUpdated: true,
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
	expect(filterableFields([protectedTitle, group, fields[1]!]).map((field) => field.path)).toEqual([
		"capacity",
	]);
	const columns = listColumnFields({ fields: [protectedTitle, group] } as SchemaCollection);
	expect(columns.map((field) => field.path)).toEqual(["title", "meta.title"]);
	expect(columns.every((field) => !sortableField(field))).toBe(true);
});
