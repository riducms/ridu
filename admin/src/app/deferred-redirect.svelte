<script lang="ts">
	import { useNavigate } from "@hvniel/svelte-router";

	interface Props {
		to: string;
		replace?: boolean;
	}

	let { to, replace = false }: Props = $props();
	const navigate = useNavigate();

	$effect(() => {
		let active = true;
		// The router activates useNavigate in a parent effect. Deferring one
		// microtask prevents a mounted redirect from racing that activation.
		queueMicrotask(() => {
			if (active) navigate(to, { replace });
		});
		return () => {
			active = false;
		};
	});
</script>
