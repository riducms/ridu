<script lang="ts" module>
	import type { SchemaField } from "@riducms/protocol";

	function fieldUsesLocalization(field: SchemaField): boolean {
		if (field.localized === true) return true;
		if (field.nested?.fields.some(fieldUsesLocalization)) return true;
		return field.blocks?.types.some((block) => block.fields.some(fieldUsesLocalization)) === true;
	}

	function withContentLocale(path: string, locale: string | undefined) {
		if (locale === undefined) return path;
		return `${path}?${new URLSearchParams({ locale }).toString()}`;
	}
</script>

<script lang="ts">
	import type { AdminDocumentExtensionHost } from "@riducms/plugin";
	import { Link, useBlocker, useLocation, useNavigate, useParams } from "@hvniel/svelte-router";
	import { tick, untrack } from "svelte";
	import CopyIcon from "~icons/lucide/copy";
	import DownloadIcon from "~icons/lucide/download";
	import FileUpIcon from "~icons/lucide/file-up";
	import Trash2Icon from "~icons/lucide/trash-2";
	import KeyRoundIcon from "~icons/lucide/key-round";
	import MonitorUpIcon from "~icons/lucide/monitor-up";
	import MoreVerticalIcon from "~icons/lucide/more-vertical";
	import LockKeyholeIcon from "~icons/lucide/lock-keyhole";

	import { Banner } from "@admin/components/ui/banner";
	import { Button, buttonVariants } from "@admin/components/ui/button";
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle,
	} from "@admin/components/ui/dialog";
	import { Popover, PopoverContent, PopoverTrigger } from "@admin/components/ui/popover";
	import { Select, SelectContent, SelectItem, SelectTrigger } from "@admin/components/ui/select";
	import { Skeleton } from "@admin/components/ui/skeleton";
	import {
		collectionPath,
		createDocumentAPIPath,
		createDocumentPath,
		documentIDFromAdminPath,
		documentAPIPath,
		documentPath,
		documentVersionsPath,
		globalAPIPath,
		globalPath,
		globalVersionsPath,
	} from "@admin/core/routing/admin-paths";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { DocumentController } from "@admin/features/documents/document-controller.svelte";
	import {
		documentRouteIsDirty,
		saveAuthCreateWithClearedCredentials,
		type AuthCreateCredentials,
	} from "@admin/features/documents/document-route-dirty";
	import LivePreviewPanel from "@admin/features/documents/live-preview-panel.svelte";
	import UploadDocumentPreview from "@admin/features/documents/upload-document-preview-loader.svelte";
	import DocumentFieldSections from "@admin/features/documents/document-field-sections.svelte";
	import DocumentAPIView from "@admin/features/documents/document-api-view-loader.svelte";

	const runtime = getAdminRuntime();
	const notifications = getNotificationCenter();
	const navigate = useNavigate();
	const params = $derived(useParams<"collection" | "document" | "global">());
	const location = $derived(useLocation());
	const globalResource = $derived(params.global !== undefined);
	const slug = $derived(params.global ?? params.collection ?? "");
	let routeDocumentID = $state(
		untrack(() =>
			params.document === undefined ? undefined : documentIDFromAdminPath(location.pathname)
		)
	);
	$effect(() => {
		if (params.document === undefined) {
			routeDocumentID = undefined;
			return;
		}
		const decoded = documentIDFromAdminPath(location.pathname);
		// A parent route can remain mounted for one render after navigation has
		// moved to the collection list. Retain its exact ID until unmount so the
		// document controller does not transiently enter create mode.
		if (decoded !== undefined) routeDocumentID = decoded;
	});
	const localization = $derived(runtime.manifest?.application.localization);
	const resourceSchema = $derived(
		(globalResource ? runtime.manifest?.globals : runtime.manifest?.collections)?.find(
			(candidate) => candidate.slug === slug
		)
	);
	const localeEnabled = $derived(resourceSchema?.fields.some(fieldUsesLocalization) === true);
	const requestedLocale = $derived(new URLSearchParams(location.search).get("locale"));
	const activeLocale = $derived(
		localization?.locales.some((locale) => locale.code === requestedLocale)
			? (requestedLocale ?? localization?.defaultLocale)
			: (runtime.contentLocale ?? localization?.defaultLocale)
	);
	const activeLocaleConfig = $derived(
		localeEnabled ? localization?.locales.find((locale) => locale.code === activeLocale) : undefined
	);
	const routeDocumentView = $derived<"edit" | "api">(
		location.pathname.endsWith("/api") ? "api" : "edit"
	);
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
		get view() {
			return routeDocumentView;
		},
		get locale() {
			return activeLocale;
		},
	});
	const form = controller.form;
	const {
		assetFilename,
		assetURL,
		canDelete,
		canDuplicate,
		canEditImage,
		canPublish,
		canReadVersions,
		canSave,
		canTakeOverLock,
		canUnpublish,
		collection,
		collectionSlug,
		copiedAssetURL,
		creating,
		currentDocument,
		currentStatus,
		draftsCollection,
		documentFields,
		documentHeading,
		documentID,
		documentView,
		duplicateOperation,
		error,
		imageOperation,
		imageOutcomeUncertain,
		loading,
		lockedByAnotherEditor,
		hasUnsavedChanges,
		lockOperation,
		lockOwnerLabel,
		lockUpdatedAt,
		pendingRestore,
		saveOutcomeUncertain,
		selectedFile,
		uploadCollection,
		versionOperation,
		versionedCollection,
		versions,
	} = $derived(controller);
	const {
		changePublication,
		copyAssetURL,
		duplicate,
		documentDate,
		remove,
		requestRestore,
		restore,
		save,
		takeOverLock,
		updateUploadImage,
		versionDate,
	} = controller;
	const customDocumentActions = $derived(
		globalResource
			? []
			: runtime.documentActions.filter(
					(action) =>
						(action.collection === undefined || action.collection === collectionSlug) &&
						(action.requires === undefined || form.access?.operations[action.requires] === true)
				)
	);
	const customDocumentViews = $derived(
		currentDocument === undefined
			? []
			: runtime.documentViews.filter(
					(view) => view.collection === undefined || view.collection === collectionSlug
				)
	);
	const customDocumentView = $derived(
		customDocumentViews.find((view) => view.key === documentView)
	);
	const extensionHost: AdminDocumentExtensionHost = {
		refresh: controller.refresh,
		notify: (tone, title, message) => notifications[tone]({ title, message }),
	};
	let unlockOperation = $state(false);
	let newUserPassword = $state("");
	let newUserPasswordConfirmation = $state("");
	let credentialIssue = $state<string>();
	let livePreviewOpen = $state(false);
	let moreActionsOpen = $state(false);
	let copyLocaleOperation = $state(false);
	const livePreviewRouteKey = $derived(
		`${globalResource ? "global" : "collection"}:${collectionSlug}:${documentID ?? collectionSlug}`
	);
	let previousLivePreviewRouteKey: string | undefined;
	let dismissedLock = $state<string>();
	const livePreview = $derived(collection?.admin.livePreview);
	const creatingAuthUser = $derived(
		creating && !globalResource && collection?.capabilities.auth === true
	);
	const dirty = $derived(
		documentRouteIsDirty({
			documentValuesDirty: hasUnsavedChanges,
			creatingAuthUser,
			password: newUserPassword,
			passwordConfirmation: newUserPasswordConfirmation,
		})
	);
	const navigationBlocker = useBlocker(() => dirty || form.submitting);
	const canForceUnlock = $derived(
		!globalResource &&
			!creating &&
			collection?.capabilities.auth === true &&
			(collection.authSettings?.maxLoginAttempts ?? 0) > 0 &&
			form.access?.operations.update === true
	);
	$effect(() => {
		if (activeLocale === undefined || requestedLocale === activeLocale) return;
		const next = new URLSearchParams(location.search);
		next.set("locale", activeLocale);
		navigate(`${location.pathname}?${next.toString()}${location.hash}`, { replace: true });
	});
	$effect(() => {
		return runtime.registerContentLocaleBlocker(
			() =>
				dirty ||
				form.submitting ||
				controller.versionOperation ||
				controller.imageOperation ||
				controller.duplicateOperation ||
				copyLocaleOperation
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

	async function forceUnlock() {
		if (collection === undefined || currentDocument === undefined) return;
		unlockOperation = true;
		try {
			await runtime.client.forceUnlock(collection.slug, currentDocument.id);
			notifications.success({ title: runtime.i18n.t("documents:accountUnlocked") });
		} catch (cause) {
			notifications.error({
				title: runtime.i18n.t("documents:accountUnlockFailed"),
				message: cause instanceof Error ? cause.message : undefined,
			});
		} finally {
			unlockOperation = false;
		}
	}

	function toggleLivePreview() {
		livePreviewOpen = !livePreviewOpen;
		if (livePreviewOpen) {
			controller.documentView = "edit";
			navigate(editPath, { replace: true });
		}
	}

	function selectCustomDocumentView(view: string) {
		controller.documentView = view;
		if (routeDocumentView === "api") navigate(editPath, { replace: true });
	}

	async function copyFromLocale(source: string) {
		if (
			activeLocale === undefined ||
			source === activeLocale ||
			currentDocument === undefined ||
			copyLocaleOperation
		)
			return;
		copyLocaleOperation = true;
		try {
			if (globalResource) {
				await runtime.client.copyGlobalLocale(
					slug,
					{ from: source, to: activeLocale },
					{ revision: currentDocument._revision }
				);
			} else {
				await runtime.client.copyLocale(
					slug,
					currentDocument.id,
					{ from: source, to: activeLocale },
					{ revision: currentDocument._revision }
				);
			}
			await controller.refresh();
			runtime.documentsChanged();
			notifications.success({
				title: runtime.i18n.t("documents:localeCopied"),
				message: runtime.i18n.t("documents:localeCopiedDescription", {
					source,
					destination: activeLocale,
				}),
			});
		} catch (cause) {
			notifications.error({
				title: runtime.i18n.t("documents:localeNotCopied"),
				message:
					cause instanceof Error ? cause.message : runtime.i18n.t("documents:localeCopyFailed"),
			});
		} finally {
			copyLocaleOperation = false;
		}
	}

	async function handleTakeover() {
		await takeOverLock();
	}

	const lockKey = $derived(
		lockedByAnotherEditor
			? `${collectionSlug}:${controller.documentID ?? ""}:${lockOwnerLabel ?? ""}:${lockUpdatedAt ?? ""}`
			: undefined
	);

	async function submitDocument(event: Event, publish: boolean) {
		event.preventDefault();
		if (creatingAuthUser) {
			credentialIssue = validateNewUserPassword();
			if (credentialIssue !== undefined) {
				document.getElementById("ridu-new-user-password")?.focus();
				return;
			}
		}
		const saved = creatingAuthUser
			? await saveAuthCreateWithClearedCredentials(
					{
						password: newUserPassword,
						passwordConfirmation: newUserPasswordConfirmation,
					},
					replaceNewUserCredentials,
					(password) => save({ password, publish })
				)
			: await save({ publish });
		if (saved) {
			resetNewUserCredentials();
			return;
		}
		const issuePath = controller.revealFirstIssue();
		if (issuePath === undefined) return;
		await tick();
		focusIssue(issuePath);
	}

	async function handleSave(event: Event) {
		await submitDocument(event, !creating && versionedCollection && currentStatus === "published");
	}

	async function handlePublish(event: Event) {
		await submitDocument(event, true);
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

	function resetNewUserCredentials() {
		replaceNewUserCredentials({ password: "", passwordConfirmation: "" });
	}

	function replaceNewUserCredentials(credentials: AuthCreateCredentials) {
		newUserPassword = credentials.password;
		newUserPasswordConfirmation = credentials.passwordConfirmation;
		credentialIssue = undefined;
	}

	function validateNewUserPassword() {
		if (!creatingAuthUser || collection?.authSettings === undefined) return undefined;
		if (newUserPassword === "") return runtime.i18n.t("documents:enterNewAccountPassword");
		if (Array.from(newUserPassword).length < collection.authSettings.passwordMinLength) {
			return runtime.i18n.t("documents:passwordTooShort", {
				minimum: runtime.i18n.formatNumber(collection.authSettings.passwordMinLength),
			});
		}
		if (
			new TextEncoder().encode(newUserPassword).length > collection.authSettings.passwordMaxBytes
		) {
			return runtime.i18n.t("documents:passwordTooLong", {
				maximum: runtime.i18n.formatNumber(collection.authSettings.passwordMaxBytes),
			});
		}
		if (newUserPassword !== newUserPasswordConfirmation)
			return runtime.i18n.t("documents:passwordsDoNotMatch");
		return undefined;
	}

	function focusIssue(path: string) {
		const segments = path.split(".");
		while (segments.length > 0) {
			const candidate = segments.join(".");
			const container = document.querySelector<HTMLElement>(
				`[data-field-path="${CSS.escape(candidate)}"]`
			);
			if (container !== null) {
				container.scrollIntoView({ behavior: "smooth", block: "center" });
				container
					.querySelector<HTMLElement>("input, textarea, button, [tabindex]:not([tabindex='-1'])")
					?.focus({ preventScroll: true });
				return;
			}
			segments.pop();
		}
	}
</script>

<section
	class="flex min-h-screen flex-col md:h-full md:min-h-0 md:overflow-x-hidden md:overflow-y-auto"
>
	<header class="shrink-0 bg-background">
		<div
			class="flex min-h-[88px] flex-col justify-center gap-3 px-5 py-3 sm:px-8 lg:flex-row lg:items-center lg:justify-between lg:px-[60px]"
		>
			<h1
				class="text-[30px] font-semibold leading-tight tracking-[-0.025em] text-foreground-strong"
				aria-busy={loading && currentDocument === undefined && !creating}
			>
				{documentHeading}
			</h1>
			<nav
				class="flex min-w-0 shrink-0 items-center gap-5 overflow-x-auto lg:ms-auto"
				aria-label={runtime.i18n.t("documents:documentViews")}
			>
				<Link
					to={editPath}
					class={[
						"mt-2 flex h-8 items-center px-3 text-[12.5px] text-foreground-muted lg:mt-0",
						documentView === "edit" && "bg-control text-foreground-strong",
					]}
					onclick={() => (controller.documentView = "edit")}
				>
					{runtime.i18n.t("documents:edit")}
				</Link>
				<Link
					to={apiPath}
					class={[
						"mt-2 flex h-8 items-center px-3 text-[12.5px] text-foreground-muted lg:mt-0",
						documentView === "api" && "bg-control text-foreground-strong",
					]}
				>
					{runtime.i18n.t("documents:api")}
				</Link>
				{#each customDocumentViews as view (view.key)}<button
						type="button"
						class={[
							"mt-2 h-8 shrink-0 px-3 text-[12.5px] text-foreground-muted lg:mt-0",
							documentView === view.key && "bg-control text-foreground-strong",
						]}
						onclick={() => selectCustomDocumentView(view.key)}
					>
						{view.labelKey === undefined ? view.label : runtime.i18n.t(view.labelKey)}
					</button>{/each}
				{#if !creating && versionedCollection && canReadVersions}<Link
						class="mt-2 flex h-8 items-center px-3 text-[12.5px] text-foreground-muted hover:text-foreground-strong lg:mt-0"
						to={versionHistoryPath}
					>
						{runtime.i18n.t("documents:versions")}
					</Link>{/if}
			</nav>
		</div>
	</header>

	<div
		class="relative sticky top-[-3.5rem] z-20 shrink-0 border-y border-control-border bg-background/96 backdrop-blur-md before:pointer-events-none before:absolute before:top-[-1px] before:end-[-1rem] before:h-px before:w-4 before:bg-control-border after:pointer-events-none after:absolute after:end-[-1rem] after:bottom-[-1px] after:h-px after:w-4 after:bg-control-border xl:top-0 xl:grid xl:grid-cols-1"
	>
		<div
			class="flex min-h-14 flex-wrap items-center gap-x-10 gap-y-1 px-5 py-2 text-[11.5px] text-foreground-faint sm:px-8 lg:px-[60px] xl:col-start-1 xl:row-start-1 xl:pe-[620px]"
		>
			{#if localeEnabled && activeLocale !== undefined && localization !== undefined && !creating && currentDocument !== undefined && localization.locales.length > 1}
				<div class="flex items-center gap-2.5">
					<Select
						type="single"
						value=""
						disabled={form.dirty || loading || form.submitting || copyLocaleOperation}
						onValueChange={copyFromLocale}
					>
						<SelectTrigger
							size="compact"
							class="min-w-28"
							aria-label={runtime.i18n.t("documents:copyFromLocale")}
						>
							<span>
								{copyLocaleOperation
									? runtime.i18n.t("documents:copying")
									: runtime.i18n.t("documents:copyFrom")}
							</span>
						</SelectTrigger>
						<SelectContent>
							{#each localization.locales.filter((locale) => locale.code !== activeLocale) as locale (locale.code)}
								<SelectItem value={locale.code} label={locale.label}>
									<span>{locale.label}</span>
									<span class="ms-1 text-foreground-faint">{locale.code}</span>
								</SelectItem>
							{/each}
						</SelectContent>
					</Select>
				</div>
			{/if}
			<span>
				{runtime.i18n.t("documents:status")}:
				<strong class="font-semibold text-foreground-strong">
					{versionedCollection
						? runtime.i18n.t(currentStatus === "draft" ? "documents:draft" : "documents:published")
						: creating
							? runtime.i18n.t("documents:draft")
							: runtime.i18n.t("documents:saved")}
				</strong>
			</span>
			{#if !creating && currentDocument !== undefined}
				<span class="hidden sm:inline">
					{runtime.i18n.t("documents:lastModifiedLabel")}:
					<strong class="font-semibold text-foreground-strong">
						{documentDate(currentDocument.updatedAt)}
					</strong>
				</span>
				<span class="hidden md:inline">
					{runtime.i18n.t("documents:createdLabelPlain")}:
					<strong class="font-semibold text-foreground-strong">
						{documentDate(currentDocument.createdAt)}
					</strong>
				</span>
			{/if}
		</div>

		<div
			class="flex min-h-12 items-center gap-2 border-t border-control-border bg-background px-5 sm:px-8 lg:px-[60px] xl:col-start-1 xl:row-start-1 xl:min-h-14 xl:w-fit xl:justify-self-end xl:border-t-0 xl:pe-[60px] xl:ps-0"
		>
			{#if !creating && currentDocument !== undefined && livePreview !== undefined}
				<Button
					variant="outline"
					size="icon-sm"
					onclick={toggleLivePreview}
					aria-label={runtime.i18n.t("documents:livePreview")}
					aria-pressed={livePreviewOpen}
				>
					<MonitorUpIcon class="size-3.5" aria-hidden="true" />
				</Button>
			{/if}
			{#if canTakeOverLock}<Button
					variant="outline"
					size="sm"
					disabled={lockOperation}
					onclick={handleTakeover}
				>
					<LockKeyholeIcon class="size-3.5" aria-hidden="true" />{lockOperation
						? runtime.i18n.t("documents:takingOver")
						: runtime.i18n.t("documents:takeOver")}
				</Button>{/if}
			{#if creating && draftsCollection && canPublish && !uploadCollection && !creatingAuthUser}
				<Button
					variant="outline"
					size="sm"
					disabled={!canSave}
					aria-busy={form.submitting}
					onclick={handleSave}
				>
					{form.submitting
						? runtime.i18n.t("documents:saving")
						: runtime.i18n.t("documents:saveDraft")}
				</Button>
				<Button size="sm" disabled={!canSave} aria-busy={form.submitting} onclick={handlePublish}>
					{form.submitting
						? runtime.i18n.t("documents:publishing")
						: runtime.i18n.t("documents:publish")}
				</Button>
			{:else if creating && versionedCollection && !draftsCollection}
				<Button
					size="sm"
					disabled={!canSave || !canPublish}
					aria-busy={form.submitting}
					onclick={handlePublish}
				>
					{form.submitting
						? runtime.i18n.t("documents:publishing")
						: runtime.i18n.t("documents:publish")}
				</Button>
			{:else}
				<Button
					size="sm"
					disabled={!canSave ||
						(versionedCollection && currentStatus === "published" && !canPublish)}
					aria-busy={form.submitting}
					onclick={handleSave}
				>
					{form.submitting
						? runtime.i18n.t(
								versionedCollection && currentStatus === "published"
									? "documents:publishing"
									: "documents:saving"
							)
						: creating && draftsCollection
							? runtime.i18n.t("documents:saveDraft")
							: versionedCollection
								? currentStatus === "published"
									? runtime.i18n.t("documents:publishChanges")
									: runtime.i18n.t("documents:saveDraft")
								: runtime.i18n.t("documents:save")}
				</Button>
			{/if}
			{#if !creating && versionedCollection && currentStatus === "draft" && canPublish}
				<Button
					variant="outline"
					size="sm"
					disabled={versionOperation || form.dirty || form.submitting}
					onclick={() => changePublication("published")}
				>
					{runtime.i18n.t("documents:publish")}
				</Button>
			{/if}
			<div class="ms-auto flex items-center gap-2">
				{#if collection !== undefined && currentDocument !== undefined && !lockedByAnotherEditor}{#each customDocumentActions as action (action.key)}<action.component
							{collection}
							document={currentDocument}
							host={extensionHost}
							i18n={runtime.i18n}
						/>{/each}{/if}
				{#if (!globalResource && !creating && canDuplicate) || (!globalResource && !creating && uploadCollection && assetURL !== undefined) || (!creating && versionedCollection && currentStatus === "published" && canUnpublish) || (!globalResource && !creating && canDelete) || canForceUnlock}
					<Popover bind:open={moreActionsOpen}>
						<PopoverTrigger
							class={buttonVariants({ variant: "outline", size: "sm", class: "size-9 p-0" })}
							aria-label={runtime.i18n.t("documents:moreActions")}
						>
							<MoreVerticalIcon class="size-4" aria-hidden="true" />
						</PopoverTrigger>
						<PopoverContent align="end" class="w-52 gap-1 p-1.5">
							{#if !globalResource && !creating && canDuplicate}<Button
									variant="ghost"
									class="w-full justify-start"
									disabled={duplicateOperation}
									onclick={() => {
										moreActionsOpen = false;
										duplicate();
									}}
								>
									<CopyIcon class="size-3.5" />{duplicateOperation
										? runtime.i18n.t("documents:duplicating")
										: runtime.i18n.t("documents:duplicate")}
								</Button>{/if}
							{#if !globalResource && !creating && uploadCollection && assetURL !== undefined}<Button
									variant="ghost"
									class="w-full justify-start"
									href={assetURL}
									download={assetFilename}
								>
									<DownloadIcon class="size-3.5" />
									{runtime.i18n.t("documents:download")}
								</Button>{/if}
							{#if !creating && versionedCollection && currentStatus === "published" && canUnpublish}<Button
									variant="ghost"
									class="w-full justify-start"
									disabled={versionOperation || form.dirty || form.submitting}
									onclick={() => {
										moreActionsOpen = false;
										changePublication("draft");
									}}
								>
									{runtime.i18n.t("documents:unpublish")}
								</Button>{/if}
							{#if !globalResource && !creating && canDelete}<Button
									variant="ghost"
									class="w-full justify-start text-destructive hover:bg-destructive/7 hover:text-destructive"
									onclick={() => {
										moreActionsOpen = false;
										controller.deleteDialogOpen = true;
									}}
								>
									<Trash2Icon class="size-3.5" />
									{runtime.i18n.t(
										uploadCollection ? "documents:deleteAsset" : "documents:deleteDocument"
									)}
								</Button>{/if}
							{#if canForceUnlock}<Button
									variant="ghost"
									class="w-full justify-start"
									disabled={unlockOperation}
									onclick={() => {
										moreActionsOpen = false;
										forceUnlock();
									}}
								>
									<KeyRoundIcon class="size-3.5" aria-hidden="true" />{unlockOperation
										? runtime.i18n.t("documents:unlocking")
										: runtime.i18n.t("documents:forceUnlock")}
								</Button>{/if}
						</PopoverContent>
					</Popover>
				{/if}
			</div>
		</div>
	</div>

	<div
		class={[
			"flex min-h-0 flex-1 flex-col",
			livePreviewOpen && "min-[1240px]:flex-row min-[1240px]:overflow-hidden",
		]}
	>
		<div class={["min-w-0 flex-1", livePreviewOpen && "min-[1240px]:overflow-y-auto"]}>
			<div class="w-full px-5 py-8 sm:px-8 sm:py-9 lg:px-[60px]">
				{#if lockedByAnotherEditor}
					<Banner class="mb-6" tone="warning">
						<LockKeyholeIcon class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
						<span>
							{runtime.i18n.t("documents:lockedReadOnly", {
								owner: lockOwnerLabel ?? runtime.i18n.t("documents:anotherEditor"),
							})}
						</span>
					</Banner>
				{/if}
				{#if error !== undefined}
					<Banner class="mb-6" tone="destructive">
						<span class="size-2 shrink-0 rounded-full bg-destructive" aria-hidden="true"></span>
						{error}
					</Banner>
				{/if}

				{#if saveOutcomeUncertain}
					<Banner class="mb-6" tone="warning">
						{runtime.i18n.t("uploads:outcomeUnknownDescription")}
					</Banner>
				{/if}

				{#if imageOutcomeUncertain}
					<Banner class="mb-6" tone="warning">
						{runtime.i18n.t("uploads:imageOutcomeUnknownDescription")}
					</Banner>
				{/if}

				{#if loading}
					<div class="grid gap-5" aria-label={runtime.i18n.t("documents:loadingDocument")}>
						<Skeleton class="h-12 w-2/3" />
						<Skeleton class="mt-5 h-10.5" />
						<Skeleton class="h-28" />
					</div>
				{:else if documentView === "api"}
					<DocumentAPIView
						resourceSlug={slug}
						resourceLabel={collection === undefined
							? runtime.i18n.t("documents:document")
							: runtime.i18n.text(
									collection.labels.singular,
									collection.labels.singularTranslations
								)}
						documentID={routeDocumentID}
						{globalResource}
						fallbackValue={form.values}
					/>
				{:else if customDocumentView !== undefined && collection !== undefined && currentDocument !== undefined}
					<customDocumentView.component
						{collection}
						document={currentDocument}
						host={extensionHost}
						i18n={runtime.i18n}
					/>
				{:else if uploadCollection && !creating && currentDocument !== undefined}
					{#key currentDocument.id}
						<UploadDocumentPreview
							document={currentDocument}
							editable={canEditImage}
							updating={imageOperation}
							onUpdate={updateUploadImage}
						/>
					{/key}

					<form class="mt-8" dir={activeLocaleConfig?.rtl ? "rtl" : "ltr"} onsubmit={handleSave}>
						<fieldset class="contents" disabled={form.submitting || imageOperation}>
							<DocumentFieldSections fields={documentFields} {form} stacked={livePreviewOpen} />
							<button class="sr-only" type="submit">{runtime.i18n.t("documents:save")}</button>
						</fieldset>
					</form>
				{:else}
					<form
						class="grid gap-y-7"
						dir={activeLocaleConfig?.rtl ? "rtl" : "ltr"}
						onsubmit={handleSave}
					>
						<fieldset class="contents" disabled={form.submitting}>
							{#if creating && uploadCollection}
								<div class="grid gap-2">
									<div class="flex items-baseline justify-between gap-3">
										<span class="ridu-field-label">
											{runtime.i18n.t("uploads:file")}
											<span class="ridu-field-required" aria-hidden="true">*</span>
										</span>
									</div>
									<label
										class="grid min-h-42 cursor-pointer place-items-center rounded-[4px] border border-dashed border-control-border bg-control px-5 py-7 text-center transition-colors hover:border-primary/55 hover:bg-primary/[0.035]"
									>
										<input
											class="sr-only"
											type="file"
											accept={collection?.uploadSettings?.mimeTypes.join(",")}
											required
											disabled={form.access?.operations.create !== true}
											bind:files={controller.selectedFiles}
										/>
										<span>
											<FileUpIcon class="mx-auto size-5 text-foreground-faint" />
											<span class="mt-3 block text-[13.5px] font-medium text-foreground-muted">
												{selectedFile?.name ?? runtime.i18n.t("uploads:chooseFileToUpload")}
											</span>
											<span class="mt-1 block text-[11.5px] text-foreground-faint">
												{runtime.i18n.t("uploads:maximumSize", {
													size: runtime.i18n.formatNumber(
														(collection?.uploadSettings?.maxFileSize ?? 0) / 1_048_576,
														{ maximumFractionDigits: 0, style: "unit", unit: "megabyte" }
													),
												})}
											</span>
										</span>
									</label>
								</div>
							{/if}

							{#if creatingAuthUser}
								<fieldset
									class="grid gap-4 rounded-[4px] border border-control-border bg-control p-4.5"
								>
									<legend class="px-1 text-[13px] font-semibold text-foreground">
										{runtime.i18n.t("documents:credentials")}
									</legend>
									<p class="text-[11.5px] leading-5 text-foreground-faint">
										{runtime.i18n.t("documents:credentialsDescription")}
									</p>
									<div class="grid gap-4 sm:grid-cols-2">
										<label class="grid gap-2" for="ridu-new-user-password">
											<span class="ridu-field-label">
												{runtime.i18n.t("documents:password")}
												<span class="ridu-field-required" aria-hidden="true">*</span>
											</span>
											<input
												id="ridu-new-user-password"
												type="password"
												autocomplete="new-password"
												minlength={collection?.authSettings?.passwordMinLength}
												required
												aria-invalid={credentialIssue !== undefined}
												aria-describedby="ridu-new-user-password-help"
												bind:value={newUserPassword}
												oninput={() => (credentialIssue = undefined)}
											/>
										</label>
										<label class="grid gap-2" for="ridu-new-user-password-confirmation">
											<span class="ridu-field-label">
												{runtime.i18n.t("documents:confirmPassword")}
												<span class="ridu-field-required" aria-hidden="true">*</span>
											</span>
											<input
												id="ridu-new-user-password-confirmation"
												type="password"
												autocomplete="new-password"
												required
												aria-invalid={credentialIssue !== undefined}
												aria-describedby="ridu-new-user-password-help"
												bind:value={newUserPasswordConfirmation}
												oninput={() => (credentialIssue = undefined)}
											/>
										</label>
									</div>
									<p
										id="ridu-new-user-password-help"
										class={credentialIssue === undefined ? "ridu-field-help" : "ridu-field-error"}
										role={credentialIssue === undefined ? undefined : "alert"}
									>
										{credentialIssue ??
											runtime.i18n.t("documents:passwordMinimum", {
												minimum: runtime.i18n.formatNumber(
													collection?.authSettings?.passwordMinLength ?? 8
												),
											})}
									</p>
								</fieldset>
							{/if}

							<DocumentFieldSections fields={documentFields} {form} stacked={livePreviewOpen} />
							<button class="sr-only" type="submit">{runtime.i18n.t("documents:save")}</button>
						</fieldset>
					</form>
				{/if}
			</div>
		</div>

		{#if livePreviewOpen && livePreview !== undefined && currentDocument !== undefined}
			{#key livePreviewRouteKey}
				<LivePreviewPanel
					preview={livePreview}
					collection={collectionSlug}
					documentID={documentID ?? collectionSlug}
					resource={globalResource ? "global" : "collection"}
					values={form.values}
					onclose={() => (livePreviewOpen = false)}
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
	<DialogContent>
		<DialogHeader>
			<p class="font-mono text-[10px] tracking-[0.13em] text-warning uppercase">
				{runtime.i18n.t("documents:unsavedChanges")}
			</p>
			<DialogTitle class="text-[28px] font-semibold leading-tight tracking-[-0.02em]">
				{runtime.i18n.t("documents:leaveWithoutSavingQuestion")}
			</DialogTitle>
			<DialogDescription class="mt-1 text-[13.5px] leading-5 text-foreground-muted">
				{runtime.i18n.t("documents:leaveWithoutSavingDescription")}
			</DialogDescription>
		</DialogHeader>
		<DialogFooter>
			<Button variant="outline" onclick={cancelNavigation}>
				{runtime.i18n.t("documents:keepEditing")}
			</Button>
			<Button variant="destructive" disabled={form.submitting} onclick={discardAndNavigate}>
				{runtime.i18n.t("documents:leaveWithoutSaving")}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>

{#if !globalResource && canDelete}
	{const deleteConfirmation = $derived(
		collection?.capabilities.trash === true
			? {
					title: runtime.i18n.t("documents:moveToTrashQuestion"),
					description: runtime.i18n.t("documents:moveToTrashDescription", {
						label: documentHeading,
					}),
					action: runtime.i18n.t("documents:moveToTrash"),
				}
			: {
					title: runtime.i18n.t("documents:deleteQuestion"),
					description: runtime.i18n.t("documents:deleteDescription", {
						label: documentHeading,
					}),
					action: runtime.i18n.t("documents:deleteDocument"),
				}
	)}

	<Dialog bind:open={controller.deleteDialogOpen}>
		<DialogContent>
			<DialogHeader>
				<p class="font-mono text-[10px] tracking-[0.13em] text-destructive uppercase">
					{runtime.i18n.t("documents:destructiveAction")}
				</p>
				<DialogTitle class="text-[28px] font-semibold leading-tight tracking-[-0.02em]">
					{deleteConfirmation.title}
				</DialogTitle>
				<DialogDescription class="mt-1 text-[13.5px] leading-5 text-foreground-muted">
					{deleteConfirmation.description}
				</DialogDescription>
			</DialogHeader>
			<DialogFooter>
				<Button variant="outline" onclick={() => (controller.deleteDialogOpen = false)}>
					{runtime.i18n.t("documents:cancel")}
				</Button>
				<Button variant="destructive" onclick={remove}>
					{deleteConfirmation.action}
				</Button>
			</DialogFooter>
		</DialogContent>
	</Dialog>
{/if}

<Dialog
	open={lockKey !== undefined && dismissedLock !== lockKey}
	onOpenChange={(open) => {
		if (!open) dismissedLock = lockKey;
	}}
>
	<DialogContent>
		<DialogHeader>
			<p class="font-mono text-[10px] tracking-[0.13em] text-warning uppercase">
				{runtime.i18n.t("documents:documentLocked")}
			</p>
			<DialogTitle class="text-[28px] font-semibold leading-tight tracking-[-0.02em]">
				{runtime.i18n.t("documents:currentlyEditing", {
					owner: lockOwnerLabel ?? runtime.i18n.t("documents:anotherEditor"),
				})}
			</DialogTitle>
			<DialogDescription class="mt-1 text-[13.5px] leading-5 text-foreground-muted">
				{lockUpdatedAt === undefined
					? runtime.i18n.t("documents:editingLeaseActive")
					: runtime.i18n.t("documents:editedSince", { date: documentDate(lockUpdatedAt) })}
				{canTakeOverLock
					? runtime.i18n.t("documents:lockOptionsWithTakeover")
					: runtime.i18n.t("documents:lockOptionsReadOnly")}
			</DialogDescription>
		</DialogHeader>
		<DialogFooter>
			<Link class={buttonVariants({ variant: "ghost" })} to={collectionPath(collectionSlug)}>
				{runtime.i18n.t("documents:goBack")}
			</Link>
			<Button variant="outline" onclick={() => (dismissedLock = lockKey)}>
				{runtime.i18n.t("documents:viewReadOnly")}
			</Button>
			{#if canTakeOverLock}
				<Button disabled={lockOperation} onclick={handleTakeover}>
					{lockOperation
						? runtime.i18n.t("documents:takingOver")
						: runtime.i18n.t("documents:takeOver")}
				</Button>
			{/if}
		</DialogFooter>
	</DialogContent>
</Dialog>

<Dialog bind:open={controller.restoreDialogOpen}>
	<DialogContent>
		<DialogHeader>
			<p class="font-mono text-[10px] tracking-[0.13em] text-warning uppercase">
				{runtime.i18n.t("versions:restore")}
			</p>
			<DialogTitle class="text-[28px] font-semibold leading-tight tracking-[-0.02em]">
				{runtime.i18n.t("versions:restoreRevisionQuestion", {
					revision:
						pendingRestore === undefined ? "—" : runtime.i18n.formatNumber(pendingRestore.Revision),
				})}
			</DialogTitle>
			<DialogDescription class="mt-1 text-[13.5px] leading-5 text-foreground-muted">
				{runtime.i18n.t("versions:restoreDescription")}
			</DialogDescription>
		</DialogHeader>
		<DialogFooter>
			<Button variant="outline" onclick={() => (controller.restoreDialogOpen = false)}>
				{runtime.i18n.t("versions:cancel")}
			</Button>
			<Button
				disabled={pendingRestore === undefined || versionOperation || !controller.canRestoreVersion}
				onclick={() => pendingRestore !== undefined && restore(pendingRestore)}
			>
				{versionOperation
					? runtime.i18n.t("versions:restoring")
					: runtime.i18n.t("versions:restore")}
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
