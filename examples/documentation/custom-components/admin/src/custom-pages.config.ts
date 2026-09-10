import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import EditorialHelp from './components/editorial-help.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	routes: [
		{
			path: 'help',
			component: EditorialHelp,
			navigation: { label: 'Editorial help' }
		}
	]
});
