<script lang="ts" generics="Payload extends { blockType: string } = never">
	import type { Component, Snippet } from "svelte";
	import type { RichTextDocument, RichTextNode } from "#richtext/document";
	import {
		documentRecoveryIssue,
		renderRichTextText,
		richTextElementTag,
		safeRichTextURL,
	} from "#richtext/render";
	import type { RichTextBlockComponents } from "#richtext/svelte";

	let {
		value,
		blocks,
		fallback,
		references,
	}: {
		value: RichTextDocument<Payload>;
		blocks?: RichTextBlockComponents<Payload>;
		fallback?: Snippet<[unknown, Error]>;
		references?: Snippet<[Extract<RichTextNode<Payload>, { type: "upload" | "relationship" }>]>;
	} = $props();

	function component(block: Payload) {
		return (
			blocks !== undefined && Object.hasOwn(blocks, block.blockType)
				? blocks[block.blockType as Payload["blockType"]]
				: undefined
		) as Component<{ block: Payload }> | undefined;
	}
	function unsupported(node: unknown, message: string) {
		const error = new Error(message);
		if (fallback === undefined) throw error;
		return { node, error, render: fallback };
	}
	const recoveryIssue = $derived(documentRecoveryIssue(value));
</script>

{#snippet nodes(values: RichTextNode<Payload>[])}
	{#each values as node}
		{#if node.type === "block"}
			{const Block = $derived(component(node.fields))}
			{#if Block !== undefined}<Block block={node.fields} />
			{:else}
				{const failure = $derived(
					unsupported(
						node,
						`No renderer registered for rich-text block ${JSON.stringify(node.fields.blockType)}`
					)
				)}
				{@render failure.render(failure.node, failure.error)}
			{/if}
		{:else if node.type === "text"}
			{@html renderRichTextText(node)}
		{:else if node.type === "linebreak"}<br />
		{:else if node.type === "horizontalrule"}<hr />
		{:else if node.type === "upload" || node.type === "relationship"}
			{#if references !== undefined}{@render references(node)}
			{:else}
				{const failure = $derived(
					unsupported(node, `No renderer registered for rich-text node ${node.type}`)
				)}
				{@render failure.render(failure.node, failure.error)}
			{/if}
		{:else if node.type === "paragraph" || node.type === "heading" || node.type === "quote" || node.type === "link" || node.type === "list" || node.type === "listitem" || node.type === "code"}
			{#if node.type === "code"}<pre><code>{@render nodes(node.children)}</code></pre>
			{:else}
				<svelte:element
					this={richTextElementTag(node)}
					{...node.type === "link" ? { href: safeRichTextURL(node.url ?? "") } : {}}
					data-align={["center", "right", "justify", "start", "end"].includes(node.format ?? "")
						? node.format
						: undefined}
					data-indent={(node.indent ?? 0) > 0
						? Math.min(8, Math.floor(node.indent ?? 0))
						: undefined}
					data-list-type={node.type === "list" && node.listType === "check" ? "check" : undefined}
					data-checked={node.type === "listitem" ? node.checked : undefined}
				>
					{@render nodes(node.children)}
				</svelte:element>
			{/if}
		{:else}
			{const failure = $derived(unsupported(node, "Unsupported rich-text node"))}
			{@render failure.render(failure.node, failure.error)}
		{/if}
	{/each}
{/snippet}

{#if recoveryIssue !== undefined}
	{const failure = $derived(
		unsupported(value, `Unsupported rich-text document envelope or budget at ${recoveryIssue}`)
	)}
	{@render failure.render(failure.node, failure.error)}
{:else}
	{@render nodes(value.root.children)}
{/if}
