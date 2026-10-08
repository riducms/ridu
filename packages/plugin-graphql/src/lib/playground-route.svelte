<script lang="ts">
	import type { AdminExtensionProps, AdminLoaderProps } from "@riducms/plugin";
	import { Button } from "@riducms/ui";

	import type { PlaygroundData } from "#lib/loader.js";

	let props: AdminExtensionProps & AdminLoaderProps<PlaygroundData> = $props();

	// CodeMirror, the GraphQL language tools, and the explorer load only when the page opens,
	// so admins who never visit it do not download them.
	const playground = import("#lib/playground.svelte");
</script>

{#await playground}
	<section class="state" aria-busy="true" aria-label={props.i18n.t("plugin.graphql:loading")}>
		<p class="message">{props.i18n.t("plugin.graphql:loading")}</p>
	</section>
{:then { default: Playground }}
	<Playground {...props} />
{:catch}
	<section class="state" role="alert">
		<p class="message message--error">{props.i18n.t("plugin.graphql:loadFailed")}</p>
		<Button onclick={() => window.location.reload()}>
			{props.i18n.t("plugin.graphql:reload")}
		</Button>
	</section>
{/await}

<style>
	.state {
		display: grid;
		place-items: center;
		align-content: center;
		gap: 16px;
		min-block-size: 18rem;
	}

	.message {
		margin: 0;
		font-size: 12px;
		color: var(--foreground-faint, var(--muted-foreground));
	}

	.message--error {
		color: var(--destructive);
	}
</style>
