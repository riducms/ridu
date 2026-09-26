<script lang="ts">
	import { Navigate, Route, Routes, useLocation } from "@hvniel/svelte-router";

	import {
		adminCreateFirstUserPath,
		adminLoginPath,
		adminRedirectFromSearch,
		adminRoutePatterns,
	} from "@admin/core/routing/admin-paths";
	import { getAdminRuntime } from "@admin/core/runtime/admin-runtime.svelte";
	import CollectionListRoute from "@admin/features/collections/collection-list-route.svelte";
	import BulkUploadRoute from "@admin/features/uploads/bulk-upload-route-loader.svelte";
	import LoginRoute from "@admin/features/auth/login-route.svelte";
	import CreateFirstUserRoute from "@admin/features/auth/create-first-user-route.svelte";
	import ForgotPasswordRoute from "@admin/features/auth/forgot-password-route.svelte";
	import ResetPasswordRoute from "@admin/features/auth/reset-password-route.svelte";
	import RequestVerificationRoute from "@admin/features/auth/request-verification-route.svelte";
	import VerifyEmailRoute from "@admin/features/auth/verify-email-route.svelte";
	import AccountSecurityRoute from "@admin/features/account/account-security-route.svelte";
	import AccountRoute from "@admin/features/account/account-route.svelte";
	import DashboardRoute from "@admin/features/dashboard/dashboard-route.svelte";
	import DocumentRoute from "@admin/features/documents/document-route-loader.svelte";
	import VersionHistoryRoute from "@admin/features/versions/version-history-route-loader.svelte";

	import AdminLayout from "@admin/app/admin-layout.svelte";
	import AdminCoreViewRoute from "@admin/app/admin-core-view-route.svelte";
	import AdminExtensionRoute from "@admin/app/admin-extension-route.svelte";
	import NotFoundRoute from "@admin/app/not-found-route.svelte";

	const runtime = getAdminRuntime();
	const location = useLocation();
	const requestedPath = $derived(
		`${location.current.pathname}${location.current.search}${location.current.hash}`
	);
	const postLoginPath = $derived(adminRedirectFromSearch(location.current.search));
</script>

{#if runtime.authenticated}
	<Routes>
		<Route path="login">
			{#snippet element()}
				<Navigate to={postLoginPath} replace />
			{/snippet}
		</Route>
		<Route path="create-first-user">
			{#snippet element()}
				<Navigate to={adminRoutePatterns.home} replace />
			{/snippet}
		</Route>
		<Route Component={AdminLayout}>
			<Route path="account" Component={AccountRoute} />
			<Route path="account/security" Component={AccountSecurityRoute} />
			<Route index Component={DashboardRoute} />
			<Route path="collections/:collection">
				{#snippet element()}
					<AdminCoreViewRoute surface="collectionList" DefaultView={CollectionListRoute} />
				{/snippet}
			</Route>
			<Route path="collections/:collection/trash" Component={CollectionListRoute} />
			<Route path="collections/:collection/upload" Component={BulkUploadRoute} />
			<Route path="collections/:collection/create">
				{#snippet element()}
					<AdminCoreViewRoute surface="collectionCreate" DefaultView={DocumentRoute} />
				{/snippet}
			</Route>
			<Route path="collections/:collection/create/api">
				{#snippet element()}
					<AdminCoreViewRoute surface="collectionCreate" DefaultView={DocumentRoute} />
				{/snippet}
			</Route>
			<Route
				path="collections/:collection/:document/versions/:revision?"
				Component={VersionHistoryRoute}
			/>
			<Route path="collections/:collection/:document">
				{#snippet element()}
					<AdminCoreViewRoute surface="collectionEdit" DefaultView={DocumentRoute} />
				{/snippet}
			</Route>
			<Route path="collections/:collection/:document/api">
				{#snippet element()}
					<AdminCoreViewRoute surface="collectionEdit" DefaultView={DocumentRoute} />
				{/snippet}
			</Route>
			{#each runtime.config.extensions.documentViews as view (`${view.collection ?? "*"}:${view.key}`)}
				<Route
					path={view.collection === undefined
						? `collections/:collection/:document/${view.key}`
						: `collections/${view.collection}/:document/${view.key}`}
					Component={DocumentRoute}
				/>
			{/each}
			<Route path="globals/:global/versions/:revision?" Component={VersionHistoryRoute} />
			<Route path="globals/:global">
				{#snippet element()}
					<AdminCoreViewRoute surface="global" DefaultView={DocumentRoute} />
				{/snippet}
			</Route>
			<Route path="globals/:global/api">
				{#snippet element()}
					<AdminCoreViewRoute surface="global" DefaultView={DocumentRoute} />
				{/snippet}
			</Route>
			{#each runtime.config.extensions.documentViews as view (`${view.collection ?? "*"}:${view.key}`)}
				<Route
					path={view.collection === undefined
						? `globals/:global/${view.key}`
						: `globals/${view.collection}/${view.key}`}
					Component={DocumentRoute}
				/>
			{/each}
			{#each runtime.config.extensions.routes as pluginRoute (pluginRoute.path)}
				<Route path={pluginRoute.path}>
					{#snippet element()}
						<AdminExtensionRoute route={pluginRoute} />
					{/snippet}
				</Route>
			{/each}
			<Route path="*">
				{#snippet element()}
					<AdminCoreViewRoute surface="notFound" DefaultView={NotFoundRoute} />
				{/snippet}
			</Route>
		</Route>
	</Routes>
{:else if runtime.authBootstrapAvailable}
	<Routes>
		<Route path="create-first-user" Component={CreateFirstUserRoute} />
		<Route path="*">
			{#snippet element()}
				<Navigate to={adminCreateFirstUserPath()} replace />
			{/snippet}
		</Route>
	</Routes>
{:else}
	<Routes>
		<Route path="create-first-user">
			{#snippet element()}
				<Navigate to={adminRoutePatterns.login} replace />
			{/snippet}
		</Route>
		<Route path="login" Component={LoginRoute} />
		<Route path="forgot-password" Component={ForgotPasswordRoute} />
		<Route path="reset-password" Component={ResetPasswordRoute} />
		<Route path="request-verification" Component={RequestVerificationRoute} />
		<Route path="verify-email" Component={VerifyEmailRoute} />
		<Route path="*">
			{#snippet element()}
				<Navigate to={adminLoginPath(requestedPath)} replace />
			{/snippet}
		</Route>
	</Routes>
{/if}
