import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import PostCards from './components/post-cards.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	listResultsRenderers: [
		{ key: 'post-cards', collection: 'posts', component: PostCards }
	]
});
