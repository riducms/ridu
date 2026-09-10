import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import CopyDocumentID from './components/copy-document-id.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	documentActions: [
		{ key: 'copy-id', collection: 'posts', component: CopyDocumentID }
	]
});
