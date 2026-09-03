import type { SchemaCollection, SchemaField } from "@riducms/protocol";
import { SvelteMap } from "svelte/reactivity";

import type { AdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";

import { RelationshipLookupController } from "@admin/fields/relationship/relationship-lookup-controller.svelte";
import type { RelationshipFilter } from "@admin/fields/relationship/relationship-query";

export interface RelationshipQuickPickerTarget {
	collection: SchemaCollection;
	filter: RelationshipFilter;
}

interface RelationshipQuickPickerControllerOptions {
	runtime: AdminRuntime;
	get targets(): readonly RelationshipQuickPickerTarget[];
	get selectedTarget(): string | undefined;
	get selectedID(): string | undefined;
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
			this.options.runtime.documentRevision;
			const timer = window.setTimeout(
				() => {
					for (const target of targets) {
						this.#lookups.get(target.collection.slug)?.search({
							slug: target.collection.slug,
							page: 1,
							limit: 5,
							search,
							searchField: findDisplayField(target.collection.fields)?.name,
							filter: target.filter,
							locale,
						});
					}
				},
				search.trim() === "" ? 0 : 180
			);
			return () => window.clearTimeout(timer);
		});

		$effect(() => () => this.#disposeLookups());
	}

	get groups() {
		return this.options.targets.flatMap((target) => {
			const lookup = this.#lookups.get(target.collection.slug);
			return lookup === undefined
				? []
				: [
						{
							collection: target.collection,
							docs: lookup.docs,
							error: lookup.error,
							status: lookup.status,
						},
					];
		});
	}

	get comboboxItems() {
		return this.groups.flatMap((group) =>
			group.docs.map((document) => ({
				value: encodeSelection(group.collection.slug, document.id),
				label: this.documentLabel(group.collection, document),
			}))
		);
	}

	get selectedValue() {
		const target = this.options.selectedTarget;
		const id = this.options.selectedID;
		return target === undefined || id === undefined ? "" : encodeSelection(target, id);
	}

	documentLabel = (collection: SchemaCollection, document: Record<string, unknown>) => {
		const searchField = findDisplayField(collection.fields);
		return String(
			(searchField === undefined ? undefined : document[searchField.name]) ?? document.id
		);
	};

	initials = (value: string) =>
		value
			.trim()
			.split(/\s+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((part) => part[0]?.toLocaleUpperCase(this.options.runtime.i18n.language) ?? "")
			.join("");

	changeOpen = (open: boolean) => {
		this.open = open;
		if (open) return;
		this.query = "";
		this.#disposeLookups();
	};

	toggleOpen = () => {
		const open = !this.open;
		this.changeOpen(open);
		return open;
	};

	setQuery = (query: string) => {
		this.query = query;
	};

	pick = (value: string) => {
		const selection = decodeSelection(value);
		if (
			selection === undefined ||
			(selection.target === this.options.selectedTarget && selection.id === this.options.selectedID)
		) {
			this.changeOpen(false);
			return;
		}
		this.options.onPick(selection.target, selection.id);
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

function encodeSelection(target: string, id: string) {
	return JSON.stringify([target, id]);
}

function decodeSelection(value: string): { target: string; id: string } | undefined {
	try {
		const selection: unknown = JSON.parse(value);
		if (
			!Array.isArray(selection) ||
			selection.length !== 2 ||
			typeof selection[0] !== "string" ||
			typeof selection[1] !== "string"
		) {
			return undefined;
		}
		return { target: selection[0], id: selection[1] };
	} catch {
		return undefined;
	}
}

function findDisplayField(fields: readonly SchemaField[]) {
	return (
		fields.find((field) => field.name === "title" || field.name === "name") ??
		fields.find((field) => field.type === "text" && field.name !== "email") ??
		fields.find((field) => field.type === "text" || field.type === "email")
	);
}
