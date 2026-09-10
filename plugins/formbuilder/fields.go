package formbuilder

import (
	"fmt"

	ridu "github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
)

func (plugin *Plugin) formFieldBlocks(localized bool) ([]field.Block, error) {
	definitions := make(map[FieldType]field.Block, 13)
	definitions[FieldText] = field.Block{Slug: string(FieldText), Fields: commonInputFields(localized, field.Text("defaultValue").Localized(localized).Label("Default value"))}
	definitions[FieldTextarea] = field.Block{Slug: string(FieldTextarea), Fields: commonInputFields(localized, field.Textarea("defaultValue").Localized(localized).Label("Default value"))}
	definitions[FieldNumber] = field.Block{Slug: string(FieldNumber), Fields: commonInputFields(localized, field.Number("defaultValue").Label("Default value"))}
	definitions[FieldEmail] = field.Block{Slug: string(FieldEmail), Fields: commonInputFields(localized)}
	definitions[FieldCheckbox] = field.Block{Slug: string(FieldCheckbox), Fields: append(commonInputFields(localized), field.Checkbox("defaultValue").Label("Checked by default"))}
	definitions[FieldDate] = field.Block{Slug: string(FieldDate), Fields: append(commonInputFields(localized), field.Date("defaultValue").Label("Default value").Format(field.DateTime))}
	definitions[FieldState] = field.Block{Slug: string(FieldState), Fields: commonInputFields(localized)}
	definitions[FieldCountry] = field.Block{Slug: string(FieldCountry), Fields: commonInputFields(localized)}
	definitions[FieldMessage] = field.Block{Slug: string(FieldMessage), Fields: field.Fields{
		field.Textarea("message").Localized(localized).Label("Message"),
	}}
	definitions[FieldSelect] = optionBlock(FieldSelect, "Select", localized, true)
	definitions[FieldRadio] = optionBlock(FieldRadio, "Radio", localized, false)
	definitions[FieldUpload] = plugin.uploadBlock(localized)
	definitions[FieldPayment] = plugin.paymentBlock(localized)

	selected := plugin.enabledFields()
	blocks := make([]field.Block, 0, len(selected))
	for _, fieldType := range selected {
		if definition, exists := definitions[fieldType]; exists {
			blocks = append(blocks, definition)
		}
	}
	if plugin.config.Fields == nil {
		return blocks, nil
	}
	overridden, err := plugin.config.Fields(append([]field.Block(nil), blocks...))
	if err != nil {
		return nil, err
	}
	if len(overridden) == 0 {
		return nil, fmt.Errorf("override returned no field blocks")
	}
	return append([]field.Block(nil), overridden...), nil
}

func commonInputFields(localized bool, additional ...field.Node) field.Fields {
	definitions := field.Fields{
		field.Row(field.Fields{
			field.Text("name").Label("Name").Required().Admin(field.Admin{Columns: 6}),
			field.Text("label").Localized(localized).Label("Label").Admin(field.Admin{Columns: 6}),
		}),
		field.Row(field.Fields{
			field.Number("width").Label("Field width (%)").Min(1).Max(100).Admin(field.Admin{Columns: 6}),
			field.Checkbox("required").Label("Required").Admin(field.Admin{Columns: 6}),
		}),
	}
	return append(definitions, additional...)
}

func optionBlock(kind FieldType, label string, localized, placeholder bool) field.Block {
	definitions := commonInputFields(localized, field.Text("defaultValue").Localized(localized).Label("Default value"))
	if placeholder {
		definitions = append(definitions, field.Text("placeholder").Localized(localized).Label("Placeholder"))
	}
	definitions = append(definitions, field.Array("options", field.Fields{
		field.Text("label").Localized(localized).Label("Label").Required().Admin(field.Admin{Columns: 6}),
		field.Text("value").Label("Value").Required().Admin(field.Admin{Columns: 6}),
	}).Label(label+" options").Required().MinRows(1).Admin(field.Admin{RowLabelPath: "label", RowLabels: field.RowLabels{Singular: "Option", Plural: "Options"}}))
	return field.Block{Slug: string(kind), Fields: definitions}
}

func (plugin *Plugin) uploadBlock(localized bool) field.Block {
	options := make([]field.Option, len(plugin.config.UploadCollections))
	for index, slug := range plugin.config.UploadCollections {
		options[index] = field.Option{Value: string(slug), Label: string(slug)}
	}
	definitions := commonInputFields(localized)
	definitions = append(definitions, field.Select("uploadCollection").Options(options...).Label("Upload collection").Required(), field.Array("mimeTypes", field.Fields{
		field.Text("mimeType").Required().Label("MIME type"),
	}).Label("Allowed MIME types").Admin(field.Admin{RowLabels: field.RowLabels{Singular: "MIME type", Plural: "MIME types"}}), field.Row(field.Fields{
		field.Number("maxFileSize").Label("Maximum file size (bytes)").Min(1).Admin(field.Admin{Columns: 6}),
		field.Checkbox("multiple").Label("Allow multiple files").Admin(field.Admin{Columns: 6}),
	}))
	return field.Block{Slug: string(FieldUpload), Fields: definitions}
}

func (plugin *Plugin) paymentBlock(localized bool) field.Block {
	definitions := commonInputFields(localized, field.Number("basePrice").Label("Base price").Required().Min(0), field.Select("paymentProcessor").Options(plugin.config.PaymentProcessors...).Label("Payment processor").Required(), field.Array("priceConditions", field.Fields{
		field.Text("fieldToUse").Label("Field to use").Required(),
		field.Select("condition").Options(
			field.Option{Value: "hasValue", Label: "Has any value"},
			field.Option{Value: "equals", Label: "Equals"},
			field.Option{Value: "notEquals", Label: "Does not equal"}).Label("Condition").Required().Default("hasValue"),
		field.Text("valueForCondition").Label("Comparison value").Admin(field.Admin{Description: "Used by equals and does-not-equal conditions."}),
		field.Select("operator", "add", "subtract", "multiply", "divide").Label("Operator").Required().Default("add"),
		field.Radio("valueType").Options(
			field.Option{Value: "static", Label: "Static value"}, field.Option{Value: "valueOfField", Label: "Value of another field"}).Label("Value type").Required().Default("static"),
		field.Text("valueForOperator").Label("Value").Required(),
	}).Label("Price conditions").Admin(field.Admin{RowLabels: field.RowLabels{Singular: "Price condition", Plural: "Price conditions"}}),
	)
	return field.Block{Slug: string(FieldPayment), Fields: definitions}
}

func (plugin *Plugin) formsCollection(blocks []field.Block, localized bool, adminCollection schema.CollectionSlug) ridu.Collection {
	redirectFields := field.Fields{}
	if len(plugin.config.RedirectRelationships) != 0 {
		redirectFields = append(redirectFields, field.Radio("type").Options(
			field.Option{Value: "custom", Label: "Custom URL"}, field.Option{Value: "reference", Label: "Internal link"}).Label("Redirect type").Default("reference"),
		)
		presentation := field.Admin{VisibleWhen: field.Equal(field.Sibling("type"), "reference")}
		if len(plugin.config.RedirectRelationships) == 1 {
			redirectFields = append(redirectFields, field.Relationship("reference", plugin.config.RedirectRelationships[0]).Label("Document to link to").Admin(presentation))
		} else {
			redirectFields = append(redirectFields, field.PolymorphicRelationship("reference", plugin.config.RedirectRelationships...).Label("Document to link to").Admin(presentation))
		}
		redirectFields = append(redirectFields, field.Text("url").Label("URL to redirect to").Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("type"), "custom")}))
	} else {
		redirectFields = append(redirectFields, field.Text("url").Label("URL to redirect to"))
	}

	return ridu.Collection{
		Slug:   plugin.config.FormsSlug,
		Labels: ridu.CollectionLabels{Singular: "Form", Plural: "Forms"},
		Admin:  ridu.CollectionAdmin{UseAsTitle: "title", Group: "Form Builder", DefaultColumns: []string{"title"}},
		Fields: field.Fields{
			field.Text("title").Label("Title").Required(),
			field.Blocks("fields", blocks...).Label("Fields"),
			field.Text("submitButtonLabel").Localized(localized).Label("Submit button").Default("Submit"),
			field.Radio("confirmationType", "message", "redirect").Label("Confirmation type").Default("message").Required().Admin(field.Admin{Description: "Choose whether successful submissions show a message or redirect."}),
			field.Textarea("confirmationMessage").Localized(localized).Label("Confirmation message").Admin(field.Admin{VisibleWhen: field.Equal(field.Root("confirmationType"), "message")}),
			field.Group("redirect", redirectFields).Label("Redirect").Admin(field.Admin{VisibleWhen: field.Equal(field.Root("confirmationType"), "redirect")}),
			field.Array("emails", field.Fields{
				field.Text("emailTo").Label("Email to"),
				field.Row(field.Fields{
					field.Text("cc").Label("CC").Admin(field.Admin{Columns: 6}),
					field.Text("bcc").Label("BCC").Admin(field.Admin{Columns: 6}),
				}),
				field.Row(field.Fields{
					field.Text("replyTo").Label("Reply to").Admin(field.Admin{Columns: 6}),
					field.Text("emailFrom").Label("Email from").Admin(field.Admin{Columns: 6}),
				}),
				field.Text("subject").Localized(localized).Label("Subject").Required().Default("You've received a new message."),
				field.Textarea("message").Localized(localized).Label("Message").Admin(field.Admin{Description: "Submission placeholders are HTML-escaped before insertion."}),
			}).Label("Emails").Admin(field.Admin{
				Description: "Send dynamic emails after a submission. Use {{field_name}}, {{*}}, or {{*:table}} in text.",
				RowLabels:   field.RowLabels{Singular: "Email", Plural: "Emails"},
			}).Access(field.Access{Read: func(context operation.AccessContext) (bool, error) {
				return (context.Actor.ID != "" && (adminCollection == "" || context.Actor.Collection == adminCollection)) || context.Context.Value(emailConfigReadKey{}) == true, nil
			}}),
		},
		Access: ridu.CollectionAccess{
			Create: allowAdminCollection(adminCollection), Read: allowAll,
			Update: allowAdminCollection(adminCollection), Delete: allowAdminCollection(adminCollection),
		},
	}
}

func (plugin *Plugin) submissionsCollection(adminCollection schema.CollectionSlug, allowedFieldTypes map[FieldType]struct{}) ridu.Collection {
	fields := field.Fields{
		field.Relationship("form", plugin.config.FormsSlug).Label("Form").Required().OnDelete(field.ReferenceDeleteRestrict),
		field.Array("submissionData", field.Fields{
			field.Text("field").Label("Field").Required(),
			field.JSON("value").Label("Value").Required(),
		}).Label("Submission data").Admin(field.Admin{RowLabels: field.RowLabels{Singular: "Submitted field", Plural: "Submission data"}}),
	}
	if len(plugin.config.UploadCollections) != 0 {
		var references field.Node
		if len(plugin.config.UploadCollections) == 1 {
			references = field.Relationships("value", plugin.config.UploadCollections[0]).Label("Files").Required().OnDelete(field.ReferenceDeleteRestrict)
		} else {
			references = field.PolymorphicRelationships("value", plugin.config.UploadCollections...).Label("Files").Required().OnDelete(field.ReferenceDeleteRestrict)
		}
		fields = append(fields, field.Array("submissionUploads", field.Fields{
			field.Text("field").Label("Field").Required(),
			references,
		}).Label("Submission uploads").Admin(field.Admin{RowLabels: field.RowLabels{Singular: "Submitted upload", Plural: "Submission uploads"}}))
	}
	if _, enabled := allowedFieldTypes[FieldPayment]; enabled {
		fields = append(fields, field.JSON("payment").Label("Payment details").Admin(field.Admin{ReadOnly: true}))
	}
	return ridu.Collection{
		Slug:   plugin.config.SubmissionsSlug,
		Labels: ridu.CollectionLabels{Singular: "Form Submission", Plural: "Form Submissions"},
		Admin:  ridu.CollectionAdmin{Group: "Form Builder", DefaultColumns: []string{"form"}},
		Fields: fields,
		Access: ridu.CollectionAccess{Create: allowAll, Read: allowAdminCollection(adminCollection), Update: denyAll, Delete: allowAdminCollection(adminCollection)},
	}
}

func (plugin *Plugin) secureFormsCollection(collection ridu.Collection, allowedFieldTypes map[FieldType]struct{}) ridu.Collection {
	collection.Slug = plugin.config.FormsSlug
	validate := func(context ridu.HookContext) error {
		return plugin.validateFormDefinition(context, allowedFieldTypes)
	}
	collection.Hooks.BeforeValidate = append(collection.Hooks.BeforeValidate, validate)
	collection.Hooks.BeforeChange = append(collection.Hooks.BeforeChange, validate)
	collection.Hooks.BeforeOperation = append(collection.Hooks.BeforeOperation, validate)
	return collection
}

func (plugin *Plugin) secureSubmissionsCollection(collection ridu.Collection, allowedFieldTypes map[FieldType]struct{}) ridu.Collection {
	collection.Slug = plugin.config.SubmissionsSlug
	validate := func(context ridu.HookContext) error {
		return plugin.validateSubmission(context, allowedFieldTypes)
	}
	collection.Hooks.BeforeValidate = append(collection.Hooks.BeforeValidate, validate)
	collection.Hooks.BeforeChange = append(collection.Hooks.BeforeChange, validate, func(context ridu.HookContext) error {
		return plugin.processPayment(context, allowedFieldTypes)
	})
	collection.Hooks.BeforeOperation = append(collection.Hooks.BeforeOperation, validate)
	collection.Hooks.AfterCommit = append(collection.Hooks.AfterCommit, plugin.sendSubmissionEmails)
	return collection
}
