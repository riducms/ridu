import { ownController } from "./controller-owner.svelte";
import { describe, expect, it } from "vitest";
import type { AccessCapabilitiesEnvelope, SchemaCollection, SchemaField } from "@riducms/protocol";
import { createAdminI18n } from "@riducms/translations";

import type { NotificationCenter } from "@admin/core/notifications/notification-center.svelte";
import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

const { CreateFirstUserController } =
	await import("@admin/features/auth/create-first-user-controller.svelte");
const { ReferenceBrowserWorkflow } =
	await import("@admin/features/reference-browser/reference-browser-workflow.svelte");

describe("create-flow default initialization", () => {
	it("uses recursive and ordered multi-select defaults for first-user setup", () => {
		const collection = collectionWithDefaults(true);
		const controller = new CreateFirstUserController({
			runtime: { authCollection: collection } as AdminRuntime,
			notifications: {} as NotificationCenter,
			navigate: () => undefined,
		});

		expect(controller.form.values).toEqual({
			owner: "schema-owner",
			preferences: {
				appearance: { theme: "dark" },
				roles: ["editor", "reviewer"],
			},
		});
		expect(controller.form.dirty).toBe(false);
	});

	it("uses canonical defaults for inline creation and preserves explicit relationship values", async () => {
		const collection = collectionWithDefaults();
		const controller = ownController(ReferenceBrowserWorkflow, {
			runtime: runtimeFor(collection),
			notifications: {} as NotificationCenter,
			field: relationshipField(),
			collection,
			hasMany: true,
			selectedIDs: [],
			readOnly: false,
			defaultValues: { owner: "source-document" },
			onCommit: () => undefined,
			onClose: () => undefined,
			get open() {
				return true;
			},
			setOpen: () => undefined,
		});

		controller.openNewDocument();
		await Promise.resolve();
		await Promise.resolve();

		expect(controller.form.values).toEqual({
			owner: "source-document",
			preferences: {
				appearance: { theme: "dark" },
				roles: ["editor", "reviewer"],
			},
		});
		expect(controller.form.dirty).toBe(false);
		expect(controller.canSave).toBe(true);
	});

	it("keeps a missing defaulted group absent and clean when editing an existing document", async () => {
		const collection = collectionWithDefaults();
		const controller = ownController(ReferenceBrowserWorkflow, {
			runtime: runtimeFor(collection),
			notifications: {} as NotificationCenter,
			field: relationshipField(),
			collection,
			hasMany: true,
			selectedIDs: [],
			readOnly: false,
			initialDocument: {
				id: "target-1",
				owner: "stored-owner",
				createdAt: "2026-08-28T00:00:00Z",
				updatedAt: "2026-08-28T00:00:00Z",
			},
			onCommit: () => undefined,
			onClose: () => undefined,
			get open() {
				return true;
			},
			setOpen: () => undefined,
		});

		await Promise.resolve();
		await Promise.resolve();

		expect(Object.hasOwn(controller.form.values, "preferences")).toBe(false);
		expect(controller.form.get("preferences")).toBeUndefined();
		expect(controller.form.dirty).toBe(false);
		expect(controller.editorDirty).toBe(false);
		expect(controller.canSave).toBe(false);
	});

	it("requires a file before an inline upload create can be saved", async () => {
		const collection = collectionWithDefaults();
		collection.capabilities.upload = true;
		const controller = ownController(ReferenceBrowserWorkflow, {
			runtime: runtimeFor(collection),
			notifications: {} as NotificationCenter,
			field: relationshipField(),
			collection,
			hasMany: true,
			selectedIDs: [],
			readOnly: false,
			onCommit: () => undefined,
			onClose: () => undefined,
			get open() {
				return true;
			},
			setOpen: () => undefined,
		});

		controller.openNewDocument();
		await Promise.resolve();
		await Promise.resolve();

		expect(controller.form.dirty).toBe(false);
		expect(controller.canSave).toBe(false);
		controller.selectedFiles = {
			item: () => ({ name: "asset.png" }),
		} as unknown as FileList;
		expect(controller.canSave).toBe(true);
	});

	it("preserves nested row constraints and labels in the inline editor scope", () => {
		const collection = collectionWithDefaults();
		const items: SchemaField = {
			id: "targets-items",
			name: "items",
			path: "items",
			type: "array",
			category: "nested",
			required: false,
			unique: false,
			admin: { label: "Items" },
			nested: {
				fields: [textField("label", undefined, "items.label")],
				minRows: 2,
				maxRows: 4,
				rowLabel: "label",
				rowLabelComponent: {
					plugin: "curriculum",
					component: "itemSummary",
					config: { prefix: "Item" },
				},
				rowLabels: {
					singular: "Item",
					singularTranslations: { fr: "Élément" },
					plural: "Items",
					pluralTranslations: { fr: "Éléments" },
				},
			},
		};
		collection.fields.push(items);

		const controller = ownController(ReferenceBrowserWorkflow, {
			runtime: runtimeFor(collection),
			notifications: {} as NotificationCenter,
			field: relationshipField(),
			collection,
			hasMany: true,
			selectedIDs: [],
			readOnly: false,
			onCommit: () => undefined,
			onClose: () => undefined,
			get open() {
				return true;
			},
			setOpen: () => undefined,
		});

		const scoped = controller.scopedFields.find((field) => field.name === "items");
		expect(scoped?.nested).toMatchObject({
			minRows: 2,
			maxRows: 4,
			rowLabel: "label",
			rowLabelComponent: {
				plugin: "curriculum",
				component: "itemSummary",
				config: { prefix: "Item" },
			},
			rowLabels: {
				singular: "Item",
				singularTranslations: { fr: "Élément" },
				plural: "Items",
				pluralTranslations: { fr: "Éléments" },
			},
		});
		expect(scoped?.nested?.fields[0]?.id).toBe("source-related-drawer-targets-items-label");
		expect(items.nested?.fields[0]?.id).toBe("targets-items-label");
	});
});

function runtimeFor(collection: SchemaCollection) {
	const access = accessEnvelope();
	return {
		client: {
			collectionAccess: async () => access,
		},
		collectionOperations: {
			[collection.slug]: { create: true },
		},
		i18n: createAdminI18n(),
	} as unknown as AdminRuntime;
}

function accessEnvelope(): AccessCapabilitiesEnvelope {
	return {
		operations: {
			admin: true,
			create: true,
			read: true,
			readVersions: true,
			update: true,
			delete: true,
			duplicate: true,
			publish: true,
			unpublish: true,
			restoreDeleted: true,
			deletePermanent: true,
			selectAll: true,
		},
		fields: {},
	};
}

function collectionWithDefaults(auth = false): SchemaCollection {
	return {
		id: "targets",
		slug: "targets",
		labels: { singular: "Target", plural: "Targets" },
		admin: { useAsTitle: "owner" },
		capabilities: {
			auth,
			upload: false,
			versions: false,
			trash: false,
			locking: false,
		},
		fields: [
			textField("owner", "schema-owner"),
			groupField("preferences", [
				groupField("appearance", [textField("theme", "dark", "preferences.appearance.theme")]),
				multiSelectField("roles", ["editor", "reviewer"]),
			]),
		],
	};
}

function textField(name: string, defaultValue?: string, path = name): SchemaField {
	return {
		id: `targets-${path.replaceAll(".", "-")}`,
		name,
		path,
		type: "text",
		category: "scalar",
		required: false,
		unique: false,
		default: defaultValue,
		admin: { label: name },
		text: {},
	};
}

function groupField(name: string, fields: SchemaField[]): SchemaField {
	return {
		id: `targets-${name}`,
		name,
		path: name,
		type: "group",
		category: "nested",
		required: false,
		unique: false,
		admin: { label: name },
		nested: { fields },
	};
}

function multiSelectField(name: string, defaultValues: string[]): SchemaField {
	return {
		id: `targets-${name}`,
		name,
		path: `preferences.${name}`,
		type: "select",
		category: "scalar",
		required: false,
		unique: false,
		admin: { label: name },
		select: {
			hasMany: true,
			defaultValues,
			options: defaultValues.map((value) => ({ value, label: value })),
		},
	};
}

function relationshipField(): SchemaField {
	return {
		id: "source-related",
		name: "related",
		path: "related",
		type: "relationship",
		category: "relationship",
		required: false,
		unique: false,
		admin: { label: "Related" },
		relationship: {
			targets: [{ collectionId: "targets", collectionSlug: "targets" }],
			hasMany: true,
			onDelete: "nullify",
		},
	};
}
