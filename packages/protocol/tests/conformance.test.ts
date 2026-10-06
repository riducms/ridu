import { describe, expect, it } from "bun:test";

import {
	isAccessCapabilities,
	isRecord,
	isAdminCollectionListData,
	isAdminPreparedRouteData,
	isErrorEnvelope,
	isPageEnvelope,
	isUncountedPageEnvelope,
	isValidationIssue,
} from "../src";

describe("record guard", () => {
	it("rejects null, arrays, and primitive values", () => {
		for (const value of [null, undefined, [], "text", 0, true, Symbol("key"), () => ({})])
			expect(isRecord(value)).toBe(false);
	});

	it("checks only the object container, not its shape or prototype", () => {
		for (const value of [{}, { arbitrary: undefined }, Object.create(null), new Date(0)])
			expect(isRecord(value)).toBe(true);
	});
});

describe("access capabilities", () => {
	const operations = Object.fromEntries(
		[
			"admin",
			"create",
			"read",
			"readVersions",
			"update",
			"delete",
			"duplicate",
			"publish",
			"unpublish",
			"restoreDeleted",
			"deletePermanent",
			"selectAll",
		].map((key) => [key, true])
	);
	const allowed = { read: true, create: true, update: false };

	it("accepts block definition capabilities by slug and definition-relative path", () => {
		const access = {
			operations,
			fields: {},
			blockFields: { card: { "details.caption": allowed } },
		};
		expect(isAccessCapabilities(access)).toBe(true);
		expect(isAccessCapabilities({ operations, fields: { title: allowed } })).toBe(true);
	});

	it("rejects malformed block definition capabilities", () => {
		for (const blockFields of [[], { card: [] }, { card: { caption: { read: true } } }])
			expect(isAccessCapabilities({ operations, fields: {}, blockFields })).toBe(false);
	});
});

describe("prepared route domain data", () => {
	const error = { error: { code: "access_denied", status: 403, message: "Denied", issues: [] } };
	const document = { document: { value: { id: "one", title: "Prepared" } }, access: error };
	const version = {
		ID: "v1",
		DocumentID: "one",
		Revision: 1,
		Status: "draft",
		Snapshot: { id: "one" },
		CreatedAt: "2026-09-15T00:00:00Z",
	};
	const routes = [
		{ kind: "dashboard" },
		{ kind: "collection-create", create: { values: { signal: "authored data" }, access: error } },
		{ kind: "collection-document", document },
		{ kind: "global-document", document },
		{ kind: "account", document },
		{ kind: "collection-api", document },
		{ kind: "global-api", document },
		{ kind: "upload", access: error },
		{
			kind: "collection-versions",
			versions: {
				history: { value: [version] },
				detail: { value: version },
				document,
			},
		},
		{ kind: "global-versions", versions: { history: error, document } },
		{ kind: "security", security: { sessions: { value: [] }, apiKeys: { value: [] } } },
	];
	it("accepts named route results, including expected independent read errors", () => {
		for (const route of routes) expect(isAdminPreparedRouteData(route)).toBe(true);
	});
	it("rejects missing data, malformed metadata and ambiguous results at the boundary", () => {
		for (const route of routes.filter((route) => route.kind !== "dashboard"))
			expect(isAdminPreparedRouteData({ kind: route.kind })).toBe(false);
		for (const route of [
			{ kind: "collection-create", create: { values: null, access: error } },
			{
				kind: "collection-document",
				document: { ...document, document: { ...error, value: { id: "one" } } },
			},
			{ kind: "account", document: { ...document, document: { value: { id: 1 } } } },
			{
				kind: "account",
				document: {
					...document,
					document: { value: { id: "one", _localization: { sources: { title: 1 } } } },
				},
			},
			{ kind: "global-api", document: { document: { value: {} }, access: error } },
			{
				kind: "collection-versions",
				versions: {
					history: { value: [{ ...version, Revision: "1" }] },
					document,
				},
			},
			{
				kind: "security",
				security: { sessions: { value: [{ id: "one" }] }, apiKeys: { value: [] } },
			},
		])
			expect(isAdminPreparedRouteData(route)).toBe(false);
	});
});

describe("semantic collection list wire data", () => {
	const access = {
		operations: Object.fromEntries(
			[
				"admin",
				"create",
				"read",
				"readVersions",
				"update",
				"delete",
				"duplicate",
				"publish",
				"unpublish",
				"restoreDeleted",
				"deletePermanent",
				"selectAll",
			].map((key) => [key, true])
		),
		fields: { title: { read: true, create: true, update: false } },
	};
	const data = {
		query: { trash: false, locale: "en", where: { title: { like: "launch" } } },
		page: {
			value: {
				docs: [{ id: "one", title: "launch" }],
				pagination: {
					page: 1,
					limit: 10,
					totalDocs: 1,
					totalPages: 1,
					hasNextPage: false,
					hasPrevPage: false,
				},
				access: { collection: access, documents: { one: access } },
			},
		},
		counts: { "": { value: 0 } },
		preferences: { workspace: { value: null }, presets: { value: [] } },
	};
	it("accepts complete data and scoped count/page results", () => {
		expect(isAdminCollectionListData(data)).toBe(true);
		expect(isAdminCollectionListData({ query: data.query, page: data.page, counts: {} })).toBe(
			true
		);
		expect(isAdminCollectionListData({ query: data.query, counts: data.counts })).toBe(true);
		expect(
			isAdminCollectionListData({
				query: data.query,
				counts: {},
				page: { error: { code: "access_denied", status: 403, message: "Denied", issues: [] } },
			})
		).toBe(true);
	});
	it("rejects malformed envelopes and access rather than letting them reach controllers", () => {
		for (const candidate of [
			null,
			{ ...data, query: { trash: "false" } },
			{ ...data, page: { value: data.page.value, error: {} } },
			{ ...data, counts: { "": { value: "0" } } },
			{ ...data, preferences: { workspace: {}, presets: { value: null } } },
			{
				...data,
				page: { value: { ...data.page.value, access: { collection: {}, documents: {} } } },
			},
			{
				...data,
				page: { value: { ...data.page.value, access: { collection: access, documents: {} } } },
			},
			{
				...data,
				page: {
					value: {
						...data.page.value,
						pagination: { page: 1, limit: 10, hasNextPage: false, hasPrevPage: false },
					},
				},
			},
		])
			expect(isAdminCollectionListData(candidate)).toBe(false);
	});
});

describe("collection page envelopes", () => {
	const metadata = { page: 2, limit: 10, hasNextPage: true, hasPrevPage: true };
	const counted = { docs: [], pagination: { ...metadata, totalDocs: 21, totalPages: 3 } };
	const uncounted = { docs: [], pagination: metadata };
	it("distinguishes counted pages from pagination=false pages", () => {
		expect(isPageEnvelope(counted)).toBe(true);
		expect(isUncountedPageEnvelope(counted)).toBe(false);
		expect(isUncountedPageEnvelope(uncounted)).toBe(true);
		expect(isPageEnvelope(uncounted)).toBe(false);
	});
	it("rejects partial or malformed totals and metadata", () => {
		for (const pagination of [
			{ ...metadata, totalDocs: 21 },
			{ ...metadata, totalPages: 3 },
			{ ...metadata, totalDocs: "21", totalPages: 3 },
			{ ...metadata, hasNextPage: "true", totalDocs: 21, totalPages: 3 },
		]) {
			expect(isPageEnvelope({ docs: [], pagination })).toBe(false);
			expect(isUncountedPageEnvelope({ docs: [], pagination })).toBe(false);
		}
	});
});

describe("Go protocol conformance fixtures", () => {
	it("validates optional resolved issue metadata without trusting TypeScript types", () => {
		const issue = { code: "url", path: "sections.0.links.0.url", message: "Choose a URL" };
		expect(isValidationIssue(issue)).toBe(true);
		for (const key of ["target", "fieldId", "collectionId", "globalId", "locale"]) {
			expect(isValidationIssue({ ...issue, [key]: "identity" })).toBe(true);
			for (const value of [null, 1, [], {}])
				expect(isValidationIssue({ ...issue, [key]: value })).toBe(false);
		}
	});
	it("decodes the checked Go error envelope", async () => {
		const fixture = await Bun.file(
			new URL("../../../testdata/protocol/error.json", import.meta.url)
		).json();
		expect(isErrorEnvelope(fixture)).toBe(true);
	});

	it("rejects an unstable error code", () => {
		expect(
			isErrorEnvelope({
				error: {
					code: "surprise",
					status: 500,
					message: "No",
					issues: [],
				},
			})
		).toBe(false);
	});
});
