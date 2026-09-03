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
	class={[
		"inline-block shrink-0 bg-current [mask:var(--ridu-logo)_center/contain_no-repeat] [-webkit-mask:var(--ridu-logo)_center/contain_no-repeat]",
		variant === "arch" ? "aspect-[82/70]" : "aspect-[234/70]",
		className,
	]}
	style={`--ridu-logo: url("${source}")`}
	aria-hidden="true"
></span>
