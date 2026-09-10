<script lang="ts">
	import type { AdminBrandComponentProps } from '@riducms/plugin';

	let { surface }: AdminBrandComponentProps = $props();
</script>

<!-- Reuse the logo at a larger size on the sign-in screen. -->
<span class="font-semibold" class:text-xl={surface === 'loginLogo'}>
	Acme Studio
</span>
