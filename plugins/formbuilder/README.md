# Form Builder plugin

`plugins/formbuilder` adds `forms` and `form-submissions` collections and validates every submission
against its selected form. Pair it with `@riducms/plugin-form-builder` for the admin UI and frontend
helpers.

The default fields are checkbox, country, email, message, number, select, state, text, and textarea.
Date, radio, upload, and payment fields are opt-in.

```go
formbuilder.New(formbuilder.Config{
	EnabledFields: []formbuilder.FieldType{
		formbuilder.FieldText,
		formbuilder.FieldEmail,
		formbuilder.FieldTextarea,
		formbuilder.FieldUpload,
	},
	UploadCollections:     []schema.CollectionSlug{"media"},
	RedirectRelationships: []schema.CollectionSlug{"pages"},
	DefaultToEmail:        "forms@example.com",
	SendEmail: func(ctx context.Context, email formbuilder.Email) error {
		return mailer.Send(ctx, email)
	},
})
```

Form definitions are public by default so application frontends can render them. Their `emails`
field is visible only to the configured admin-user collection. Form mutations and submission reads
and deletes require that admin identity; submissions allow anonymous create and reject updates.
Collection overrides may replace those defaults, while the plugin keeps the collection identity and
the validation hooks required by the pair.

Email delivery runs after commit. `{{field}}`, `{{*}}`, `{{*:table}}`, and
`{{formSubmissionID}}` placeholders are supported; submission-derived HTML is escaped. The
application supplies the mail provider and credentials through `SendEmail`. Delivery failures are
reported through `ReportError` or structured logging and do not roll back an already committed
submission.

Payment work runs before commit. Configure `PaymentProcessors` and `HandlePayment`; exactly one
payment block may appear in a form. An omitted optional payment does no provider work. The callback
receives the server-calculated finite total and returns the JSON value stored with the submission.
An error aborts the write, but provider side effects must still be idempotent because they are not
part of the database transaction. Upload submissions use string IDs for a single configured target
or Ridu's `{relationTo, id}` shape for polymorphic targets. They are checked against the configured
collection, existence and read access, MIME allowlist, file-size limit, and singular/multiple
setting; missing protected metadata fails closed.

`Fields` can also append custom block types. The plugin preserves their stored submission values and
enforces identity and required presence; provide their renderer and any additional validation.

See the [Form Builder guide](../../website/src/content/docs/form-builder.md) for setup, field
overrides, validation, email, payments, migrations, and verification.
