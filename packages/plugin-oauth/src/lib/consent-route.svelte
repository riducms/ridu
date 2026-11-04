<script lang="ts">
	import type { AdminExtensionProps } from "@riducms/plugin";
	import { Button } from "@riducms/ui";

	import { ConsentController } from "#lib/consent-controller.svelte.js";

	let props: AdminExtensionProps = $props();

	// The client sent the browser here with its request in the query string, which the admin
	// keeps across signing in.
	const consent = new ConsentController(window.location.search);
	void consent.load();

	const t = (key: string, values?: Record<string, string>) =>
		props.i18n.t(`plugin.oauth:${key}`, values);
	const application = $derived(props.manifest.application.name);
</script>

<section class="ridu-oauth-consent" aria-live="polite">
	{#if consent.state.kind === "loading"}
		<p class="ridu-oauth-consent__message">{t("loading")}</p>
	{:else if consent.state.kind === "redirecting"}
		<p class="ridu-oauth-consent__message">{t("redirecting")}</p>
	{:else if consent.state.kind === "refused"}
		{@const redirect = consent.state.redirect}
		<h1 class="ridu-oauth-consent__title">{t("refusedTitle")}</h1>
		<p class="ridu-oauth-consent__message" role="alert">{consent.state.message}</p>
		{#if redirect !== undefined}
			<div class="ridu-oauth-consent__actions">
				<Button variant="outline" onclick={() => consent.return(redirect)}>{t("backToApp")}</Button>
			</div>
		{/if}
	{:else if consent.state.kind === "failed"}
		<p class="ridu-oauth-consent__message ridu-oauth-consent__message--error" role="alert">
			{t("failed", { message: consent.state.message })}
		</p>
		<div class="ridu-oauth-consent__actions">
			<Button variant="outline" onclick={() => consent.load()}>{t("retry")}</Button>
		</div>
	{:else}
		{@const { request, submitting, error } = consent.state}
		<h1 class="ridu-oauth-consent__title">{t("title", { client: request.client.name })}</h1>
		<p class="ridu-oauth-consent__description">
			{t("description", { client: request.client.name, application })}
		</p>
		<fieldset class="ridu-oauth-consent__choices" disabled={submitting}>
			<legend>{t("actAs")}</legend>
			{#each request.actAs as choice, index (choice.id)}
				<label class="ridu-oauth-consent__choice">
					<input type="radio" name="act-as" value={choice.id} bind:group={consent.actAs} />
					<span>{index === 0 ? t("you", { label: choice.label }) : choice.label}</span>
				</label>
			{/each}
		</fieldset>
		<p class="ridu-oauth-consent__hint">
			{t(request.loopback ? "returnToLoopback" : "returnTo", { host: request.redirectHost })}
			{t("disconnectHint")}
		</p>
		{#if error !== undefined}
			<p class="ridu-oauth-consent__message ridu-oauth-consent__message--error" role="alert">
				{error}
			</p>
		{/if}
		<div class="ridu-oauth-consent__actions">
			<Button variant="outline" disabled={submitting} onclick={() => consent.decide(false)}>
				{t("deny")}
			</Button>
			<Button disabled={submitting} onclick={() => consent.decide(true)}>{t("allow")}</Button>
		</div>
	{/if}
</section>

<style>
	.ridu-oauth-consent {
		display: grid;
		gap: 16px;
		max-inline-size: 28rem;
		margin-block: 48px;
		margin-inline: auto;
		padding: 24px;
		border: 1px solid var(--border);
		border-radius: var(--radius-lg, 8px);
		background: var(--card, var(--background));
	}

	.ridu-oauth-consent__title {
		margin: 0;
		font-size: 18px;
		font-weight: 600;
	}

	.ridu-oauth-consent__description,
	.ridu-oauth-consent__message,
	.ridu-oauth-consent__hint {
		margin: 0;
		font-size: 13px;
		line-height: 1.5;
	}

	.ridu-oauth-consent__hint {
		color: var(--muted-foreground);
	}

	.ridu-oauth-consent__message--error {
		color: var(--destructive);
	}

	.ridu-oauth-consent__choices {
		display: grid;
		gap: 8px;
		margin: 0;
		padding: 0;
		border: 0;
		font-size: 13px;
	}

	.ridu-oauth-consent__choices legend {
		margin-block-end: 8px;
		font-weight: 500;
	}

	.ridu-oauth-consent__choice {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.ridu-oauth-consent__actions {
		display: flex;
		justify-content: flex-end;
		gap: 8px;
	}
</style>
