import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import { defineRowLabel } from '@riducms/plugin/admin';
import LinkRowLabel from './components/link-row-label.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	rowLabels: {
		'app:linkSummary': defineRowLabel({ component: LinkRowLabel })
	}
});
