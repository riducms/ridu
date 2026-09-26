<script lang="ts">
	import type { AdminBrandComponentProps } from '@riducms/plugin';

	let { surface }: AdminBrandComponentProps = $props();
</script>

<!-- Reuse the logo at a larger size on the sign-in screen. -->
<span
	class={[
		'brand-logo',
		{ 'brand-logo--login': surface === 'loginLogo' }
	]}
>
	Acme Studio
</span>

<style>
	.brand-logo {
		font-weight: 600;
	}

	.brand-logo--login {
		font-size: 1.25rem;
	}
</style>
