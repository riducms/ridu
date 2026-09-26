import { defineAdmin } from '@riducms/plugin/admin';
import { withAdminLoader } from '@riducms/plugin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import { adminLoaders } from '../../generated/ridu.generated';
import PostSummary from './components/post-summary.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	dashboardPanels: [
		{
			key: 'post-summary',
			position: 'before',
			...withAdminLoader(adminLoaders['post-summary'], PostSummary)
		}
	]
});
