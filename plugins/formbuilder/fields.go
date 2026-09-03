package formbuilder

import (
	"fmt"

	ridu "github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func (plugin *Plugin) formFieldBlocks(localized bool) ([]field.Block, error) {
	definitions := make(map[FieldType]field.Block, 13)
	definitions[FieldText] = field.BlockType(string(FieldText), "Text", commonInputFields(localized, localizedText("defaultValue", localized, field.Label("Default value")))...)
	definitions[FieldTextarea] = field.BlockType(string(FieldTextarea), "Textarea", commonInputFields(localized, localizedTextarea("defaultValue", localized, field.Label("Default value")))...)
	definitions[FieldNumber] = field.BlockType(string(FieldNumber), "Number", commonInputFields(localized, field.Number("defaultValue", field.Label("Default value")))...)
	definitions[FieldEmail] = field.BlockType(string(FieldEmail), "Email", commonInputFields(localized)...)
	definitions[FieldCheckbox] = field.BlockType(string(FieldCheckbox), "Checkbox", append(commonInputFields(localized), field.Checkbox("defaultValue", field.Label("Checked by default")))...)
	definitions[FieldDate] = field.BlockType(string(FieldDate), "Date", append(commonInputFields(localized), field.Date("defaultValue", field.Label("Default value"), field.PickerAppearance(field.DatePickerDayAndTime)))...)
	definitions[FieldState] = field.BlockType(string(FieldState), "State", commonInputFields(localized)...)
	definitions[FieldCountry] = field.BlockType(string(FieldCountry), "Country", commonInputFields(localized)...)
	definitions[FieldMessage] = field.BlockType(string(FieldMessage), "Message", localizedTextarea("message", localized, field.Label("Message")))
	definitions[FieldSelect] = choiceBlock(FieldSelect, "Select", localized, true)
	definitions[FieldRadio] = choiceBlock(FieldRadio, "Radio", localized, false)
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

func commonInputFields(localized bool, additional ...field.Definition) []field.Definition {
	definitions := []field.Definition{
		field.Row(
			field.Text("name", field.Label("Name"), field.Required(), field.Columns(6)),
			localizedText("label", localized, field.Label("Label"), field.Columns(6)),
		),
		field.Row(
			field.Number("width", field.Label("Field width (%)"), field.Min(1), field.Max(100), field.Columns(6)),
			field.Checkbox("required", field.Label("Required"), field.Columns(6)),
		),
	}
	return append(definitions, additional...)
}

func choiceBlock(kind FieldType, label string, localized, placeholder bool) field.Block {
	definitions := commonInputFields(localized, localizedText("defaultValue", localized, field.Label("Default value")))
	if placeholder {
		definitions = append(definitions, localizedText("placeholder", localized, field.Label("Placeholder")))
	}
	definitions = append(definitions, field.Array("options",
		field.Label(label+" options"),
		field.Required(),
		field.MinRows(1),
		field.RowLabel("label"),
		field.ArrayRowLabels(field.RowLabels{Singular: "Option", Plural: "Options"}),
		field.Fields(
			localizedText("label", localized, field.Label("Label"), field.Required(), field.Columns(6)),
			field.Text("value", field.Label("Value"), field.Required(), field.Columns(6)),
		),
	))
	return field.BlockType(string(kind), label, definitions...)
}

func (plugin *Plugin) uploadBlock(localized bool) field.Block {
	choices := make([]field.Choice, len(plugin.config.UploadCollections))
	for index, slug := range plugin.config.UploadCollections {
		choices[index] = field.Choice{Value: string(slug), Label: string(slug)}
	}
	definitions := commonInputFields(localized)
	definitions = append(definitions,
		field.Select("uploadCollection", field.Label("Upload collection"), field.Required(), field.Choices(choices...)),
		field.Array("mimeTypes", field.Label("Allowed MIME types"), field.ArrayRowLabels(field.RowLabels{Singular: "MIME type", Plural: "MIME types"}), field.Fields(field.Text("mimeType", field.Required(), field.Label("MIME type")))),
		field.Row(
			field.Number("maxFileSize", field.Label("Maximum file size (bytes)"), field.Min(1), field.Columns(6)),
			field.Checkbox("multiple", field.Label("Allow multiple files"), field.Columns(6)),
		),
	)
	return field.BlockType(string(FieldUpload), "Upload", definitions...)
}

func (plugin *Plugin) paymentBlock(localized bool) field.Block {
	definitions := commonInputFields(localized,
		field.Number("basePrice", field.Label("Base price"), field.Required(), field.Min(0)),
		field.Select("paymentProcessor", field.Label("Payment processor"), field.Required(), field.Choices(plugin.config.PaymentProcessors...)),
		field.Array("priceConditions", field.Label("Price conditions"), field.ArrayRowLabels(field.RowLabels{Singular: "Price condition", Plural: "Price conditions"}), field.Fields(
			field.Text("fieldToUse", field.Label("Field to use"), field.Required()),
			field.Select("condition", field.Label("Condition"), field.Required(), field.Default("hasValue"), field.Choices(
				field.Choice{Value: "hasValue", Label: "Has any value"},
				field.Choice{Value: "equals", Label: "Equals"},
				field.Choice{Value: "notEquals", Label: "Does not equal"},
			)),
			field.Text("valueForCondition", field.Label("Comparison value"), field.Description("Used by equals and does-not-equal conditions.")),
			field.Select("operator", field.Label("Operator"), field.Required(), field.Default("add"), field.Choices(
				field.Choice{Value: "add", Label: "Add"}, field.Choice{Value: "subtract", Label: "Subtract"},
				field.Choice{Value: "multiply", Label: "Multiply"}, field.Choice{Value: "divide", Label: "Divide"},
			)),
			field.Radio("valueType", field.Label("Value type"), field.Required(), field.Default("static"), field.Choices(
				field.Choice{Value: "static", Label: "Static value"}, field.Choice{Value: "valueOfField", Label: "Value of another field"},
			)),
			field.Text("valueForOperator", field.Label("Value"), field.Required()),
		)),
	)
	return field.BlockType(string(FieldPayment), "Payment", definitions...)
}

func localizedText(name string, localized bool, options ...field.StringOption) field.Definition {
	if localized {
		options = append(options, field.Localized())
	}
	return field.Text(name, options...)
}

func localizedTextarea(name string, localized bool, options ...field.StringOption) field.Definition {
	if localized {
		options = append(options, field.Localized())
	}
	return field.Textarea(name, options...)
}

func (plugin *Plugin) formsCollection(blocks []field.Block, localized bool, adminCollection schema.CollectionSlug) ridu.Collection {
	redirectFields := []field.Definition{}
	if len(plugin.config.RedirectRelationships) != 0 {
		redirectFields = append(redirectFields, field.Radio("type", field.Label("Redirect type"), field.Default("reference"), field.Choices(
			field.Choice{Value: "custom", Label: "Custom URL"}, field.Choice{Value: "reference", Label: "Internal link"},
		)))
		targets := make([]string, len(plugin.config.RedirectRelationships))
		for index, slug := range plugin.config.RedirectRelationships {
			targets[index] = string(slug)
		}
		redirectFields = append(redirectFields, field.Relationship("reference", field.Label("Document to link to"), field.ToAny(targets...), field.ShowWhen("type", "reference")))
		redirectFields = append(redirectFields, field.Text("url", field.Label("URL to redirect to"), field.ShowWhen("type", "custom")))
	} else {
		redirectFields = append(redirectFields, field.Text("url", field.Label("URL to redirect to")))
	}

	return ridu.Collection{
		Slug:   plugin.config.FormsSlug,
		Labels: ridu.CollectionLabels{Singular: "Form", Plural: "Forms"},
		Admin:  ridu.CollectionAdmin{UseAsTitle: "title", Group: "Form Builder", DefaultColumns: []string{"title"}},
		FieldAccess: map[string]ridu.FieldAccess{
			"emails": {Read: func(context ridu.FieldAccessContext) (bool, error) {
				return (context.Actor != nil && (adminCollection == "" || context.ActorCollection == adminCollection)) || context.Context.Value(emailConfigReadKey{}) == true, nil
			}},
		},
		Fields: []field.Definition{
			field.Text("title", field.Label("Title"), field.Required()),
			field.Blocks("fields", field.Label("Fields"), field.BlockTypes(blocks...)),
			localizedText("submitButtonLabel", localized, field.Label("Submit button"), field.Default("Submit")),
			field.Radio("confirmationType", field.Label("Confirmation type"), field.Default("message"), field.Required(), field.Description("Choose whether successful submissions show a message or redirect."), field.Choices(
				field.Choice{Value: "message", Label: "Message"}, field.Choice{Value: "redirect", Label: "Redirect"},
			)),
			localizedTextarea("confirmationMessage", localized, field.Label("Confirmation message"), field.ShowWhen("confirmationType", "message")),
			field.Group("redirect", field.Label("Redirect"), field.ShowWhen("confirmationType", "redirect"), field.Fields(redirectFields...)),
			field.Array("emails", field.Label("Emails"), field.Description("Send dynamic emails after a submission. Use {{field_name}}, {{*}}, or {{*:table}} in text."), field.ArrayRowLabels(field.RowLabels{Singular: "Email", Plural: "Emails"}), field.Fields(
				field.Text("emailTo", field.Label("Email to")),
				field.Row(field.Text("cc", field.Label("CC"), field.Columns(6)), field.Text("bcc", field.Label("BCC"), field.Columns(6))),
				field.Row(field.Text("replyTo", field.Label("Reply to"), field.Columns(6)), field.Text("emailFrom", field.Label("Email from"), field.Columns(6))),
				localizedText("subject", localized, field.Label("Subject"), field.Required(), field.Default("You've received a new message.")),
				localizedTextarea("message", localized, field.Label("Message"), field.Description("Submission placeholders are HTML-escaped before insertion.")),
			)),
		},
		Access: ridu.CollectionAccess{
			Create: allowAdminCollection(adminCollection), Read: allowAll,
			Update: allowAdminCollection(adminCollection), Delete: allowAdminCollection(adminCollection),
		},
	}
}

func (plugin *Plugin) submissionsCollection(adminCollection schema.CollectionSlug, allowedFieldTypes map[FieldType]struct{}) ridu.Collection {
	fields := []field.Definition{
		field.Relationship("form", field.Label("Form"), field.To(string(plugin.config.FormsSlug)), field.Required(), field.OnDelete(field.ReferenceDeleteRestrict)),
		field.Array("submissionData", field.Label("Submission data"), field.ArrayRowLabels(field.RowLabels{Singular: "Submitted field", Plural: "Submission data"}), field.Fields(
			field.Text("field", field.Label("Field"), field.Required()),
			field.JSON("value", field.Label("Value"), field.Required()),
		)),
	}
	if len(plugin.config.UploadCollections) != 0 {
		targets := make([]string, len(plugin.config.UploadCollections))
		for index, slug := range plugin.config.UploadCollections {
			targets[index] = string(slug)
		}
		fields = append(fields, field.Array("submissionUploads", field.Label("Submission uploads"), field.ArrayRowLabels(field.RowLabels{Singular: "Submitted upload", Plural: "Submission uploads"}), field.Fields(
			field.Text("field", field.Label("Field"), field.Required()),
			field.Relationship("value", field.Label("Files"), field.ToAny(targets...), field.HasMany(), field.Required(), field.OnDelete(field.ReferenceDeleteRestrict)),
		)))
	}
	if _, enabled := allowedFieldTypes[FieldPayment]; enabled {
		fields = append(fields, field.JSON("payment", field.Label("Payment details"), field.ReadOnly()))
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
