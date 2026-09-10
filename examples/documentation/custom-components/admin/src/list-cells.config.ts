import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import ReadingTimeCell from './components/reading-time-cell.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	listCells: [
		{
			key: 'reading-time',
			collection: 'posts',
			field: 'readingMinutes',
			label: 'Reading time',
			component: ReadingTimeCell
		}
	]
});
