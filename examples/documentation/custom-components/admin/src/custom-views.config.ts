import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import EditorialScreen from './components/editorial-screen.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	views: [
		{
			key: 'posts-editor',
			surface: 'collectionEdit',
			collection: 'posts',
			component: EditorialScreen
		}
	]
});
