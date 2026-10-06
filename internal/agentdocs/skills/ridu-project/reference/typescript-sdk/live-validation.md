<!-- Generated from website/src/content/docs/typescript-sdk/live-validation.md by scripts/sync-agent-docs.ts. -->

# Live checks

A field with a `.LiveValidate(...)` rule in Go can be checked before anything is saved. The admin
does this for you; a custom form calls `collectionLiveValidation` or `globalLiveValidation`. Set up
the rule with [Live server validation](../fields/live-validation.md).

The examples use that guide's `products` collection, whose sale price must be lower than its
regular price.

## Check a field {#check}

Send the unsaved values the rule needs, and the fields to check:

```ts title="scripts/check-sale-price.ts"
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
```

This returns an issue on `salePrice`, because 120 isn't lower than 100. With `salePrice: 80`, it
returns none.

| Send     | What                                                                                      |
| -------- | ----------------------------------------------------------------------------------------- |
| `data`   | the form's unsaved values, including any other field the rule reads; it can be incomplete |
| `fields` | the paths to check, such as `salePrice`, `seo.title` or `variants.0.salePrice`            |
| `id`     | the document's ID when editing a saved one; leave it out for a new document               |

For a global, call `globalLiveValidation('site-settings', { data, fields })` without an `id`. A
second argument takes one `locale` and a `signal`.

## Read the result {#results}

Each evaluation has a `path`, a `status` and `issues`:

| Result                 | Show                                                                                             |
| ---------------------- | ------------------------------------------------------------------------------------------------ |
| `checked`, with issues | the issues, beside the field                                                                     |
| `checked`, no issues   | nothing. Saving can still fail on another rule.                                                  |
| `skipped`              | nothing, and not a success: the check didn't run, for example because a value had the wrong type |

Keep an evaluation's `target` when one is present and match feedback to fields with it; it's an
opaque token, not a path to parse. A live check never saves and never runs `.Validate(...)`, so
handle validation errors from `create` or `update` as well.

## Drop outdated answers {#cancel}

Someone typing can trigger checks faster than they return. Cancel the previous check when the
input changes, and ignore its answer:

```ts title="live-check.ts"
let controller: AbortController | undefined;

async function checkSalePrice(price: number, salePrice: number) {
	controller?.abort();
	const current = (controller = new AbortController());
	clearIssues('salePrice');
	try {
		const { evaluations } = await ridu.collectionLiveValidation(
			'products',
			{ data: { price, salePrice }, fields: ['salePrice'] },
			{ signal: current.signal }
		);
		if (current.signal.aborted) return;
		for (const evaluation of evaluations) {
			for (const issue of evaluation.issues)
				showIssue(issue.path, issue.message);
		}
	} catch (error) {
		// A cancelled check rejects with the browser's abort error.
		if (!current.signal.aborted) throw error;
	}
}
```

Choose when to check, such as on blur or after a short pause in typing. Cancel too when the form
closes or switches document or locale.
