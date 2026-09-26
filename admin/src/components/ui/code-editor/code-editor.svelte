<script lang="ts">
	import { getAdminI18n } from "@riducms/plugin";
	import type {
		CodeEditor,
		CodeEditorConfiguration,
	} from "@admin/components/ui/code-editor/code-editor";
	import "@admin/components/ui/code-editor/code-editor.scss";

	let {
		value,
		language,
		configuration,
		onChange,
	}: {
		value: string;
		language: string;
		configuration: CodeEditorConfiguration;
		onChange: (value: string) => void;
	} = $props();

	const i18n = getAdminI18n();
	let editor = $state.raw<CodeEditor>();
	let loadFailed = $state(false);

	function mount(element: HTMLElement) {
		let disposed = false;
		let instance: CodeEditor | undefined;

		import("@admin/components/ui/code-editor/code-editor")
			.then(({ CodeEditor }) => {
				if (disposed) return;
				instance = new CodeEditor(element, value, configuration, (next) => onChange(next));
				editor = instance;
			})
			.catch(() => {
				if (!disposed) loadFailed = true;
			});

		return () => {
			disposed = true;
			instance?.destroy();
		};
	}

	$effect(() => {
		editor?.configure(configuration);
	});
	$effect(() => {
		const current = editor;
		const next = value;
		current?.setValue(next);
	});
	$effect(() => {
		editor?.setLanguage(language).catch(() => {
			loadFailed = true;
		});
	});
</script>

<div
	class="ridu-code-editor"
	data-language={language}
	data-invalid={configuration.invalid}
	data-readonly={configuration.readOnly}
	aria-busy={!editor && !loadFailed}
>
	<div {@attach mount}></div>
	{#if loadFailed}
		<p role="alert">{i18n.t("fields:codeEditorLoadFailed")}</p>
	{/if}
</div>
