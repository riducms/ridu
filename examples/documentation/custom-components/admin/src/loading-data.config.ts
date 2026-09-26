import { defineAdmin } from '@riducms/plugin/admin';
import { withAdminLoader } from '@riducms/plugin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import { adminLoaders } from '../../generated/ridu.generated';
import PostSummary from './components/post-summary.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	routes: [
		{
			path: 'post-report',
			navigation: { label: 'Post report' },
			...withAdminLoader(adminLoaders['post-summary'], PostSummary)
		}
	]
});
