import { describe, expect, test } from "bun:test";

import {
	selectedRelationshipIDs,
	updateRelationshipValue,
} from "../src/fields/relationship/relationship-value";
import {
	buildRelationshipWhere,
	combineRelationshipFilters,
	relationshipOptionFilters,
} from "../src/fields/relationship/relationship-query";

describe("relationship values", () => {
	test("reads scalar and has-many IDs", () => {
		expect(selectedRelationshipIDs("authors_1", false, false, "authors")).toEqual(["authors_1"]);
		expect(selectedRelationshipIDs(["posts_1", "posts_2"], true, false, "posts")).toEqual([
			"posts_1",
			"posts_2",
		]);
	});

	test("preserves other polymorphic targets when one target changes", () => {
		const current = [
			{ relationTo: "people", id: "people_1" },
			{ relationTo: "teams", id: "teams_1" },
		];
		expect(selectedRelationshipIDs(current, true, true, "people")).toEqual(["people_1"]);
		expect(updateRelationshipValue(current, ["people_2"], true, true, "people")).toEqual([
			{ relationTo: "teams", id: "teams_1" },
			{ relationTo: "people", id: "people_2" },
		]);
	});

	test("derives and combines a document-aware option filter", () => {
		const optionFilter = relationshipOptionFilters(
			{
				id: "related",
				name: "related",
				path: "related",
				type: "relationship",
				category: "relationship",
				required: false,
				unique: false,
				admin: { label: "Related" },
				relationship: {
					collectionId: "posts",
					collectionSlug: "posts",
					onDelete: "nullify",
					optionFilters: [
						{
							targetPath: "category",
							operator: "equals",
							sourcePath: "category",
						},
						{
							collectionSlug: "posts",
							targetPath: "seo.title",
							operator: "notEquals",
							sourcePath: "seo.title",
						},
						{
							collectionSlug: "pages",
							targetPath: "template",
							operator: "equals",
							sourcePath: "template",
						},
						{
							collectionSlug: "posts",
							targetPath: "_status",
							operator: "equals",
							value: { type: "string", value: "published" },
						},
					],
				},
			},
			{ category: "news", seo: { title: "Current title" }, template: "landing" },
			"posts"
		);
		expect(
			buildRelationshipWhere(
				"title",
				"",
				combineRelationshipFilters(optionFilter, {
					field: "status",
					operator: "equals",
					value: "published",
				})
			)
		).toEqual({
			and: [
				{ category: { equals: "news" } },
				{ "seo.title": { notEquals: "Current title" } },
				{ _status: { equals: "published" } },
				{ status: { equals: "published" } },
			],
		});
	});

	test("derives the same server-backed filters for upload references", () => {
		const filters = relationshipOptionFilters(
			{
				id: "hero",
				name: "hero",
				path: "hero",
				type: "upload",
				category: "upload",
				required: false,
				unique: false,
				admin: { label: "Hero" },
				upload: {
					collectionId: "media",
					collectionSlug: "media",
					onDelete: "nullify",
					optionFilters: [
						{ targetPath: "mimeType", operator: "equals", sourcePath: "assetType" },
						{ targetPath: "filename", operator: "like", sourcePath: "assetName" },
					],
				},
			},
			{ assetType: "image/png", assetName: "hero" },
			"media"
		);
		expect(filters).toEqual([
			{ field: "mimeType", operator: "equals", value: "image/png" },
			{ field: "filename", operator: "like", value: "hero" },
		]);
	});
});

describe("relationship queries", () => {
	test("combines trimmed search and the selected collection filter", () => {
		expect(
			buildRelationshipWhere("title", " trail ", {
				field: "status",
				operator: "equals",
				value: "published",
			})
		).toEqual({
			and: [{ title: { like: "trail" } }, { status: { equals: "published" } }],
		});
	});

	test("keeps an empty-string document filter authoritative", () => {
		expect(
			buildRelationshipWhere(undefined, "", {
				field: "category",
				operator: "equals",
				value: "",
			})
		).toEqual({ category: { equals: "" } });
	});
});
