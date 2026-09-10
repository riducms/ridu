// Package formbuilder contributes Payload-familiar dynamic form definitions,
// submission persistence, validation, payment callbacks, and email delivery.
package formbuilder

import (
	"fmt"
	"slices"

	ridu "github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

const (
	// Key is the stable backend/admin plugin identity.
	Key = "form-builder"
	// AdminPluginPairingVersion changes when the Go and TypeScript packages no
	// longer understand the same generated form contract.
	AdminPluginPairingVersion = 1
	// DefaultFormsSlug is the collection containing reusable form definitions.
	DefaultFormsSlug schema.CollectionSlug = "forms"
	// DefaultSubmissionsSlug is the collection containing captured responses.
	DefaultSubmissionsSlug schema.CollectionSlug = "form-submissions"
)

// FieldType identifies one dynamic input block available to editors.
type FieldType string

const (
	FieldCheckbox FieldType = "checkbox"
	FieldCountry  FieldType = "country"
	FieldDate     FieldType = "date"
	FieldEmail    FieldType = "email"
	FieldMessage  FieldType = "message"
	FieldNumber   FieldType = "number"
	FieldPayment  FieldType = "payment"
	FieldRadio    FieldType = "radio"
	FieldSelect   FieldType = "select"
	FieldState    FieldType = "state"
	FieldText     FieldType = "text"
	FieldTextarea FieldType = "textarea"
	FieldUpload   FieldType = "upload"
)

var defaultFieldTypes = []FieldType{
	FieldCheckbox, FieldCountry, FieldEmail, FieldMessage, FieldNumber,
	FieldSelect, FieldState, FieldText, FieldTextarea,
}

// FieldsOverride receives detached default block definitions and returns the
// complete ordered set available in the form builder.
type FieldsOverride func(defaultFields []field.Block) ([]field.Block, error)

// CollectionOverride receives a detached generated collection and returns its
// complete replacement. The plugin reasserts lifecycle invariants afterward.
type CollectionOverride func(defaultCollection ridu.Collection) (ridu.Collection, error)

// Config customizes the generated collections and executable form lifecycle.
type Config struct {
	FormsSlug             schema.CollectionSlug
	SubmissionsSlug       schema.CollectionSlug
	EnabledFields         []FieldType
	UploadCollections     []schema.CollectionSlug
	RedirectRelationships []schema.CollectionSlug
	PaymentProcessors     []field.Option
	Fields                FieldsOverride
	Forms                 CollectionOverride
	Submissions           CollectionOverride
	DefaultToEmail        string
	BeforeEmail           BeforeEmail
	SendEmail             SendEmail
	HandlePayment         HandlePayment
	ReportError           func(error, string)
}

// Plugin is one configured Form Builder extension.
type Plugin struct {
	config Config
}

// New returns one compiled plugin. Slice-bearing config is detached so later
// application mutation cannot change the resolved schema or runtime behavior.
func New(config Config) *Plugin {
	config.EnabledFields = append([]FieldType(nil), config.EnabledFields...)
	config.UploadCollections = append([]schema.CollectionSlug(nil), config.UploadCollections...)
	config.RedirectRelationships = append([]schema.CollectionSlug(nil), config.RedirectRelationships...)
	config.PaymentProcessors = cloneOptions(config.PaymentProcessors)
	if config.FormsSlug == "" {
		config.FormsSlug = DefaultFormsSlug
	}
	if config.SubmissionsSlug == "" {
		config.SubmissionsSlug = DefaultSubmissionsSlug
	}
	return &Plugin{config: config}
}

// Key implements ridu.Plugin.
func (*Plugin) Key() string { return Key }

// Descriptor exposes deterministic backend/admin pairing metadata.
func (*Plugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{
		Version:    ridu.FrameworkVersion,
		GoPackage:  "github.com/riducms/ridu/plugins/formbuilder",
		APIVersion: ridu.PluginAPIVersion,
		Ridu:       ridu.RiduCompatibility{Minimum: ridu.FrameworkVersion},
		Admin: &ridu.AdminPluginMetadata{
			Package:        "@riducms/plugin-form-builder/admin",
			Export:         "formBuilderAdminPlugin",
			APIVersion:     ridu.AdminPluginAPIVersion,
			PairingVersion: AdminPluginPairingVersion,
		},
	}
}

// TransformConfig adds the Forms and Form Submissions collections through the
// same public config surface available to application code.
func (plugin *Plugin) TransformConfig(config ridu.Config) (ridu.Config, error) {
	if issues := plugin.validateConfig(config); len(issues) != 0 {
		return ridu.Config{}, schema.NewValidationError(issues)
	}
	blocks, err := plugin.formFieldBlocks(len(config.Localization.Locales) != 0)
	if err != nil {
		return ridu.Config{}, fmt.Errorf("form builder fields override: %w", err)
	}
	effectiveTypes := formFieldTypes(blocks)
	if issues := plugin.validateEffectiveFields(effectiveTypes); len(issues) != 0 {
		return ridu.Config{}, schema.NewValidationError(issues)
	}
	forms := plugin.formsCollection(blocks, len(config.Localization.Locales) != 0, config.Admin.User)
	if plugin.config.Forms != nil {
		forms, err = plugin.config.Forms(forms)
		if err != nil {
			return ridu.Config{}, fmt.Errorf("form builder forms override: %w", err)
		}
	}
	allowedFieldTypes := effectiveTypes
	forms = plugin.secureFormsCollection(forms, allowedFieldTypes)

	submissions := plugin.submissionsCollection(config.Admin.User, allowedFieldTypes)
	if plugin.config.Submissions != nil {
		submissions, err = plugin.config.Submissions(submissions)
		if err != nil {
			return ridu.Config{}, fmt.Errorf("form builder submissions override: %w", err)
		}
	}
	submissions = plugin.secureSubmissionsCollection(submissions, allowedFieldTypes)

	config.Collections = append(config.Collections, forms, submissions)
	return config, nil
}

func (plugin *Plugin) validateEffectiveFields(types map[FieldType]struct{}) []schema.Issue {
	var issues []schema.Issue
	if _, enabled := types[FieldUpload]; enabled && len(plugin.config.UploadCollections) == 0 {
		issues = append(issues, schema.Issue{Code: "missing_form_upload_collection", Path: "plugins[form-builder].uploadCollections", Message: "upload fields require at least one upload-enabled collection"})
	}
	if _, enabled := types[FieldPayment]; enabled {
		if len(plugin.config.PaymentProcessors) == 0 {
			issues = append(issues, schema.Issue{Code: "missing_form_payment_processor", Path: "plugins[form-builder].paymentProcessors", Message: "payment fields require at least one payment processor"})
		}
		if plugin.config.HandlePayment == nil {
			issues = append(issues, schema.Issue{Code: "missing_form_payment_handler", Path: "plugins[form-builder].handlePayment", Message: "payment fields require a payment handler"})
		}
	}
	return issues
}

func (plugin *Plugin) validateConfig(config ridu.Config) []schema.Issue {
	collections := make(map[schema.CollectionSlug]ridu.Collection, len(config.Collections))
	for _, collection := range config.Collections {
		collections[collection.Slug] = collection
	}
	var issues []schema.Issue
	for name, slug := range map[string]schema.CollectionSlug{"formsSlug": plugin.config.FormsSlug, "submissionsSlug": plugin.config.SubmissionsSlug} {
		if _, exists := collections[slug]; exists {
			issues = append(issues, schema.Issue{Code: "form_builder_slug_conflict", Path: "plugins[form-builder]." + name, Message: fmt.Sprintf("collection %q already exists", slug)})
		}
	}
	if plugin.config.FormsSlug == plugin.config.SubmissionsSlug {
		issues = append(issues, schema.Issue{Code: "form_builder_slug_conflict", Path: "plugins[form-builder].submissionsSlug", Message: "forms and submissions slugs must differ"})
	}
	seenUploads := make(map[schema.CollectionSlug]struct{}, len(plugin.config.UploadCollections))
	for index, slug := range plugin.config.UploadCollections {
		path := fmt.Sprintf("plugins[form-builder].uploadCollections[%d]", index)
		collection, exists := collections[slug]
		switch {
		case !exists:
			issues = append(issues, schema.Issue{Code: "unknown_form_upload_collection", Path: path, Message: fmt.Sprintf("upload collection %q does not exist", slug)})
		case !collection.Upload:
			issues = append(issues, schema.Issue{Code: "invalid_form_upload_collection", Path: path, Message: fmt.Sprintf("collection %q is not upload-enabled", slug)})
		case hasSlug(seenUploads, slug):
			issues = append(issues, schema.Issue{Code: "duplicate_form_upload_collection", Path: path, Message: fmt.Sprintf("upload collection %q is selected more than once", slug)})
		}
		seenUploads[slug] = struct{}{}
	}
	seenRedirects := make(map[schema.CollectionSlug]struct{}, len(plugin.config.RedirectRelationships))
	for index, slug := range plugin.config.RedirectRelationships {
		path := fmt.Sprintf("plugins[form-builder].redirectRelationships[%d]", index)
		if _, exists := collections[slug]; !exists {
			issues = append(issues, schema.Issue{Code: "unknown_form_redirect_collection", Path: path, Message: fmt.Sprintf("redirect collection %q does not exist", slug)})
		} else if hasSlug(seenRedirects, slug) {
			issues = append(issues, schema.Issue{Code: "duplicate_form_redirect_collection", Path: path, Message: fmt.Sprintf("redirect collection %q is selected more than once", slug)})
		}
		seenRedirects[slug] = struct{}{}
	}
	seenFields := make(map[FieldType]struct{})
	for index, fieldType := range plugin.enabledFields() {
		path := fmt.Sprintf("plugins[form-builder].enabledFields[%d]", index)
		if !validFieldType(fieldType) {
			issues = append(issues, schema.Issue{Code: "unknown_form_field_type", Path: path, Message: fmt.Sprintf("form field type %q is not supported", fieldType)})
		} else if _, exists := seenFields[fieldType]; exists {
			issues = append(issues, schema.Issue{Code: "duplicate_form_field_type", Path: path, Message: fmt.Sprintf("form field type %q is enabled more than once", fieldType)})
		}
		seenFields[fieldType] = struct{}{}
	}
	if _, enabled := seenFields[FieldUpload]; enabled && len(plugin.config.UploadCollections) == 0 {
		issues = append(issues, schema.Issue{Code: "missing_form_upload_collection", Path: "plugins[form-builder].uploadCollections", Message: "upload fields require at least one upload-enabled collection"})
	}
	if _, enabled := seenFields[FieldPayment]; enabled && len(plugin.config.PaymentProcessors) == 0 {
		issues = append(issues, schema.Issue{Code: "missing_form_payment_processor", Path: "plugins[form-builder].paymentProcessors", Message: "payment fields require at least one payment processor"})
	}
	if _, enabled := seenFields[FieldPayment]; enabled && plugin.config.HandlePayment == nil {
		issues = append(issues, schema.Issue{Code: "missing_form_payment_handler", Path: "plugins[form-builder].handlePayment", Message: "payment fields require a payment handler"})
	}
	return issues
}

func (plugin *Plugin) enabledFields() []FieldType {
	if plugin.config.EnabledFields == nil {
		return append([]FieldType(nil), defaultFieldTypes...)
	}
	return append([]FieldType(nil), plugin.config.EnabledFields...)
}

func validFieldType(value FieldType) bool {
	return slices.Contains([]FieldType{FieldCheckbox, FieldCountry, FieldDate, FieldEmail, FieldMessage, FieldNumber, FieldPayment, FieldRadio, FieldSelect, FieldState, FieldText, FieldTextarea, FieldUpload}, value)
}

func formFieldTypes(blocks []field.Block) map[FieldType]struct{} {
	types := make(map[FieldType]struct{}, len(blocks))
	for _, block := range blocks {
		types[FieldType(block.Slug)] = struct{}{}
	}
	return types
}

func hasSlug(values map[schema.CollectionSlug]struct{}, slug schema.CollectionSlug) bool {
	_, exists := values[slug]
	return exists
}

func allowAll(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil }
func denyAll(ridu.AccessContext) (ridu.AccessDecision, error)  { return ridu.Deny(), nil }
func allowAdminCollection(collection schema.CollectionSlug) ridu.AccessRule {
	return func(context ridu.AccessContext) (ridu.AccessDecision, error) {
		if context.Actor == nil || (collection != "" && context.ActorCollection != collection) {
			return ridu.Deny(), nil
		}
		return ridu.Allow(), nil
	}
}

func cloneOptions(options []field.Option) []field.Option {
	cloned := make([]field.Option, len(options))
	for index, option := range options {
		cloned[index] = option
		if option.LabelTranslations != nil {
			cloned[index].LabelTranslations = make(map[string]string, len(option.LabelTranslations))
			for language, label := range option.LabelTranslations {
				cloned[index].LabelTranslations[language] = label
			}
		}
	}
	return cloned
}

var (
	_ ridu.ConfigTransformer  = (*Plugin)(nil)
	_ ridu.DescriptorProvider = (*Plugin)(nil)
)
