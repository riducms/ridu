import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import CollectionOverview from './components/collection-overview.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	dashboard: [
		{
			key: 'collections',
			component: CollectionOverview,
			position: 'before'
		}
	]
});
