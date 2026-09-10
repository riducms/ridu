import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import SupportProvider from './components/support-provider.svelte';
import ContextSupportLink from './components/context-support-link.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	providers: [{ key: 'support', component: SupportProvider }],
	navigation: [
		{
			key: 'support-link',
			component: ContextSupportLink,
			position: 'afterLinks'
		}
	]
});
