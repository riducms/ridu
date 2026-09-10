import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import WelcomePanel from './components/welcome-panel.svelte';

export default defineAdmin({
	// Include admin components from the project's installed plugins.
	plugins: generatedAdminPlugins,
	dashboard: [
		{ key: 'welcome', component: WelcomePanel, position: 'before' }
	]
});
