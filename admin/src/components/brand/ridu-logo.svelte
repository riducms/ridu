<script lang="ts">
	import archLogo from "@admin/assets/ridu-logo-arch.svg";
	import wordLogo from "@admin/assets/ridu-word-logo.svg";

	let {
		variant = "word",
		class: className,
	}: {
		variant?: "arch" | "word";
		class?: string;
	} = $props();

	const source = $derived(variant === "arch" ? archLogo : wordLogo);
</script>

<span
	class={["ridu-logo", variant === "arch" ? "ridu-logo--arch" : "ridu-logo--word", className]}
	style={`--ridu-logo: url("${source}")`}
	aria-hidden="true"
></span>

<style lang="scss">
	@use "@riducms/ui/layers.css";
	@layer ridu.components {
		.ridu-logo {
			display: inline-block;
			flex-shrink: 0;
			background: currentColor;
			mask: var(--ridu-logo) center / contain no-repeat;
		}
		.ridu-logo--arch {
			aspect-ratio: 82 / 70;
		}
		.ridu-logo--word {
			aspect-ratio: 234 / 70;
		}
	}
</style>
