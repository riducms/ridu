import type { AdminDocument } from "@admin/core/api/admin-client";
import { documentLabel, documentTitleField } from "@admin/features/documents/document-title";
import type { SchemaCollection } from "@riducms/protocol";
import { SvelteMap } from "svelte/reactivity";

import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

import { RelationshipLookupController } from "@admin/fields/relationship/relationship-lookup-controller.svelte";
import type { RelationshipFilter } from "@admin/fields/relationship/relationship-query";
import {
	parseRelationshipKey,
	relationshipKey,
} from "@admin/fields/relationship/relationship-value";

export interface RelationshipQuickPickerTarget {
	collection: SchemaCollection;
	filter: RelationshipFilter;
}

interface RelationshipQuickPickerControllerOptions {
	runtime: AdminRuntime;
	get targets(): readonly RelationshipQuickPickerTarget[];
	get selectedTarget(): string | undefined;
	get selectedID(): string | undefined;
	get excludedValues(): readonly string[];
	get locale(): string | undefined;
	onPick(target: string, id: string): void;
	onRemove(): void;
	onBrowse(target: string): void;
}

export class RelationshipQuickPickerController {
	open = $state(false);
	query = $state("");
	readonly #lookups = new SvelteMap<string, RelationshipLookupController>();

	constructor(readonly options: RelationshipQuickPickerControllerOptions) {
		$effect(() => {
			const targets = this.options.targets;
			const activeTargets = new Set(targets.map((target) => target.collection.slug));
			for (const [slug, lookup] of this.#lookups) {
				if (activeTargets.has(slug)) continue;
				lookup.dispose();
				this.#lookups.delete(slug);
			}
			for (const target of targets) {
				if (this.#lookups.has(target.collection.slug)) continue;
				this.#lookups.set(
					target.collection.slug,
					new RelationshipLookupController(this.options.runtime.client, this.options.runtime.i18n)
				);
			}
		});

		$effect(() => {
			const targets = this.options.targets;
			if (!this.open) return;
			const search = this.query;
			const locale = this.options.locale;
			const excluded = this.options.excludedValues.map(parseRelationshipKey);
			this.options.runtime.documentRevision;
			const timer = window.setTimeout(
				() => {
					for (const target of targets) {
						const ids = excluded.flatMap((selection) =>
							selection?.relationTo === target.collection.slug ? [selection.id] : []
						);
						this.#lookups.get(target.collection.slug)?.search({
							slug: target.collection.slug,
							page: 1,
							limit: 5,
							search,
							searchField: documentTitleField(target.collection)?.name,
							filter: target.filter,
							excludeIDs: ids,
							locale,
						});
					}
				},
				search.trim() === "" ? 0 : 180
			);
			return () => {
				window.clearTimeout(timer);
				this.#disposeLookups();
			};
		});
	}

	get groups() {
		const excluded = new Set(this.options.excludedValues);

		return this.options.targets.flatMap((target) => {
			const lookup = this.#lookups.get(target.collection.slug);
			return lookup === undefined
				? []
				: [
						{
							collection: target.collection,
							docs: lookup.docs.filter(
								(document) =>
									!excluded.has(
										relationshipKey({ relationTo: target.collection.slug, id: document.id })
									)
							),
							error: lookup.error,
							status: lookup.status,
						},
					];
		});
	}

	get comboboxItems() {
		return this.groups.flatMap((group) =>
			group.docs.map((document) => ({
				value: relationshipKey({ relationTo: group.collection.slug, id: document.id }),
				label: this.documentLabel(group.collection, document),
			}))
		);
	}

	get selectedValue() {
		const target = this.options.selectedTarget;
		const id = this.options.selectedID;
		return target === undefined || id === undefined
			? ""
			: relationshipKey({ relationTo: target, id });
	}

	documentLabel = (collection: SchemaCollection, document: AdminDocument) =>
		documentLabel(collection, document);

	changeOpen = (open: boolean) => {
		this.open = open;
		if (open) return;
		this.query = "";
	};

	setQuery = (query: string) => {
		this.query = query;
	};

	pick = (value: string) => {
		const selection = parseRelationshipKey(value);
		if (
			selection === undefined ||
			(selection.relationTo === this.options.selectedTarget &&
				selection.id === this.options.selectedID)
		) {
			this.changeOpen(false);
			return;
		}
		this.options.onPick(selection.relationTo, selection.id);
		this.changeOpen(false);
	};

	browseAll = (target?: string) => {
		const targetSlug =
			target ?? this.options.selectedTarget ?? this.options.targets[0]?.collection.slug;
		if (targetSlug === undefined) return;
		this.changeOpen(false);
		this.options.onBrowse(targetSlug);
	};

	remove = () => {
		this.changeOpen(false);
		this.options.onRemove();
	};

	#disposeLookups() {
		for (const lookup of this.#lookups.values()) lookup.dispose();
	}
}
