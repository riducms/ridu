# `@riducms/plugin-form-builder`

TypeScript types, frontend helpers, and the admin UI for Ridu's Form Builder plugin. Use the helpers
to render and validate form definitions; generated projects import the admin registration.

For an existing project, install the same package version in the root and admin workspaces, register
`formbuilder.New(...)` in Go, then start development:

```sh
npm install @riducms/plugin-form-builder
npm install --workspace admin @riducms/plugin-form-builder
npm run dev
```

Create and test a form locally. Before deployment, create and review the adapter migration with
`npm run ridu -- migrate create --name add-form-builder`.

Application frontends import helpers from the package root:

```ts
import {
	buildSubmissionInput,
	confirmationFor,
	validateFormValues,
	type FormDefinition,
	type FormValues,
} from "@riducms/plugin-form-builder";
```

The generated admin registry imports `formBuilderAdminPlugin` from
`@riducms/plugin-form-builder/admin`; do not register it again. Client validation provides immediate
feedback, but the Go plugin reloads the selected form and validates the submission in the write
transaction.

See the [Form Builder guide](../../website/src/content/docs/form-builder.md) for Go configuration,
access defaults, uploads, payments, email delivery, rendering, migrations, and verification.
