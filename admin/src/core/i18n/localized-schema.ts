import type { AdminI18n } from "@riducms/plugin";
import type { SchemaCollection, SchemaField } from "@riducms/protocol";

/** Returns detached display metadata for the active interface language. */
export function localizeSchemaField(field: SchemaField, i18n: AdminI18n): SchemaField {
	return {
		...field,
		admin: {
			...field.admin,
			label: i18n.text(field.admin.label, field.admin.labelTranslations),
			...(field.admin.description === undefined
				? {}
				: {
						description: i18n.text(field.admin.description, field.admin.descriptionTranslations),
					}),
			...(field.admin.placeholder === undefined
				? {}
				: {
						placeholder: i18n.text(field.admin.placeholder, field.admin.placeholderTranslations),
					}),
			...(field.admin.collapsible === undefined
				? {}
				: {
						collapsible: {
							...field.admin.collapsible,
							label: i18n.text(
								field.admin.collapsible.label,
								field.admin.collapsible.labelTranslations
							),
						},
					}),
		},
		...(field.select === undefined
			? {}
			: {
					select: {
						...field.select,
						choices: field.select.choices.map((choice) => ({
							...choice,
							label: i18n.text(choice.label, choice.labelTranslations),
						})),
					},
				}),
		...(field.nested === undefined
			? {}
			: {
					nested: {
						...field.nested,
						fields: field.nested.fields.map((child) => localizeSchemaField(child, i18n)),
						...(field.nested.rowLabels === undefined
							? {}
							: {
									rowLabels: {
										...field.nested.rowLabels,
										singular: i18n.text(
											field.nested.rowLabels.singular,
											field.nested.rowLabels.singularTranslations
										),
										plural: i18n.text(
											field.nested.rowLabels.plural,
											field.nested.rowLabels.pluralTranslations
										),
									},
								}),
					},
				}),
		...(field.blocks === undefined
			? {}
			: {
					blocks: {
						...field.blocks,
						types: field.blocks.types.map((block) => ({
							...block,
							label: i18n.text(block.label, block.labelTranslations),
							fields: block.fields.map((child) => localizeSchemaField(child, i18n)),
						})),
					},
				}),
	};
}

export function localizeSchemaCollection(
	collection: SchemaCollection,
	i18n: AdminI18n
): SchemaCollection {
	return {
		...collection,
		labels: {
			...collection.labels,
			singular: i18n.text(collection.labels.singular, collection.labels.singularTranslations),
			plural: i18n.text(collection.labels.plural, collection.labels.pluralTranslations),
		},
		admin: {
			...collection.admin,
			...(collection.admin.group === undefined
				? {}
				: {
						group: i18n.text(collection.admin.group, collection.admin.groupTranslations),
					}),
			...(collection.admin.description === undefined
				? {}
				: {
						description: i18n.text(
							collection.admin.description,
							collection.admin.descriptionTranslations
						),
					}),
		},
		fields: collection.fields.map((field) => localizeSchemaField(field, i18n)),
	};
}
