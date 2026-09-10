import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import { defineFieldEditor } from '@riducms/plugin/editor';
import TitleField from './components/title-field.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	fields: {
		// Use this name in the Go field's Editor option.
		'app:titleCounter': defineFieldEditor({
			type: 'text',
			component: TitleField
		})
	}
});
