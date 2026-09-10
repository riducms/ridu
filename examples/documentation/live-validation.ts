import { createClient } from '~/generated/ridu.generated';

const ridu = createClient({
	baseURL: 'http://localhost:8080'
});
const controller = new AbortController();
const pending = ridu.collectionLiveValidation(
	'products',
	{
		// Send the regular price too: the rule compares these values.
		data: { price: 100, salePrice: 120 },
		fields: ['salePrice']
		// Add id: product.id when checking an existing document.
	},
	{ signal: controller.signal }
);

// Call controller.abort() when input changes or the editor closes.
try {
	const { evaluations } = await pending;
	// A superseded response must not put old messages back in the UI.
	if (!controller.signal.aborted) {
		for (const evaluation of evaluations) {
			if (evaluation.status === 'skipped') {
				console.info(evaluation.path, 'Not checked');
				continue;
			}
			// "checked" means the callback ran; it may have found issues.
			for (const issue of evaluation.issues) {
				console.info(issue.path, issue.message);
			}
		}
	}
} catch (error) {
	// Fetch cancellation is expected when a newer edit replaces this check.
	if (!controller.signal.aborted) throw error;
}
