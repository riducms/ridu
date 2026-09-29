<script lang="ts">
	import { focusFieldIssue } from "@admin/core/forms/field-issue-focus";
	import type { AdminDocumentExtensionHost } from "@riducms/plugin";
	import { Link, useBlocker, useLocation, useNavigate, useParams } from "@hvniel/svelte-router";
	import { tick } from "svelte";
	import type { Attachment } from "svelte/attachments";
	import LockKeyholeIcon from "~icons/lucide/lock-keyhole";

	import { Banner } from "@admin/components/ui/banner";
	import {
		Button,
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle,
	} from "@riducms/ui";
	import { Skeleton } from "@admin/components/ui/skeleton";
	import {
		createDocumentAPIPath,
		createDocumentPath,
		documentIDFromAdminPath,
		documentAPIPath,
		documentPath,
		documentVersionsPath,
		globalAPIPath,
		globalPath,
		globalVersionsPath,
		withContentLocale,
	} from "@admin/core/routing/admin-paths";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { registerAdminScrollPage } from "@admin/core/routing/admin-scroll.svelte";
	import { MediaQuery } from "svelte/reactivity";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { getAdminBootstrapCoordinator } from "@admin/core/bootstrap/admin-bootstrap";
	import { fieldsUseLocalization } from "@admin/core/schema/field-localization";
	import { DocumentController } from "@admin/features/documents/document-controller.svelte";
	import {
		documentRouteIsDirty,
		validateNewUserCredentials,
		saveAuthCreateWithClearedCredentials,
		type AuthCreateCredentials,
	} from "@admin/features/documents/document-credentials";
	import LivePreviewPanel from "@admin/features/documents/live-preview-panel.svelte";
	import UploadDocumentPreview from "@admin/features/documents/upload-document-preview-loader.svelte";
	import DocumentFieldSections from "@admin/features/documents/document-field-sections.svelte";
	import DocumentAPIView from "@admin/features/documents/document-api-view-loader.svelte";

	import { getBreadcrumbs } from "@admin/features/navigation/breadcrumb-context.svelte";
	import DocumentActionBar from "@admin/features/documents/document-action-bar.svelte";
	import DocumentOperationDialogs from "@admin/features/documents/document-operation-dialogs.svelte";
	import DocumentCredentials from "@admin/features/documents/document-credentials.svelte";

	import "@admin/features/documents/document.scss";

	const runtime = getAdminRuntime();
	const bootstrap = getAdminBootstrapCoordinator();
	const notifications = getNotificationCenter();
	const navigate = useNavigate();
	const params = useParams<"collection" | "document" | "global">();
	const location = useLocation();
	const preparedRoute = $derived(
		bootstrap.routeData(location.current.pathname, location.current.search)
	);
	const globalResource = $derived(params.current.global !== undefined);
	const slug = $derived(params.current.global ?? params.current.collection ?? "");
	let routeDocumentID = $state(
		params.current.document === undefined
			? undefined
			: documentIDFromAdminPath(location.current.pathname)
	);
	$effect.pre(() => {
		if (params.current.document === undefined) return;
		const decoded = documentIDFromAdminPath(location.current.pathname);
		// Parent route params can clear one render before unmount. Retain the exact
		// document owner so the outgoing editor cannot transiently enter create mode.
		if (decoded !== undefined) routeDocumentID = decoded;
	});
	const localization = $derived(runtime.manifest?.application.localization);
	const resourceSchema = $derived(
		(globalResource ? runtime.manifest?.globals : runtime.manifest?.collections)?.find(
			(candidate) => candidate.slug === slug
		)
	);
	const localeEnabled = $derived(fieldsUseLocalization(resourceSchema?.fields ?? []));
	const requestedLocale = $derived(new URLSearchParams(location.current.search).get("locale"));
	const activeLocale = $derived(runtime.resolveContentLocale(requestedLocale));
	const activeLocaleConfig = $derived(
		localeEnabled ? localization?.locales.find((locale) => locale.code === activeLocale) : undefined
	);
	const routeDocumentView = $derived<"edit" | "api">(
		location.current.pathname.endsWith("/api") ? "api" : "edit"
	);
	let documentView = $derived<string>(routeDocumentView);
	const editPath = $derived(
		withContentLocale(
			globalResource
				? globalPath(slug)
				: routeDocumentID === undefined
					? createDocumentPath(slug)
					: documentPath(slug, routeDocumentID),
			activeLocale
		)
	);
	const apiPath = $derived(
		withContentLocale(
			globalResource
				? globalAPIPath(slug)
				: routeDocumentID === undefined
					? createDocumentAPIPath(slug)
					: documentAPIPath(slug, routeDocumentID),
			activeLocale
		)
	);
	const versionHistoryPath = $derived(
		withContentLocale(
			globalResource ? globalVersionsPath(slug) : documentVersionsPath(slug, routeDocumentID ?? ""),
			activeLocale
		)
	);
	const controller = new DocumentController({
		runtime,
		notifications,
		navigate,
		get slug() {
			return slug;
		},
		get documentID() {
			return routeDocumentID;
		},
		get global() {
			return globalResource;
		},
		get locale() {
			return activeLocale;
		},
		get editable() {
			return routeDocumentView === "edit";
		},
		get prepared() {
			if (preparedRoute?.kind === "collection-create") return preparedRoute.create;
			if (
				preparedRoute?.kind === "collection-document" ||
				preparedRoute?.kind === "global-document" ||
				preparedRoute?.kind === "collection-api" ||
				preparedRoute?.kind === "global-api"
			)
				return preparedRoute.document;
			return undefined;
		},
	});
	const form = controller.form;
	const customDocumentViews = $derived(
		controller.currentDocument === undefined
			? []
			: runtime.config.extensions.documentViews.filter(
					(view) => view.collection === undefined || view.collection === controller.collectionSlug
				)
	);
	const customDocumentView = $derived(
		customDocumentViews.find((view) => view.key === documentView)
	);
	const extensionHost: AdminDocumentExtensionHost = {
		refresh: controller.refresh,
		notify: (tone, title, message) => notifications[tone]({ title, message }),
	};
	let credentials = $state<AuthCreateCredentials>({ password: "", passwordConfirmation: "" });
	let credentialIssue = $state<string>();
	let livePreviewOpen = $state(false);
	// Sticky content inside the document, such as a rich-text toolbar, stays below the action bar.
	let actionBarHeight = $state(0);
	let viewport: HTMLElement | null = null;
	let fieldsViewport: HTMLElement | null = null;
	const attachViewport: Attachment<HTMLElement> = (element) => {
		viewport = element;
		return () => {
			if (viewport === element) viewport = null;
		};
	};
	const attachFieldsViewport: Attachment<HTMLElement> = (element) => {
		fieldsViewport = element;
		return () => {
			if (fieldsViewport === element) fieldsViewport = null;
		};
	};
	const splitPreview = new MediaQuery("(min-width: 1240px)");
	registerAdminScrollPage({
		ready: () => !controller.loading,
		target: () =>
			documentView !== "api" && livePreviewOpen && splitPreview.current ? fieldsViewport : viewport,
	});
	const livePreviewRouteKey = $derived(
		`${globalResource ? "global" : "collection"}:${controller.collectionSlug}:${controller.documentID ?? controller.collectionSlug}`
	);
	let previousLivePreviewRouteKey: string | undefined;
	const livePreview = $derived(controller.collection?.admin.livePreview);
	$effect(() => {
		const state = location.current.state as { openLivePreview?: boolean } | null;
		if (routeDocumentView !== "edit" || state?.openLivePreview !== true) return;
		livePreviewOpen = true;
		// API and edit views have separate route instances. Consume the transient
		// intent on arrival so Back or reload cannot reopen a dismissed preview.
		const { openLivePreview: _, ...remainingState } = state;
		navigate(`${location.current.pathname}${location.current.search}${location.current.hash}`, {
			replace: true,
			state: remainingState,
		});
	});
	const dirty = $derived(
		documentRouteIsDirty({
			documentValuesDirty: controller.hasUnsavedChanges,
			creatingAuthUser: controller.creatingAuthUser,
			password: credentials.password,
			passwordConfirmation: credentials.passwordConfirmation,
		})
	);
	const navigationBlocker = useBlocker(() => dirty || form.submitting);

	$effect(() => {
		return runtime.registerContentLocaleBlocker(
			() =>
				dirty ||
				form.submitting ||
				controller.publicationOperation ||
				controller.upload.busy ||
				controller.duplicateOperation ||
				controller.copyLocaleOperation
		);
	});
	$effect(() => {
		if (!dirty && !form.submitting) return;
		const preventUnload = (event: BeforeUnloadEvent) => {
			event.preventDefault();
			event.returnValue = "";
		};
		window.addEventListener("beforeunload", preventUnload);
		return () => window.removeEventListener("beforeunload", preventUnload);
	});
	$effect(() => {
		if (!dirty && !form.submitting && navigationBlocker.state === "blocked") {
			navigationBlocker.reset();
		}
	});
	$effect(() => {
		const routeKey = livePreviewRouteKey;
		if (previousLivePreviewRouteKey === undefined) {
			previousLivePreviewRouteKey = routeKey;
			return;
		}
		if (routeKey !== previousLivePreviewRouteKey) {
			previousLivePreviewRouteKey = routeKey;
			livePreviewOpen = false;
		}
	});

	function toggleLivePreview() {
		if (!livePreviewOpen && routeDocumentView === "api") {
			navigate(editPath, { replace: true, state: { openLivePreview: true } });
			return;
		}
		livePreviewOpen = !livePreviewOpen;
		if (livePreviewOpen) documentView = "edit";
	}

	function selectCustomDocumentView(view: string) {
		documentView = view;
		if (routeDocumentView === "api") navigate(editPath, { replace: true });
	}

	async function submitDocument(event: Event, publish: boolean) {
		event.preventDefault();
		if (controller.creatingAuthUser) {
			credentialIssue = validateNewUserCredentials(
				credentials,
				controller.collection?.authSettings,
				runtime.i18n
			);
			if (credentialIssue !== undefined) {
				document.getElementById("ridu-new-user-password")?.focus();
				return;
			}
		}
		const saved = controller.creatingAuthUser
			? await saveAuthCreateWithClearedCredentials(
					{
						password: credentials.password,
						passwordConfirmation: credentials.passwordConfirmation,
					},
					replaceNewUserCredentials,
					(password) => controller.save({ password, publish })
				)
			: await controller.save({ publish });
		if (saved) {
			resetNewUserCredentials();
			return;
		}
		const issuePath = form.issues[0]?.path;
		if (issuePath === undefined) return;
		documentView = "edit";
		await tick();
		if (fieldsViewport) await focusFieldIssue(issuePath, fieldsViewport);
	}

	function handleSave(event: Event) {
		return submitDocument(
			event,
			!controller.creating &&
				controller.versionedCollection &&
				controller.currentStatus === "published"
		);
	}

	function handlePublish(event: Event) {
		if (!controller.creating && controller.currentStatus === "draft" && !dirty) {
			event.preventDefault();
			return controller.changePublication("published");
		}
		return submitDocument(event, true);
	}

	function cancelNavigation() {
		if (navigationBlocker.state === "blocked") navigationBlocker.reset();
	}

	function discardAndNavigate() {
		if (form.submitting || navigationBlocker.state !== "blocked") return;
		controller.discardChanges();
		resetNewUserCredentials();
		navigationBlocker.proceed();
	}

	const breadcrumbs = getBreadcrumbs();
	$effect(() => {
		if (!breadcrumbs) return;
		const entry = { pathname: location.current.pathname, label: controller.documentHeading };
		breadcrumbs.document = entry;
		return () => {
			if (breadcrumbs.document === entry) breadcrumbs.document = undefined;
		};
	});

	function resetNewUserCredentials() {
		replaceNewUserCredentials({ password: "", passwordConfirmation: "" });
	}

	function replaceNewUserCredentials(next: AuthCreateCredentials) {
		credentials = next;
		credentialIssue = undefined;
	}
</script>

<section
	{@attach attachViewport}
	data-slot="document-viewport"
	class={["ridu-document", documentView === "api" && "ridu-document--api"]}
	style:--admin-sticky-offset={`${actionBarHeight}px`}
>
	<header class="ridu-document-header">
		<div class="ridu-document-heading">
			<h1
				class="ridu-document-title"
				aria-busy={controller.loading &&
					controller.currentDocument === undefined &&
					!controller.creating}
			>
				{controller.documentHeading}
			</h1>
			{#if !controller.creating || routeDocumentView === "api"}
				<nav class="ridu-document-tabs" aria-label={runtime.i18n.t("documents:documentViews")}>
					<Link
						to={editPath}
						class={["ridu-document-tab", documentView === "edit" && "ridu-document-tab--active"]}
						onclick={() => (documentView = "edit")}
					>
						{runtime.i18n.t("documents:edit")}
					</Link>

					{#each customDocumentViews as view (view.key)}
						<button
							type="button"
							class={[
								"ridu-document-tab",
								documentView === view.key && "ridu-document-tab--active",
							]}
							onclick={() => selectCustomDocumentView(view.key)}
						>
							{view.labelKey === undefined ? view.label : runtime.i18n.t(view.labelKey)}
						</button>
					{/each}
					{#if !controller.creating && controller.versionedCollection && controller.canReadVersions}
						<Link class="ridu-document-tab" to={versionHistoryPath}>
							{runtime.i18n.t("documents:versions")}
						</Link>
					{/if}
					<Link
						to={apiPath}
						class={["ridu-document-tab", documentView === "api" && "ridu-document-tab--active"]}
					>
						{runtime.i18n.t("documents:api")}
					</Link>
				</nav>
			{/if}
		</div>
		{#if documentView !== "api" && controller.collection?.admin.description}
			<p class="ridu-document-description">
				{runtime.i18n.text(
					controller.collection.admin.description,
					controller.collection.admin.descriptionTranslations
				)}
			</p>
		{/if}
	</header>

	{#if documentView !== "api"}
		<!-- Editor-owned overlays and extension actions must not survive a route or locale identity change. -->
		{#key `${livePreviewRouteKey}:${activeLocale ?? ""}`}
			<DocumentActionBar
				{controller}
				{extensionHost}
				{localeEnabled}
				{activeLocale}
				{livePreviewOpen}
				onTogglePreview={toggleLivePreview}
				onSave={handleSave}
				onPublish={handlePublish}
				bind:height={actionBarHeight}
			/>
		{/key}
	{/if}

	<div
		class={[
			"ridu-document-body",
			{ "ridu-document-body--preview": documentView !== "api" && livePreviewOpen },
		]}
	>
		<div
			{@attach attachFieldsViewport}
			data-slot="document-fields-viewport"
			class="ridu-document-editor"
		>
			<div class="ridu-document-fields">
				{#if controller.lock.error !== undefined}
					<Banner class="ridu-document-notice" tone="destructive">
						<span class="ridu-document-notice__message">{controller.lock.error}</span>
						<Button
							type="button"
							variant="outline"
							size="xs"
							disabled={controller.lock.operation}
							onclick={controller.lock.retry}
						>
							{runtime.i18n.t("general:retry")}
						</Button>
					</Banner>
				{/if}
				{#if controller.lock.lockedByAnotherEditor}
					<Banner class="ridu-document-notice" tone="warning">
						<LockKeyholeIcon class="ridu-document-notice__icon" aria-hidden="true" />
						<span>
							{runtime.i18n.t("documents:lockedReadOnly", {
								owner: controller.lock.ownerLabel ?? runtime.i18n.t("documents:anotherEditor"),
							})}
						</span>
					</Banner>
				{/if}
				{#if documentView !== "api" && controller.error !== undefined}
					<Banner class="ridu-document-notice" tone="destructive">
						<span class="ridu-document-notice__error-marker" aria-hidden="true"></span>
						{controller.error}
					</Banner>
				{/if}

				{#if controller.saveOutcomeUncertain}
					<Banner class="ridu-document-notice" tone="warning">
						{runtime.i18n.t(
							controller.creating
								? "uploads:outcomeUnknownDescription"
								: "uploads:saveOutcomeUnknownDescription"
						)}
						{#if !controller.creating}
							<Button
								variant="outline"
								size="sm"
								disabled={controller.loading}
								onclick={controller.refresh}
							>
								{runtime.i18n.t("uploads:reloadSaved")}
							</Button>
						{/if}
					</Banner>
				{/if}

				{#if documentView === "api"}
					<DocumentAPIView
						resourceSlug={slug}
						documentID={routeDocumentID}
						{globalResource}
						contentLocale={activeLocale}
						fallbackValue={form.values}
						prepared={preparedRoute?.kind === "collection-api" ||
						preparedRoute?.kind === "global-api"
							? preparedRoute.document.document
							: undefined}
					/>
				{:else if controller.loading}
					<div
						class="ridu-document-loading"
						data-ridu-loading-surface="document"
						aria-label={runtime.i18n.t("documents:loadingDocument")}
					>
						<Skeleton class="ridu-document-loading__heading" />
						<Skeleton class="ridu-document-loading__input" />
						<Skeleton class="ridu-document-loading__textarea" />
					</div>
				{:else if !controller.creating && controller.currentDocument === undefined}
					<!-- A failed read has no editable document; the recovery/error banner remains visible. -->
				{:else if customDocumentView !== undefined && controller.collection !== undefined && controller.currentDocument !== undefined}
					<customDocumentView.component
						collection={controller.collection}
						document={controller.currentDocument}
						host={extensionHost}
						i18n={runtime.i18n}
					/>
				{:else}
					<form
						class="ridu-document-form"
						dir={activeLocaleConfig?.rtl ? "rtl" : "ltr"}
						novalidate
						onsubmit={handleSave}
					>
						<fieldset class="ridu-document-form__fieldset" disabled={form.submitting}>
							{#if controller.uploadCollection && controller.collection?.uploadSettings}
								{#key JSON.stringify([globalResource, slug, routeDocumentID, activeLocale])}
									<UploadDocumentPreview
										draft={controller.upload}
										settings={controller.collection.uploadSettings}
										editable={controller.canEditUpload}
									/>
								{/key}
							{/if}

							{#if controller.creatingAuthUser}
								<DocumentCredentials
									bind:credentials
									bind:issue={credentialIssue}
									minimumLength={controller.collection?.authSettings?.passwordMinLength}
								/>
							{/if}

							<DocumentFieldSections
								fields={controller.documentFields}
								{form}
								stacked={livePreviewOpen}
							/>
							<button
								class="ridu-document-form__submit"
								type="submit"
								aria-busy={form.submitting}
								disabled={!controller.canSave ||
									(!controller.creating &&
										controller.versionedCollection &&
										controller.currentStatus === "published" &&
										!controller.canPublish)}
							>
								{runtime.i18n.t("documents:save")}
							</button>
						</fieldset>
					</form>
				{/if}
			</div>
		</div>

		{#if documentView !== "api" && livePreviewOpen && livePreview !== undefined && controller.currentDocument !== undefined}
			{#key livePreviewRouteKey}
				<LivePreviewPanel
					preview={livePreview}
					collection={controller.collectionSlug}
					documentID={controller.documentID ?? controller.collectionSlug}
					resource={globalResource ? "global" : "collection"}
					values={form.values}
				/>
			{/key}
		{/if}
	</div>
</section>

<Dialog
	open={navigationBlocker.state === "blocked"}
	onOpenChange={(open) => {
		if (!open) cancelNavigation();
	}}
>
	<DialogContent variant="confirmation">
		<DialogHeader>
			<DialogTitle>
				{runtime.i18n.t("documents:leaveWithoutSavingQuestion")}
			</DialogTitle>
			<DialogDescription>
				{runtime.i18n.t("documents:leaveWithoutSavingDescription")}
			</DialogDescription>
		</DialogHeader>
		<DialogFooter>
			<Button variant="outline" onclick={cancelNavigation}>
				{runtime.i18n.t("documents:keepEditing")}
			</Button>
			<Button disabled={form.submitting} onclick={discardAndNavigate}>
				{runtime.i18n.t("documents:leaveWithoutSaving")}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>

<DocumentOperationDialogs {controller} />
