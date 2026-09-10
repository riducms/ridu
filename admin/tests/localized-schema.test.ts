import { describe, expect, test } from "bun:test";
import { createAdminI18n, en, fr } from "@riducms/translations";
import {
	bindSchemaManifest,
	resolveBlockTypes,
	type SchemaBlockType,
	type SchemaCollection,
	type SchemaField,
} from "@riducms/protocol";

import { localizeSchemaCollection, localizeSchemaField } from "@admin/core/i18n/localized-schema";

const field: SchemaField = {
	id: "posts-status",
	name: "status",
	path: "status",
	type: "select",
	category: "scalar",
	required: false,
	unique: false,
	admin: {
		label: "Status",
		labelTranslations: { fr: "Statut" },
		description: "Editorial state",
		descriptionTranslations: { fr: "État éditorial" },
		placeholder: "Choose a status",
		placeholderTranslations: { fr: "Choisissez un statut" },
	},
	select: {
		options: [{ value: "draft", label: "Draft", labelTranslations: { fr: "Brouillon" } }],
	},
};

describe("localized schema display metadata", () => {
	const i18n = createAdminI18n({ languages: [en, fr], language: "fr" });

	test("localizes fields without changing their stable identity", () => {
		const localized = localizeSchemaField(field, i18n);
		expect(localized.id).toBe(field.id);
		expect(localized.path).toBe(field.path);
		expect(localized.admin.label).toBe("Statut");
		expect(localized.admin.description).toBe("État éditorial");
		expect(localized.admin.placeholder).toBe("Choisissez un statut");
		expect(localized.select?.options[0]?.label).toBe("Brouillon");
		expect(field.admin.label).toBe("Status");
	});

	test("keeps the canonical tab identity while retaining its translated display metadata", () => {
		const tabbed = {
			...field,
			admin: {
				...field.admin,
				tab: "Publishing",
				tabTranslations: { fr: "Publication" },
			},
		};
		const localized = localizeSchemaField(tabbed, i18n);

		expect(localized.admin.tab).toBe("Publishing");
		expect(localized.admin.tabTranslations).toEqual({ fr: "Publication" });
	});

	test("localizes collection labels and retains canonical fallback metadata", () => {
		const collection: SchemaCollection = {
			id: "posts",
			slug: "posts",
			labels: {
				singular: "Post",
				singularTranslations: { fr: "Article" },
				plural: "Posts",
				pluralTranslations: { fr: "Articles" },
			},
			admin: {},
			capabilities: {
				auth: false,
				upload: false,
				versions: false,
				trash: false,
				locking: false,
			},
			fields: [field],
			indexes: [],
		};
		const localized = localizeSchemaCollection(collection, i18n);
		expect(localized.labels).toMatchObject({ singular: "Article", plural: "Articles" });
		expect(localized.fields[0]?.admin.label).toBe("Statut");
		expect(collection.labels.singular).toBe("Post");
	});
});

test("both block label forms localize identically through inline and shared embedded placements", () => {
	const definition: SchemaBlockType = {
		slug: "people",
		labels: {
			singular: "Person",
			plural: "People",
			singularTranslations: { fr: "Personne" },
			pluralTranslations: { fr: "Personnes" },
		},
		fields: [field],
	};
	for (const references of [false, true]) {
		const types = references ? { blockReferences: ["people"] } : { types: [definition] };
		const ordinary: SchemaField = {
			...field,
			name: "layout",
			path: "layout",
			id: "pages-layout",
			type: "blocks",
			category: "nested",
			blocks: types,
		};
		const embedded: SchemaField = {
			...field,
			name: "body",
			path: "body",
			id: "pages-body",
			type: "plugin",
			category: "plugin",
			plugin: {
				key: "test",
				config: {},
				embeddedTrees: [
					{
						version: 1,
						key: "blocks",
						root: ["root"],
						children: "children",
						tag: "type",
						cases: [
							{
								tagValue: "block",
								payload: "fields",
								discriminator: "blockType",
								identity: "_key",
								...types,
							},
						],
					},
				],
			},
		};
		const schema = bindSchemaManifest({
			blocks: references ? [definition] : [],
			collections: [
				{
					id: "pages",
					slug: "pages",
					labels: { singular: "Page", plural: "Pages" },
					admin: {},
					capabilities: {
						auth: false,
						upload: false,
						versions: false,
						trash: false,
						locking: false,
					},
					fields: [ordinary, embedded],
				},
			],
			globals: [],
		});
		const wire = JSON.stringify(schema);
		for (const language of ["fr", "en"] as const) {
			const i18n = createAdminI18n({ languages: [en, fr], language });
			const layout = localizeSchemaField(ordinary, i18n);
			const body = localizeSchemaField(embedded, i18n);
			const labels = resolveBlockTypes(layout.blocks)[0]!.labels;
			expect(labels).toMatchObject(
				language === "fr"
					? { singular: "Personne", plural: "Personnes" }
					: { singular: "Person", plural: "People" }
			);
			expect(resolveBlockTypes(body.plugin!.embeddedTrees![0]!.cases[0])[0]!.labels).toEqual(
				labels
			);
			expect(JSON.stringify(schema)).toBe(wire);
			expect(resolveBlockTypes(layout.blocks)[0]!.slug).toBe("people");
		}
	}
});
