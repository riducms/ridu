import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import DocumentSummary from './components/document-summary.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	documentViews: [
		{
			key: 'summary',
			label: 'Summary',
			collection: 'posts',
			component: DocumentSummary
		}
	]
});
