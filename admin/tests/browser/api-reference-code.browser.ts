import { svelte } from "@hvniel/vite-plugin-svelte-inline-component";
import { expect, it } from "vitest";
import { render } from "vitest-browser-svelte";
import type { ReferenceLanguage } from "@admin/features/api-reference/api-reference-examples";

const Example = svelte`
	<script>
		import APIReferenceCode from "../../src/features/api-reference/api-reference-code.svelte";
		let { code, language } = $props();
	</script>
	<section aria-label="Example"><APIReferenceCode {code} {language} /></section>
`;

it("highlights each language using the website palette and font without changing source text", async () => {
	const examples: { language: ReferenceLanguage; code: string }[] = [
		{
			language: "typescript",
			code: 'function greet(name: string) {\n  return "<img src=x onerror=alert(1)>";\n}\n',
		},
		{
			language: "curl",
			code: "curl --request POST 'https://example.test/api/collections/posts' \\\n  --data-raw '{\"title\":\"Example\"}'",
		},
		{
			language: "go",
			code: 'package example\n\nfunc Example(name string) string {\n\treturn "hello"\n}\n',
		},
	];
	const screen = await render(Example, examples[0]);
	try {
		for (const example of examples) {
			await screen.rerender(example);
			const pre = screen.getByRole("region", { name: "Example" }).element().querySelector("pre")!;
			expect(pre.textContent).toBe(example.code);
			expect(pre.querySelector("img, script")).toBeNull();
			expect(getComputedStyle(pre).fontFamily).toContain("Martian Mono Variable");
			expect(getComputedStyle(pre).fontSize).toBe("14px");
			const colors = [...pre.querySelectorAll("span")].map((span) => getComputedStyle(span).color);
			expect(colors).toContain("rgb(181, 228, 140)");
			expect(new Set(colors).size).toBeGreaterThan(2);
			if (example.language !== "curl") expect(colors).toContain("rgb(255, 126, 182)");
		}
	} finally {
		await screen.unmount();
	}
});
