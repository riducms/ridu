import { createClient } from '~/generated/ridu.generated';

const ridu = createClient({ baseURL: 'http://localhost:8080' });

const { evaluations } = await ridu.collectionLiveValidation(
	'products',
	{
		// Send the regular price too: the rule compares these values.
		data: { price: 100, salePrice: 120 },
		fields: ['salePrice']
		// Add id: product.id when editing a saved product.
	}
);

for (const evaluation of evaluations) {
	for (const issue of evaluation.issues) {
		console.info(issue.path, issue.message);
	}
}
