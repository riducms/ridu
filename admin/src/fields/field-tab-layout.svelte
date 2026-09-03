<script lang="ts">
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
	let revealedIssueSignature = $state("");
	const activeTab = $derived(
		tabs.some((tab) => tab.key === selectedTab) ? selectedTab : (tabs[0]?.key ?? "")
	);

	function issueCountForTab(tab: string) {
		return fields.filter(
			(field) => (field.admin.tab ?? "") === tab && form.issuesFor(field.path).length > 0
		).length;
	}

	$effect(() => {
		const signature = form.issues.map((issue) => `${issue.code}:${issue.path}`).join("|");
		if (signature === "" || signature === revealedIssueSignature) return;
		revealedIssueSignature = signature;
		const issueTab = tabs.find((tab) => issueCountForTab(tab.key) > 0);
		if (issueTab !== undefined) selectedTab = issueTab.key;
	});
</script>

<Tabs
	class="col-span-1 mb-1 gap-0 sm:col-span-12"
	value={activeTab}
	onValueChange={(tab) => (selectedTab = tab)}
	data-field-tab-group={tabGroup.id}
>
	<TabsList
		variant="line"
		class="-ms-5 h-12 w-[calc(100%+2.5rem)] justify-start gap-6 overflow-x-auto overflow-y-hidden border-b border-control-border px-5 sm:-ms-8 sm:w-[calc(100%+4rem)] sm:px-8 lg:-ms-[60px] lg:w-[calc(100%+120px)] lg:px-[60px]"
		aria-label={accessibleLabel}
	>
		{#each tabs as tab (tab.key)}
			{const issueCount = $derived(issueCountForTab(tab.key))}
			<TabsTrigger
				class="h-12 flex-none rounded-none px-px text-[14px] font-medium focus-visible:bg-control focus-visible:outline-none data-[state=active]:border-b-foreground-strong!"
				value={tab.key}
			>
				{tab.label}{#if issueCount > 0}<span
						class="font-mono grid min-w-4 place-items-center rounded-full bg-destructive/14 px-1 text-[9px] text-destructive"
						aria-label={runtime.i18n.t("fields:invalidCount", { count: issueCount })}
					>
						{runtime.i18n.formatNumber(issueCount)}
					</span>{/if}
			</TabsTrigger>
		{/each}
	</TabsList>
	{#each tabs as tab (tab.key)}
		<TabsContent value={tab.key} class="pt-6">
			{#if activeTab === tab.key}
				<FieldLayout
					fields={fields.filter((field) => (field.admin.tab ?? "") === tab.key)}
					{form}
					suppressedTabGroupID={tabGroup.id}
				/>
			{/if}
		</TabsContent>
	{/each}
</Tabs>
