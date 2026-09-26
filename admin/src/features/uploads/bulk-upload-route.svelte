<script lang="ts">
	import { Dialog as DialogPrimitive } from "bits-ui";
	import { Link, useBlocker, useLocation, useNavigate, useParams } from "@hvniel/svelte-router";
	import {
		Button,
		buttonVariants,
		Dialog as AdminDialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle,
	} from "@riducms/ui";
	import XIcon from "~icons/lucide/x";

	import { Banner } from "@admin/components/ui/banner";
	import { getAdminBootstrapCoordinator } from "@admin/core/bootstrap/admin-bootstrap";
	import { normalizedURLIdentity } from "@admin/core/bootstrap/admin-prepared-state";
	import { getNotificationCenter } from "@admin/core/notifications/notification-center.svelte";
	import { collectionPath, withContentLocale } from "@admin/core/routing/admin-paths";
	import { registerAdminScrollPage } from "@admin/core/routing/admin-scroll.svelte";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import { BulkUploadController } from "@admin/features/uploads/bulk-upload-controller.svelte";
	import BulkUploadEditAll from "@admin/features/uploads/bulk-upload-edit-all.svelte";
	import BulkUploadEditor from "@admin/features/uploads/bulk-upload-editor.svelte";
	import BulkUploadFilePicker from "@admin/features/uploads/bulk-upload-file-picker.svelte";
	import BulkUploadRemote from "@admin/features/uploads/bulk-upload-remote.svelte";
	import BulkUploadSidebar from "@admin/features/uploads/bulk-upload-sidebar.svelte";

	import "@admin/features/uploads/bulk-upload.scss";

	const runtime = getAdminRuntime();
	const bootstrap = getAdminBootstrapCoordinator();
	const notifications = getNotificationCenter();
	const navigate = useNavigate();
	const params = useParams<"collection">();
	const location = useLocation();
	const slug = $derived(params.current.collection ?? "");
	const collection = $derived(runtime.manifest?.collections.find((item) => item.slug === slug));
	const requestedLocale = $derived(new URLSearchParams(location.current.search).get("locale"));
	const contentLocale = $derived(runtime.resolveContentLocale(requestedLocale));
	const activeLocaleConfig = $derived(
		runtime.manifest?.application.localization?.locales.find(
			(locale) => locale.code === contentLocale
		)
	);
	const contentDirection = $derived<"ltr" | "rtl">(
		activeLocaleConfig?.rtl === true ? "rtl" : "ltr"
	);
	const preparedRoute = $derived(
		bootstrap.routeData(location.current.pathname, location.current.search)
	);
	const closePath = $derived(withContentLocale(collectionPath(slug), contentLocale));
	const controller = new BulkUploadController({
		runtime,
		notifications,
		get slug() {
			return slug;
		},
		get locale() {
			return contentLocale;
		},
		get collection() {
			return collection;
		},
		get routeIdentity() {
			return normalizedURLIdentity(location.current);
		},
		get preparedAccess() {
			return preparedRoute?.kind === "upload" ? preparedRoute.access : undefined;
		},
	});
	registerAdminScrollPage({ ready: () => !controller.accessLoading });
	const navigationBlocker = useBlocker(({ currentLocation, nextLocation }) => {
		if (!controller.dirty) return false;
		const nextLocale = runtime.resolveContentLocale(
			new URLSearchParams(nextLocation.search).get("locale")
		);
		return currentLocation.pathname !== nextLocation.pathname || nextLocale !== contentLocale;
	});

	$effect(() => runtime.registerContentLocaleBlocker(() => controller.dirty));

	$effect(() => {
		if (!controller.dirty) return;
		const preventUnload = (event: BeforeUnloadEvent) => {
			event.preventDefault();
			event.returnValue = "";
		};
		window.addEventListener("beforeunload", preventUnload);
		return () => window.removeEventListener("beforeunload", preventUnload);
	});

	$effect(() => {
		if (!controller.dirty && navigationBlocker.state === "blocked") navigationBlocker.reset();
	});

	function cancelNavigation() {
		if (navigationBlocker.state === "blocked") navigationBlocker.reset();
	}

	function discardAndNavigate() {
		if (controller.running || controller.remotePending || navigationBlocker.state !== "blocked")
			return;
		controller.discard();
		navigationBlocker.proceed();
	}
</script>

<DialogPrimitive.Root
	open
	onOpenChange={(open) => {
		if (!open) navigate(closePath);
	}}
>
	<DialogPrimitive.Portal>
		<DialogPrimitive.Overlay class="ridu-bulk-upload-overlay" />
		<DialogPrimitive.Content
			class="ridu-bulk-upload"
			dir={runtime.i18n.direction}
			aria-busy={controller.running || controller.remotePending}
		>
			<DialogPrimitive.Description class="ridu-bulk-upload-description">
				{runtime.i18n.t("uploads:bulkUploadDescription")}
			</DialogPrimitive.Description>
			{#if controller.queue.length === 0}
				<header class="ridu-bulk-upload-header">
					<DialogPrimitive.Title
						level={1}
						id="bulk-upload-title"
						class="ridu-bulk-upload-header__title"
					>
						{runtime.i18n.t("uploads:addFiles")}
					</DialogPrimitive.Title>
					<Link
						class={buttonVariants({ variant: "ghost", size: "icon-sm" })}
						to={closePath}
						aria-label={runtime.i18n.t("general:close")}
					>
						<XIcon />
					</Link>
				</header>

				<div class="ridu-bulk-upload-empty">
					{const accessNotice = $derived(
						collection !== undefined && collection.capabilities.upload !== true
							? {
									message: runtime.i18n.t("uploads:notConfigured"),
									tone: "destructive" as const,
								}
							: controller.accessError !== undefined
								? { message: controller.accessError, tone: "destructive" as const }
								: !controller.accessLoading && !controller.canCreate
									? {
											message: runtime.i18n.t("uploads:createPermissionDenied"),
											tone: "warning" as const,
										}
									: undefined
					)}
					{#if accessNotice}
						<Banner tone={accessNotice.tone}>{accessNotice.message}</Banner>
					{/if}
					<BulkUploadFilePicker {controller} />
					<BulkUploadRemote {controller} {contentDirection} />
				</div>
			{:else}
				<div class="ridu-bulk-upload-workspace">
					<div class="ridu-bulk-upload-workspace__sidebar">
						<BulkUploadSidebar {controller} />
						<BulkUploadRemote {controller} {contentDirection} />
					</div>
					<BulkUploadEditor {controller} {closePath} {contentDirection} />
				</div>
			{/if}
		</DialogPrimitive.Content>
	</DialogPrimitive.Portal>
</DialogPrimitive.Root>

<BulkUploadEditAll {controller} {contentDirection} />

<AdminDialog
	open={navigationBlocker.state === "blocked"}
	onOpenChange={(open) => {
		if (!open) cancelNavigation();
	}}
>
	<DialogContent variant="confirmation">
		<DialogHeader>
			<DialogTitle>{runtime.i18n.t("uploads:discardQueueTitle")}</DialogTitle>
			<DialogDescription>{runtime.i18n.t("uploads:discardQueueDescription")}</DialogDescription>
		</DialogHeader>
		<DialogFooter>
			<Button variant="outline" onclick={cancelNavigation}>
				{runtime.i18n.t("uploads:keepUploading")}
			</Button>
			<Button
				disabled={controller.running || controller.remotePending}
				onclick={discardAndNavigate}
			>
				{runtime.i18n.t("uploads:discardQueue")}
			</Button>
		</DialogFooter>
	</DialogContent>
</AdminDialog>
