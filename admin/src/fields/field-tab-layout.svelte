<script lang="ts">
	import { SvelteSet } from "svelte/reactivity";
	import type { SchemaField, SchemaFieldTabGroup } from "@riducms/protocol";

	import { Tabs, TabsContent, TabsList, TabsTrigger } from "@admin/components/ui/tabs";
	import type { FormController } from "@admin/core/forms/form-controller.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import FieldLayout from "@admin/fields/field-layout.svelte";

	let {
		fields,
		form,
		tabGroup,
	}: {
		fields: readonly SchemaField[];
		form: FormController;
		tabGroup: SchemaFieldTabGroup;
	} = $props();
	const runtime = getAdminRuntime();
	const fallbackTab = $derived(runtime.i18n.t("fields:content"));
	const tabs = $derived.by(() => {
		const entries: Array<{ key: string; label: string }> = [];
		for (const field of fields) {
			const key = field.admin.tab ?? "";
			if (entries.some((entry) => entry.key === key)) continue;
			entries.push({
				key,
				label:
					field.admin.tab === undefined
						? fallbackTab
						: runtime.i18n.text(field.admin.tab, field.admin.tabTranslations),
			});
		}
		return entries;
	});
	const accessibleLabel = $derived(
		runtime.i18n.t("fields:sections", {
			labels: runtime.i18n.formatList(tabs.map((tab) => tab.label)),
			count: tabs.length,
		})
	);
	let selectedTab = $state("");
	const visitedTabs = new SvelteSet<string>();
	let revealedIssueSignature = "";
	const activeTab = $derived(
		tabs.some((tab) => tab.key === selectedTab) ? selectedTab : (tabs[0]?.key ?? "")
	);

	function selectTab(tab: string) {
		// Retain mounted editors so tab changes do not discard unfinished input.
		visitedTabs.add(activeTab);
		visitedTabs.add(tab);
		selectedTab = tab;
	}

	function issueCountForTab(tab: string) {
		return fields.filter(
			(field) => (field.admin.tab ?? "") === tab && form.issuesFor(field.path).length > 0
		).length;
	}

	$effect(() => {
		const signature = form.issues.map((issue) => `${issue.code}:${issue.path}`).join("|");
		if (signature === revealedIssueSignature) return;
		revealedIssueSignature = signature;
		if (signature === "") return;
		const issueTab = tabs.find((tab) => issueCountForTab(tab.key) > 0);
		if (issueTab !== undefined) selectTab(issueTab.key);
	});
</script>

<Tabs
	class="ridu-field-tabs"
	value={activeTab}
	onValueChange={selectTab}
	data-field-tab-group={tabGroup.id}
>
	<TabsList variant="line" class="ridu-field-tabs__list" aria-label={accessibleLabel}>
		{#each tabs as tab (tab.key)}
			{const issueCount = $derived(issueCountForTab(tab.key))}
			<TabsTrigger class="ridu-field-tabs__trigger" value={tab.key}>
				{tab.label}
				{#if issueCount > 0}
					<span
						class="ridu-error-count"
						aria-label={runtime.i18n.t("fields:invalidCount", { count: issueCount })}
					>
						{runtime.i18n.formatNumber(issueCount)}
					</span>
				{/if}
			</TabsTrigger>
		{/each}
	</TabsList>
	{#each tabs as tab (tab.key)}
		<TabsContent value={tab.key} hidden={activeTab !== tab.key} class="ridu-field-tabs__content">
			{#if activeTab === tab.key || visitedTabs.has(tab.key)}
				<FieldLayout
					fields={fields.filter((field) => (field.admin.tab ?? "") === tab.key)}
					{form}
					suppressedTabGroupID={tabGroup.id}
				/>
			{/if}
		</TabsContent>
	{/each}
</Tabs>
