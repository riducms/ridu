/// <reference types="bun-types" />

import { expect, test } from 'bun:test';
import { resolve } from 'node:path';
import { format, resolveConfig } from 'prettier';

test('formats Svelte examples with readable control blocks and markup', async () => {
	const filepath = resolve(import.meta.dir, 'content/guides/custom-fields.md');
	const config = await resolveConfig(filepath);
	const options = {
		...config,
		filepath,
		plugins: config?.plugins?.map((plugin) =>
			typeof plugin === 'string' ? import.meta.resolve(plugin) : plugin
		)
	};
	const source = [
		'```svelte title="messages.svelte"',
		'<script lang="ts">let {issues}=$props();</script>',
		'{#each issues as issue (issue.code)}',
		'<p>{issue.message}</p>',
		'{/each}',
		'<span class="font-semibold" class:text-xl={surface === "loginLogo"}>Acme Studio</span>',
		'```',
		''
	].join('\n');
	const expected = [
		'```svelte title="messages.svelte"',
		'<script lang="ts">',
		'\tlet { issues } = $props();',
		'</script>',
		'',
		'{#each issues as issue (issue.code)}',
		'\t<p>{issue.message}</p>',
		'{/each}',
		'<span class="font-semibold" class:text-xl={surface === \'loginLogo\'}>',
		'\tAcme Studio',
		'</span>',
		'```',
		''
	].join('\n');

	expect(await format(source, options)).toBe(expected);
	expect(await format(expected, options)).toBe(expected);
});
