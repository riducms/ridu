<!-- Generated from website/src/content/docs/form-builder.md by scripts/sync-agent-docs.ts. -->

# Form Builder

The Form Builder plugin lets editors assemble reusable forms from blocks in the Ridu admin.
Frontends read those definitions, render them in their own design system, and submit values to a
generated `form-submissions` collection.

Forms and submissions use normal collection access, validation, hooks, REST routes, generated types,
and SDK methods.

## Use it in a new project {#new-project}

Generated projects include `@riducms/plugin-form-builder` in the root and admin workspaces. Add
`formbuilder.New` to your Go config, choose the admin-user collection and any email, upload,
redirect, or payment options, then follow the configuration below.

## Add it to an existing project {#existing-project}

Install the package in both workspaces:

```bash title="terminal" package-manager="bun"
bun add @riducms/plugin-form-builder
bun add --cwd admin @riducms/plugin-form-builder
go mod tidy
```

```bash title="terminal" package-manager="npm"
npm install @riducms/plugin-form-builder
npm install --workspace admin @riducms/plugin-form-builder
go mod tidy
```

```bash title="terminal" package-manager="pnpm"
pnpm add --workspace-root @riducms/plugin-form-builder
pnpm --dir admin add @riducms/plugin-form-builder
go mod tidy
```

```bash title="terminal" package-manager="yarn"
yarn add --ignore-workspace-root-check @riducms/plugin-form-builder
yarn --cwd admin add @riducms/plugin-form-builder
go mod tidy
```

Add `formbuilder.New` below. Review the default public submission access before migrating; do not
expose form creation or submission reads accidentally.

## Configure the paired plugin {#configure}

Configure the Go plugin:

```go title="content/config.go"
package content

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/formbuilder"
	"github.com/riducms/ridu/schema"
)

func Config() ridu.Config {
	return ridu.Config{
		Name: "Acme Forms",
		Plugins: []ridu.Plugin{
			formbuilder.New(formbuilder.Config{
				EnabledFields: []formbuilder.FieldType{
					formbuilder.FieldText,
					formbuilder.FieldEmail,
					formbuilder.FieldSelect,
					formbuilder.FieldTextarea,
					formbuilder.FieldUpload,
				},
				UploadCollections:     []schema.CollectionSlug{"media"},
				RedirectRelationships: []schema.CollectionSlug{"pages"},
				DefaultToEmail:        "forms@example.com",
				SendEmail: func(
					ctx context.Context,
					email formbuilder.Email,
				) error {
					return mailer.Send(ctx, email)
				},
			}),
		},
	}
}
```

```sh title="terminal"
npm install
npm run dev
```

Create a form in the admin, read it through the generated SDK, and submit one valid and one invalid
payload from the application.

Before deployment, create and verify the immutable migration:

```sh title="terminal"
ridu migrate create --name add-form-builder
ridu migrate verify
ridu check
```

Apply the reviewed artifact with `migrate up` during deployment, then require a clean
`migrate status`. Use the selected adapter's URL/path and safety flags for those commands. Confirm
the stored submission and any configured after-commit email or task behavior.

## Collections and access {#collections}

The plugin creates two collections by default:

| Collection         | Purpose                                                                                     | Default access                                                                                                                    |
| ------------------ | ------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| `forms`            | Reusable field definitions, button text, confirmation behavior, and notification templates. | Public read; configured admin-user create, update, and delete. `emails` is visible only to that configured admin-user collection. |
| `form-submissions` | The selected form, scalar values, optional upload references, and optional payment result.  | Public create; configured admin-user read and delete; update denied.                                                              |

`FormsSlug` and `SubmissionsSlug` rename these collections. `Forms` and `Submissions` receive copies
of the defaults and must return complete replacements. Overrides can replace the default access
rules, while the plugin keeps the required collection identities and lifecycle validation hooks.

Form deletion is restricted while submissions reference it. Submitted uploads use restrict-on-delete
references so a retained submission cannot silently lose its files.

## Editor field blocks {#fields}

The default field set matches Payload's default Form Builder selection:

- checkbox, country, email, message, number, select, state, text, and textarea.

Set `EnabledFields` to opt into an exact ordered selection. Date, radio, upload, and payment are
available but are not default-enabled. Upload requires at least one upload-enabled collection in
`UploadCollections`. Payment requires at least one `PaymentProcessors` choice and a compiled
`HandlePayment` callback. A form may contain at most one payment block.

Every input block carries a stable `name`, optional label, required flag, and width. Choice fields
carry their exact allowed options. Upload fields choose one enabled target plus optional MIME,
maximum-size, and multiple-file constraints. Payment fields carry a base price, processor, and
ordered conditional arithmetic.

`Fields` receives copies of the default `field.Block` definitions and replaces the complete ordered
set. It may add custom block types; provide rendering and client validation for those blocks. Ridu
still enforces field identity and required presence. Malformed fields, duplicate names,
invalid targets, bad widths, duplicate choice values, and payment references to unknown fields fail
before or during the form write with field-addressable validation issues.

## Render and submit from an application {#render-submit}

Import the helpers from `@riducms/plugin-form-builder`, render the discriminated `FormField` union
with your own components, then use the generated SDK collection method:

```ts title="src/lib/submit-form.ts"
import {
	buildSubmissionInput,
	confirmationFor,
	validateFormValues,
	type FormDefinition,
	type FormValues
} from '@riducms/plugin-form-builder';
import { client } from './ridu';

export async function submitForm(
	form: FormDefinition,
	values: FormValues
) {
	const issues = validateFormValues(form, values);
	if (issues.length) return { issues };

	const submission = await client.create(
		'form-submissions',
		buildSubmissionInput(form, values)
	);
	return { submission, confirmation: confirmationFor(form) };
}
```

Client validation is only immediate author feedback. The server reloads the selected form inside the
submission transaction and rejects missing required fields, unknown or duplicate names, invalid
email/date/number/checkbox values, unavailable choice values, and invalid upload references with a
`422 validation` error and exact issue paths. Submitted field names are data, never trusted schema
instructions.

## Confirmations and redirects {#confirmation}

Editors choose either a localized confirmation message or a redirect. Redirects may use a custom
URL or a polymorphic relationship limited by `RedirectRelationships`. The TypeScript
`confirmationFor` helper normalizes the saved definition after a successful create; your frontend
handles navigation.

## Templated email after commit {#email}

Each form may define multiple messages with To, CC, BCC, From, Reply-To, subject, and trusted
application-authored HTML. Anonymous form reads cannot see this `emails` field. Templates support:

| Placeholder            | Output                                   |
| ---------------------- | ---------------------------------------- |
| `{{fieldName}}`        | One submitted scalar value.              |
| `{{*}}`                | Every submitted scalar as text.          |
| `{{*:table}}`          | Every submitted scalar in an HTML table. |
| `{{formSubmissionID}}` | The committed submission ID.             |

Submission-derived HTML is escaped, addresses are parsed before delivery, and subjects containing
line breaks are rejected. `DefaultToEmail` supplies a recipient when a row omits one.
`BeforeEmail` may transform or filter the batch; `SendEmail` connects your email provider. Keep
provider credentials in server-side Go code.

Email work runs only after the submission commits. Failures call `ReportError` or structured logging
and do not turn a durable successful submission into an HTTP failure. Use a durable task inside
`SendEmail` when delivery needs persistent retries.

## Upload and payment lifecycle {#uploads-payments}

An upload row stores references separately from scalar `submissionData`. A field targeting one
collection uses document IDs such as `['media_1']`; a polymorphic upload block uses
`{ relationTo, id }` references. Before commit, the server verifies that each target
collection is enabled, each document exists and is readable to the submitter, its detected MIME type
matches the form allowlist, its size is within the configured maximum, and a singular field does not
receive multiple files. Missing or redacted MIME/size metadata fails closed. Upload bytes themselves
must first pass through Ridu's upload endpoint, so the target upload collection's create
access must admit the public actor or authenticated user who is completing the form.

For payment, configure display choices and one trusted callback:

```go title="content/config.go"
PaymentProcessors: []field.Option{{Value: "stripe", Label: "Card"}},
HandlePayment: func(
	ctx formbuilder.PaymentContext,
) (store.Value, error) {
	charge, err := payments.Charge(ctx.Context.Context, ctx.Total)
	if err != nil {
		return store.Value{}, err
	}
	return store.Object(
		store.Values{"chargeID": store.String(charge.ID)},
	), nil
},
```

`GetPaymentTotal` applies matching conditions in authored order. Static or field-derived operands may
add, subtract, multiply, or divide. Negative or non-finite totals and division by zero are rejected. An optional
payment block does not invoke the callback when omitted. When present, the callback runs before
commit; an error rolls back the submission, while its returned JSON is stored in the read-only
`payment` field. External provider side effects cannot be rolled back with the database transaction:
make the callback idempotent, prefer an authorization or payment-intent step keyed to the request,
and capture irrevocably only after durable application state exists. The matching TypeScript helper
is for display only—the Go calculation and provider callback are authoritative.

## What your application provides {#boundaries}

Provide the public form UI, any rich-message rendering, and the email or payment integrations. Use
Ridu's `{ relationTo, id }` shape when converting polymorphic references from another system.

See the exact exported contracts in the [`formbuilder` Go reference](https://riducms.com/reference/formbuilder/) and
[`@riducms/plugin-form-builder` reference](https://riducms.com/reference/plugin-form-builder/).
