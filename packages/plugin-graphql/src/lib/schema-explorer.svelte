<script lang="ts">
	import type { AdminI18n } from "@riducms/plugin";
	import { Button, Input } from "@riducms/ui";
	import type { GraphQLSchema } from "graphql";
	import { tick } from "svelte";

	import { describeType, schemaRoots, searchSchema, type TypeReference } from "#lib/schema-docs.js";

	let {
		id,
		schema,
		trail,
		i18n,
		onOpen,
		onBack,
		onClose,
	}: {
		id: string;
		schema: GraphQLSchema;
		/** Types opened so far; the last one is shown. Empty shows the root types. */
		trail: readonly string[];
		i18n: AdminI18n;
		onOpen: (type: string) => void;
		onBack: () => void;
		onClose: () => void;
	} = $props();

	let search = $state("");
	let heading: HTMLHeadingElement | undefined;

	const current = $derived(trail.at(-1));
	const type = $derived(current === undefined ? undefined : describeType(schema, current));
	const matches = $derived(searchSchema(schema, search));

	// Navigating inside the explorer moves focus to the new heading so keyboard users land on the
	// content they chose. Opening docs from the editor leaves focus in the editor.
	async function open(name: string) {
		search = "";
		onOpen(name);
		await tick();
		heading?.focus();
	}

	async function back() {
		onBack();
		await tick();
		heading?.focus();
	}
</script>

{#snippet reference(target: TypeReference)}
	<button type="button" class="ridu-graphql-docs__type" onclick={() => open(target.name)}>
		{target.label}
	</button>
{/snippet}

<aside {id} class="ridu-graphql-docs" aria-label={i18n.t("plugin.graphql:docs")}>
	<header class="ridu-graphql-docs__header">
		{#if trail.length > 0}
			<Button variant="ghost" size="sm" onclick={back}>← {i18n.t("plugin.graphql:back")}</Button>
		{/if}
		<h2 class="ridu-graphql-docs__title" tabindex="-1" bind:this={heading}>
			{current ?? i18n.t("plugin.graphql:docs")}
		</h2>
		<Button variant="ghost" size="sm" onclick={onClose}>
			{i18n.t("plugin.graphql:closeDocs")}
		</Button>
	</header>

	<Input
		type="search"
		class="ridu-graphql-docs__search"
		placeholder={i18n.t("plugin.graphql:searchSchema")}
		aria-label={i18n.t("plugin.graphql:searchSchema")}
		bind:value={search}
	/>

	<div class="ridu-graphql-docs__body">
		{#if search.trim() !== ""}
			{#if matches.length === 0}
				<p class="ridu-graphql-docs__muted">{i18n.t("plugin.graphql:noMatches")}</p>
			{:else}
				<ul class="ridu-graphql-docs__list">
					{#each matches as match (`${match.type}.${match.field ?? ""}`)}
						<li>
							<button
								type="button"
								class="ridu-graphql-docs__type"
								onclick={() => open(match.type)}
							>
								{match.field === undefined ? match.type : `${match.type}.${match.field}`}
							</button>
						</li>
					{/each}
				</ul>
			{/if}
		{:else if current === undefined}
			<h3 class="ridu-graphql-docs__section">{i18n.t("plugin.graphql:rootTypes")}</h3>
			<ul class="ridu-graphql-docs__list">
				{#each schemaRoots(schema) as root (root.operation)}
					<li>
						<span class="ridu-graphql-docs__keyword">{root.operation}</span>
						:
						{@render reference({ label: root.type, name: root.type })}
					</li>
				{/each}
			</ul>
		{:else if type === undefined}
			<p class="ridu-graphql-docs__muted">
				{i18n.t("plugin.graphql:typeNotFound", { type: current })}
			</p>
		{:else}
			<p class="ridu-graphql-docs__kind">{type.kind}</p>
			{#if type.description}
				<p class="ridu-graphql-docs__description">{type.description}</p>
			{/if}

			{#if type.fields.length > 0}
				<h3 class="ridu-graphql-docs__section">{i18n.t("plugin.graphql:fields")}</h3>
				<ul class="ridu-graphql-docs__fields">
					{#each type.fields as field (field.name)}
						<li class="ridu-graphql-docs__field">
							<div class="ridu-graphql-docs__signature" dir="ltr">
								<span class="ridu-graphql-docs__name">{field.name}</span>
								:
								{@render reference(field.type)}
							</div>
							{#if field.description}
								<p class="ridu-graphql-docs__description">{field.description}</p>
							{/if}
							{#if field.deprecation}
								<p class="ridu-graphql-docs__warning">
									{i18n.t("plugin.graphql:deprecated", { reason: field.deprecation })}
								</p>
							{/if}
							{#if field.args.length > 0}
								<details class="ridu-graphql-docs__arguments">
									<summary>{i18n.t("plugin.graphql:arguments")} ({field.args.length})</summary>
									<ul>
										{#each field.args as argument (argument.name)}
											<li>
												<span class="ridu-graphql-docs__name">{argument.name}</span>
												:
												{@render reference(argument.type)}
												{#if argument.defaultValue !== undefined}
													<span class="ridu-graphql-docs__muted">
														{i18n.t("plugin.graphql:defaultValue", {
															value: argument.defaultValue,
														})}
													</span>
												{/if}
												{#if argument.description}
													<p class="ridu-graphql-docs__description">{argument.description}</p>
												{/if}
											</li>
										{/each}
									</ul>
								</details>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}

			{#if type.values.length > 0}
				<h3 class="ridu-graphql-docs__section">{i18n.t("plugin.graphql:values")}</h3>
				<ul class="ridu-graphql-docs__list">
					{#each type.values as value (value.name)}
						<li>
							<span class="ridu-graphql-docs__name">{value.name}</span>
							{#if value.description}
								<p class="ridu-graphql-docs__description">{value.description}</p>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}

			{#if type.members.length > 0}
				<h3 class="ridu-graphql-docs__section">{i18n.t("plugin.graphql:members")}</h3>
				<ul class="ridu-graphql-docs__list">
					{#each type.members as member (member.name)}
						<li>{@render reference(member)}</li>
					{/each}
				</ul>
			{/if}
		{/if}
	</div>
</aside>
