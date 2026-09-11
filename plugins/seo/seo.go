// Package seo contributes Payload-familiar search metadata fields and a paired
// admin authoring experience without moving executable generators into the
// schema manifest.
package seo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"

	ridu "github.com/riducms/ridu"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const (
	// Key is the stable backend/admin plugin identity.
	Key = "seo"
	// AdminPluginPairingVersion changes when the Go and admin packages cease to
	// understand the same renderer configuration.
	AdminPluginPairingVersion = 1

	defaultTitleMin       = 50
	defaultTitleMax       = 60
	defaultDescriptionMin = 100
	defaultDescriptionMax = 150
)

// GenerateContext is the trusted runtime input for one generator. Document is
// the current unsaved admin form snapshot. Collection and Global are detached
// authoring definitions; exactly one is non-nil.
type GenerateContext struct {
	Context    ridu.EndpointContext
	Document   map[string]any
	ID         string
	Locale     schema.LocaleCode
	Collection *ridu.Collection
	Global     *ridu.Global
}

// GenerateText produces a title, description, or preview URL.
type GenerateText func(GenerateContext) (string, error)

// GenerateImage produces the stable ID of an upload document.
type GenerateImage func(GenerateContext) (string, error)

// FieldsOverride receives the immutable default field definitions and returns
// the complete ordered contents of the meta group.
type FieldsOverride func(defaultFields field.Fields) (field.Fields, error)

// Config selects resources and executable generation behavior.
type Config struct {
	Collections         []schema.CollectionSlug
	Globals             []schema.CollectionSlug
	UploadsCollection   schema.CollectionSlug
	TabbedUI            bool
	Fields              FieldsOverride
	GenerateTitle       GenerateText
	GenerateDescription GenerateText
	GenerateImage       GenerateImage
	GenerateURL         GenerateText
}

// OverviewConfig maps the presentation-only overview checks to stored fields.
type OverviewConfig struct {
	Label           string
	TitlePath       string
	DescriptionPath string
	ImagePath       string
	TitleMin        int
	TitleMax        int
	DescriptionMin  int
	DescriptionMax  int
}

// PreviewConfig maps the search preview to stored title and description
// fields. Generate enables the URL callback control.
type PreviewConfig struct {
	Generate        bool
	Label           string
	TitlePath       string
	DescriptionPath string
}

// MetaImageConfig configures the upload target and image presentation.
type MetaImageConfig struct {
	Collection  schema.CollectionSlug
	Generate    bool
	Label       string
	Description string
}

// Plugin is one configured SEO extension.
type Plugin struct {
	config      Config
	mu          sync.RWMutex
	collections map[schema.CollectionSlug]ridu.Collection
	globals     map[schema.CollectionSlug]ridu.Global
	fieldsGraph field.Fields
}

// New returns one compiled plugin. Slices are copied so later application
// mutation cannot change target selection or endpoint admission.
func New(config Config) *Plugin {
	config.Collections = append([]schema.CollectionSlug(nil), config.Collections...)
	config.Globals = append([]schema.CollectionSlug(nil), config.Globals...)
	return &Plugin{config: config, collections: make(map[schema.CollectionSlug]ridu.Collection), globals: make(map[schema.CollectionSlug]ridu.Global)}
}

// Key implements ridu.Plugin.
func (*Plugin) Key() string { return Key }

// Descriptor exposes only deterministic build metadata. Generators remain
// executable Go values owned by the runtime plugin instance.
func (*Plugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{
		Version: ridu.FrameworkVersion, GoPackage: "github.com/riducms/ridu/plugins/seo",
		APIVersion: ridu.PluginAPIVersion,
		Ridu:       ridu.RiduCompatibility{Minimum: ridu.FrameworkVersion},
		Admin: &ridu.AdminPluginMetadata{
			Package: "@riducms/plugin-seo", Export: "seoAdminPlugin",
			APIVersion: ridu.AdminPluginAPIVersion, PairingVersion: AdminPluginPairingVersion,
		},
	}
}

// TransformConfig validates resource selection and snapshots generator context.
// Field injection runs in the canonical graph phase after resource transforms.
func (plugin *Plugin) TransformConfig(config ridu.Config) (ridu.Config, error) {
	issues := plugin.validateTargets(config)
	if len(issues) != 0 {
		return ridu.Config{}, schema.NewValidationError(issues)
	}
	fields, err := plugin.fields(len(config.Localization.Locales) != 0)
	if err != nil {
		return ridu.Config{}, fmt.Errorf("seo fields override: %w", err)
	}
	plugin.mu.Lock()
	plugin.fieldsGraph = fields.Snapshot()
	plugin.collections = make(map[schema.CollectionSlug]ridu.Collection, len(config.Collections))
	plugin.globals = make(map[schema.CollectionSlug]ridu.Global, len(config.Globals))
	for _, collection := range config.Collections {
		plugin.collections[collection.Slug] = cloneCollection(collection)
	}
	for _, global := range config.Globals {
		plugin.globals[global.Slug] = cloneGlobal(global)
	}
	plugin.mu.Unlock()
	return config, nil
}

// TransformFields attaches the SEO group through the same immutable graph used
// by application fields, preserving behavior on every existing node.
func (plugin *Plugin) TransformFields(context core.FieldGraphContext, graph field.Fields) (field.Fields, error) {
	selected := slices.Contains(plugin.config.Collections, context.Slug)
	if context.ResourceKind == "global" {
		selected = slices.Contains(plugin.config.Globals, context.Slug)
	}
	if !selected {
		return graph, nil
	}
	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	label, auth := "", false
	if context.ResourceKind == "collection" {
		collection := plugin.collections[context.Slug]
		label, auth = collection.Labels.Singular, collection.Auth
	} else {
		label = plugin.globals[context.Slug].Label
	}
	result, err := plugin.inject(graph, label, plugin.fieldsGraph, auth)
	if err != nil {
		return graph, err
	}
	if context.ResourceKind == "collection" {
		collection := plugin.collections[context.Slug]
		collection.Fields = result.Snapshot()
		plugin.collections[context.Slug] = collection
	} else {
		global := plugin.globals[context.Slug]
		global.Fields = result.Snapshot()
		plugin.globals[context.Slug] = global
	}
	return result, nil
}

func (plugin *Plugin) fields(localized bool) (field.Fields, error) {
	defaults := field.Fields{
		Overview(OverviewConfig{}),
		MetaTitle(plugin.config.GenerateTitle != nil).Localized(localized),
		MetaDescription(plugin.config.GenerateDescription != nil).Localized(localized),
	}
	if plugin.config.UploadsCollection != "" {
		defaults = append(defaults, MetaImage(MetaImageConfig{Collection: plugin.config.UploadsCollection, Generate: plugin.config.GenerateImage != nil}).Localized(localized))
	}
	defaults = append(defaults, Preview(PreviewConfig{Generate: plugin.config.GenerateURL != nil}))
	if plugin.config.Fields == nil {
		return defaults, nil
	}
	fields, err := plugin.config.Fields(defaults.Snapshot())
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, errors.New("override returned no fields")
	}
	return fields.Snapshot(), nil
}

func (plugin *Plugin) inject(existing field.Fields, contentLabel string, fields field.Fields, auth bool) (field.Fields, error) {
	meta := field.Group("meta", fields).Label("SEO")
	if !plugin.config.TabbedUI {
		return existing.Edit(func(draft *field.ChildrenDraft) error { return draft.Insert(len(existing), meta) })
	}
	seoTab := field.UnnamedTab("SEO", field.Fields{
		meta,
	})
	if len(existing) != 0 && existing[0].Kind() == field.KindTabs {
		result := field.Fields{
			existing[0],
			seoTab,
		}
		return append(result, existing[1:]...).Snapshot(), nil
	}
	leading, content, tabs := field.Fields{}, field.Fields{}, field.Fields{}
	for _, node := range existing {
		if auth && node.Name() == "email" {
			leading = append(leading, node)
		} else if node.Kind() == field.KindTabs || field.Snapshot(node).IsNamedTab() {
			tabs = append(tabs, node)
		} else {
			content = append(content, node)
		}
	}
	contentLabel = strings.TrimSpace(contentLabel)
	if contentLabel == "" {
		contentLabel = "Content"
	}
	if len(content) > 0 {
		leading = append(leading, field.UnnamedTab(contentLabel, content))
	}
	leading = append(leading, tabs...)
	return append(leading, seoTab).Snapshot(), nil
}

func (plugin *Plugin) validateTargets(config ridu.Config) []schema.Issue {
	collections := make(map[schema.CollectionSlug]ridu.Collection, len(config.Collections))
	for _, collection := range config.Collections {
		collections[collection.Slug] = collection
	}
	globals := make(map[schema.CollectionSlug]struct{}, len(config.Globals))
	for _, global := range config.Globals {
		globals[global.Slug] = struct{}{}
	}
	var issues []schema.Issue
	seen := make(map[schema.CollectionSlug]struct{}, len(plugin.config.Collections))
	for index, slug := range plugin.config.Collections {
		path := fmt.Sprintf("plugins[%s].collections[%d]", Key, index)
		if _, duplicate := seen[slug]; duplicate {
			issues = append(issues, schema.Issue{Code: "duplicate_seo_collection", Path: path, Message: fmt.Sprintf("SEO collection %q is selected more than once", slug)})
		} else if _, exists := collections[slug]; !exists {
			issues = append(issues, schema.Issue{Code: "unknown_seo_collection", Path: path, Message: fmt.Sprintf("SEO collection %q does not exist", slug)})
		}
		seen[slug] = struct{}{}
	}
	seen = make(map[schema.CollectionSlug]struct{}, len(plugin.config.Globals))
	for index, slug := range plugin.config.Globals {
		path := fmt.Sprintf("plugins[%s].globals[%d]", Key, index)
		if _, duplicate := seen[slug]; duplicate {
			issues = append(issues, schema.Issue{Code: "duplicate_seo_global", Path: path, Message: fmt.Sprintf("SEO global %q is selected more than once", slug)})
		} else if _, exists := globals[slug]; !exists {
			issues = append(issues, schema.Issue{Code: "unknown_seo_global", Path: path, Message: fmt.Sprintf("SEO global %q does not exist", slug)})
		}
		seen[slug] = struct{}{}
	}
	if slug := plugin.config.UploadsCollection; slug != "" {
		collection, exists := collections[slug]
		if !exists || !collection.Upload {
			issues = append(issues, schema.Issue{Code: "invalid_seo_upload_collection", Path: "plugins[seo].uploadsCollection", Message: fmt.Sprintf("SEO upload collection %q must exist and enable uploads", slug)})
		}
	}
	return issues
}

// Endpoints contributes generation calls below /api/plugins/seo/. All are
// declared for a stable OpenAPI surface; absent callbacks return an empty value.
func (plugin *Plugin) Endpoints() []ridu.Endpoint {
	collections, globals := plugin.resourceSnapshot()
	return []ridu.Endpoint{
		plugin.endpoint("generate-title", "Generate SEO title", plugin.config.GenerateTitle, true, collections, globals),
		plugin.endpoint("generate-description", "Generate SEO description", plugin.config.GenerateDescription, true, collections, globals),
		plugin.endpoint("generate-image", "Generate SEO image", func(ctx GenerateContext) (string, error) {
			if plugin.config.GenerateImage == nil {
				return "", nil
			}
			return plugin.config.GenerateImage(ctx)
		}, true, collections, globals),
		plugin.endpoint("generate-url", "Generate SEO preview URL", plugin.config.GenerateURL, false, collections, globals),
	}
}

func (plugin *Plugin) resourceSnapshot() (map[schema.CollectionSlug]ridu.Collection, map[schema.CollectionSlug]ridu.Global) {
	plugin.mu.RLock()
	defer plugin.mu.RUnlock()
	collections := make(map[schema.CollectionSlug]ridu.Collection, len(plugin.collections))
	for slug, collection := range plugin.collections {
		collections[slug] = cloneCollection(collection)
	}
	globals := make(map[schema.CollectionSlug]ridu.Global, len(plugin.globals))
	for slug, global := range plugin.globals {
		globals[slug] = cloneGlobal(global)
	}
	return collections, globals
}

type generationRequest struct {
	Collection string            `json:"collection,omitempty"`
	Global     string            `json:"global,omitempty"`
	ID         string            `json:"id,omitempty"`
	Locale     schema.LocaleCode `json:"locale,omitempty"`
	Document   map[string]any    `json:"document"`
}

func (plugin *Plugin) endpoint(path, summary string, generator GenerateText, requireWrite bool, collections map[schema.CollectionSlug]ridu.Collection, globals map[schema.CollectionSlug]ridu.Global) ridu.Endpoint {
	return ridu.Endpoint{Method: http.MethodPost, Path: path, Summary: summary, MaxBodyBytes: 1 << 20, Handler: func(endpoint ridu.EndpointContext) {
		if endpoint.Actor == nil {
			writeError(endpoint.Writer, http.StatusUnauthorized, protocol.ErrorAccess, "authentication is required")
			return
		}
		decoder := json.NewDecoder(endpoint.Request.Body)
		decoder.DisallowUnknownFields()
		var request generationRequest
		if err := decoder.Decode(&request); err != nil {
			writeError(endpoint.Writer, http.StatusBadRequest, protocol.ErrorBadRequest, "request must contain one valid SEO generation input")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeError(endpoint.Writer, http.StatusBadRequest, protocol.ErrorBadRequest, "request must contain exactly one SEO generation input")
			return
		}
		context, status, err := plugin.generationContext(endpoint, request, requireWrite, collections, globals)
		if err != nil {
			plugin.writeContextError(endpoint, status, err)
			return
		}
		result := ""
		if generator != nil {
			result, err = generator(context)
			if err != nil {
				if endpoint.ReportError != nil {
					endpoint.ReportError(err, "seo_generation_failed")
				}
				writeError(endpoint.Writer, http.StatusInternalServerError, protocol.ErrorInternal, "SEO generation failed")
				return
			}
		}
		endpoint.Writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(endpoint.Writer).Encode(map[string]string{"result": result})
	}}
}

func (plugin *Plugin) generationContext(endpoint ridu.EndpointContext, request generationRequest, requireWrite bool, collections map[schema.CollectionSlug]ridu.Collection, globals map[schema.CollectionSlug]ridu.Global) (GenerateContext, int, error) {
	if (request.Collection == "") == (request.Global == "") {
		return GenerateContext{}, http.StatusBadRequest, errors.New("exactly one collection or global is required")
	}
	context := GenerateContext{Context: endpoint, Document: request.Document, ID: request.ID, Locale: request.Locale}
	if request.Collection != "" {
		collection, enabled := collections[schema.CollectionSlug(request.Collection)]
		if !enabled {
			return GenerateContext{}, http.StatusNotFound, errors.New("SEO is not enabled for this collection")
		}
		values, err := storeValues(request.Document)
		if err != nil {
			return GenerateContext{}, http.StatusBadRequest, errors.New("document contains unsupported values")
		}
		capabilities, err := endpoint.Local.Capabilities(endpoint.Request.Context(), request.Collection, request.ID, ridu.CapabilityOptions{Data: values, Actor: endpoint.Actor, ActorCollection: endpoint.ActorCollection, Locale: request.Locale})
		if err != nil {
			return GenerateContext{}, operationStatus(err), err
		}
		allowed := capabilities.Operations.Read
		if request.ID == "" {
			allowed = capabilities.Operations.Create
		} else if requireWrite {
			allowed = capabilities.Operations.Update
		}
		if !allowed {
			return GenerateContext{}, http.StatusForbidden, errors.New("document access was denied")
		}
		collection = cloneCollection(collection)
		context.Collection = &collection
		return context, 0, nil
	}
	global, enabled := globals[schema.CollectionSlug(request.Global)]
	if !enabled {
		return GenerateContext{}, http.StatusNotFound, errors.New("SEO is not enabled for this global")
	}
	values, err := storeValues(request.Document)
	if err != nil {
		return GenerateContext{}, http.StatusBadRequest, errors.New("document contains unsupported values")
	}
	capabilities, err := endpoint.Local.Capabilities(endpoint.Request.Context(), "global:"+request.Global, request.Global, ridu.CapabilityOptions{Data: values, Actor: endpoint.Actor, ActorCollection: endpoint.ActorCollection, Locale: request.Locale})
	if err != nil {
		return GenerateContext{}, operationStatus(err), err
	}
	allowed := capabilities.Operations.Read
	if requireWrite {
		allowed = capabilities.Operations.Update
	}
	if !allowed {
		return GenerateContext{}, http.StatusForbidden, errors.New("global access was denied")
	}
	global = cloneGlobal(global)
	context.Global = &global
	return context, 0, nil
}

func operationStatus(err error) int {
	var operationError *ridu.OperationError
	if errors.As(err, &operationError) && operationError.Status >= 400 {
		return operationError.Status
	}
	return http.StatusInternalServerError
}

func (plugin *Plugin) writeContextError(endpoint ridu.EndpointContext, status int, err error) {
	var operationError *ridu.OperationError
	if errors.As(err, &operationError) && operationError.Status >= 400 && operationError.Status < 500 {
		writeError(endpoint.Writer, operationError.Status, errorCode(operationError.Status), operationError.Message)
		return
	}
	if status >= 400 && status < 500 {
		writeError(endpoint.Writer, status, errorCode(status), err.Error())
		return
	}
	if endpoint.ReportError != nil {
		endpoint.ReportError(err, "seo_generation_context_failed")
	}
	writeError(endpoint.Writer, http.StatusInternalServerError, protocol.ErrorInternal, "SEO generation could not be authorized")
}

func storeValues(document map[string]any) (store.Values, error) {
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	var values store.Values
	if err := json.Unmarshal(encoded, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func writeError(writer http.ResponseWriter, status int, code protocol.ErrorCode, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(protocol.ErrorEnvelope{Error: protocol.ErrorPayload{Code: code, Status: status, Message: message, Issues: []protocol.ValidationIssue{}}})
}

func errorCode(status int) protocol.ErrorCode {
	switch status {
	case http.StatusBadRequest:
		return protocol.ErrorBadRequest
	case http.StatusUnauthorized, http.StatusForbidden:
		return protocol.ErrorAccess
	case http.StatusNotFound:
		return protocol.ErrorNotFound
	case http.StatusConflict:
		return protocol.ErrorConflict
	default:
		return protocol.ErrorInternal
	}
}

type lengthConfig struct {
	Generate  bool `json:"generate,omitempty"`
	MinLength int  `json:"minLength"`
	MaxLength int  `json:"maxLength"`
}

// Overview returns presentation-only checks mapped to explicit stored field paths.
// Zero values use the default meta paths and guidance bounds.
func Overview(selected OverviewConfig) field.LayoutField {
	if selected.Label == "" {
		selected.Label = "Overview"
	}
	if selected.TitlePath == "" {
		selected.TitlePath = "meta.title"
	}
	if selected.DescriptionPath == "" {
		selected.DescriptionPath = "meta.description"
	}
	if selected.ImagePath == "" {
		selected.ImagePath = "meta.image"
	}
	if selected.TitleMin == 0 {
		selected.TitleMin = defaultTitleMin
	}
	if selected.TitleMax == 0 {
		selected.TitleMax = defaultTitleMax
	}
	if selected.DescriptionMin == 0 {
		selected.DescriptionMin = defaultDescriptionMin
	}
	if selected.DescriptionMax == 0 {
		selected.DescriptionMax = defaultDescriptionMax
	}
	config := struct {
		TitlePath       string `json:"titlePath"`
		DescriptionPath string `json:"descriptionPath"`
		ImagePath       string `json:"imagePath"`
		TitleMin        int    `json:"titleMin"`
		TitleMax        int    `json:"titleMax"`
		DescriptionMin  int    `json:"descriptionMin"`
		DescriptionMax  int    `json:"descriptionMax"`
	}{selected.TitlePath, selected.DescriptionPath, selected.ImagePath, selected.TitleMin, selected.TitleMax, selected.DescriptionMin, selected.DescriptionMax}
	return field.UI("overview").Label(selected.Label).Admin(field.Admin{Editor: adminComponent("overview", config)})
}

// MetaTitle returns the localized built-in text field with the SEO renderer.
func MetaTitle(hasGenerator bool) field.TextField {
	return field.Text("title").Localized().Admin(field.Admin{Editor: adminComponent("title", lengthConfig{Generate: hasGenerator, MinLength: defaultTitleMin, MaxLength: defaultTitleMax})})
}

// MetaDescription returns the localized built-in textarea field with the SEO renderer.
func MetaDescription(hasGenerator bool) field.TextField {
	return field.Textarea("description").Localized().Admin(field.Admin{Editor: adminComponent("description", lengthConfig{Generate: hasGenerator, MinLength: defaultDescriptionMin, MaxLength: defaultDescriptionMax})})
}

// MetaImage returns the localized upload reference with the SEO renderer.
func MetaImage(selected MetaImageConfig) field.UploadField {
	if selected.Label == "" {
		selected.Label = "Meta Image"
	}
	if selected.Description == "" {
		selected.Description = "Maximum upload file size: 12MB. Recommended file size for images is <500KB."
	}
	config := struct {
		Generate bool `json:"generate,omitempty"`
	}{selected.Generate}
	return field.Upload("image", selected.Collection).Localized().Label(selected.Label).Admin(field.Admin{
		Description: selected.Description, Editor: adminComponent("image", config),
	})
}

// Preview returns a presentation-only search preview mapped to explicit stored paths.
func Preview(selected PreviewConfig) field.LayoutField {
	if selected.Label == "" {
		selected.Label = "Preview"
	}
	if selected.TitlePath == "" {
		selected.TitlePath = "meta.title"
	}
	if selected.DescriptionPath == "" {
		selected.DescriptionPath = "meta.description"
	}
	config := struct {
		Generate        bool   `json:"generate,omitempty"`
		TitlePath       string `json:"titlePath"`
		DescriptionPath string `json:"descriptionPath"`
	}{selected.Generate, selected.TitlePath, selected.DescriptionPath}
	return field.UI("preview").Label(selected.Label).Admin(field.Admin{Editor: adminComponent("preview", config)})
}

func cloneCollection(collection ridu.Collection) ridu.Collection {
	cloned := collection
	cloned.Fields = collection.Fields.Snapshot()
	cloned.Indexes = append([]ridu.CollectionIndex(nil), collection.Indexes...)
	for index := range cloned.Indexes {
		cloned.Indexes[index].Fields = append([]string(nil), collection.Indexes[index].Fields...)
	}
	cloned.Labels.SingularTranslations = cloneMap(collection.Labels.SingularTranslations)
	cloned.Labels.PluralTranslations = cloneMap(collection.Labels.PluralTranslations)
	cloned.Admin.DefaultColumns = append([]string(nil), collection.Admin.DefaultColumns...)
	cloned.Admin.GroupTranslations = cloneMap(collection.Admin.GroupTranslations)
	cloned.Admin.DescriptionTranslations = cloneMap(collection.Admin.DescriptionTranslations)
	cloned.Admin.LivePreview = cloneLivePreview(collection.Admin.LivePreview)
	cloned.UploadConfig.MimeTypes = append([]string(nil), collection.UploadConfig.MimeTypes...)
	cloned.UploadConfig.ImageSizes = append([]ridu.ImageSize(nil), collection.UploadConfig.ImageSizes...)
	cloned.Hooks = cloneHooks(collection.Hooks)
	cloned.AuthConfig.Strategies = append([]ridu.AuthStrategy(nil), collection.AuthConfig.Strategies...)
	cloned.AuthConfig.Hooks = cloneAuthHooks(collection.AuthConfig.Hooks)
	if collection.AuthConfig.Verify != nil {
		verify := *collection.AuthConfig.Verify
		cloned.AuthConfig.Verify = &verify
	}
	return cloned
}

func cloneAuthHooks(hooks ridu.AuthHooks) ridu.AuthHooks {
	hooks.BeforeLogin = append([]ridu.AuthHook(nil), hooks.BeforeLogin...)
	hooks.AfterLogin = append([]ridu.AuthHook(nil), hooks.AfterLogin...)
	hooks.AfterMe = append([]ridu.AuthHook(nil), hooks.AfterMe...)
	hooks.BeforeLogout = append([]ridu.AuthHook(nil), hooks.BeforeLogout...)
	hooks.AfterLogout = append([]ridu.AuthHook(nil), hooks.AfterLogout...)
	hooks.BeforeRefresh = append([]ridu.AuthHook(nil), hooks.BeforeRefresh...)
	hooks.AfterRefresh = append([]ridu.AuthHook(nil), hooks.AfterRefresh...)
	hooks.BeforeForgotPassword = append([]ridu.AuthHook(nil), hooks.BeforeForgotPassword...)
	hooks.AfterForgotPassword = append([]ridu.AuthHook(nil), hooks.AfterForgotPassword...)
	hooks.BeforePasswordReset = append([]ridu.AuthHook(nil), hooks.BeforePasswordReset...)
	hooks.AfterPasswordReset = append([]ridu.AuthHook(nil), hooks.AfterPasswordReset...)
	hooks.BeforeVerification = append([]ridu.AuthHook(nil), hooks.BeforeVerification...)
	hooks.AfterVerification = append([]ridu.AuthHook(nil), hooks.AfterVerification...)
	hooks.BeforeAPIKey = append([]ridu.AuthHook(nil), hooks.BeforeAPIKey...)
	hooks.AfterAPIKey = append([]ridu.AuthHook(nil), hooks.AfterAPIKey...)
	return hooks
}

func cloneGlobal(global ridu.Global) ridu.Global {
	cloned := global
	cloned.Fields = global.Fields.Snapshot()
	cloned.LabelTranslations = cloneMap(global.LabelTranslations)
	cloned.Admin.GroupTranslations = cloneMap(global.Admin.GroupTranslations)
	cloned.Admin.DescriptionTranslations = cloneMap(global.Admin.DescriptionTranslations)
	cloned.Admin.LivePreview = cloneLivePreview(global.Admin.LivePreview)
	cloned.Hooks = cloneHooks(global.Hooks)
	return cloned
}

func cloneLivePreview(preview ridu.LivePreviewConfig) ridu.LivePreviewConfig {
	preview.Breakpoints = append([]ridu.PreviewBreakpoint(nil), preview.Breakpoints...)
	for index := range preview.Breakpoints {
		preview.Breakpoints[index].LabelTranslations = cloneMap(preview.Breakpoints[index].LabelTranslations)
	}
	return preview
}

func cloneMap[Value any](source map[string]Value) map[string]Value {
	if source == nil {
		return nil
	}
	cloned := make(map[string]Value, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneHooks(hooks ridu.CollectionHooks) ridu.CollectionHooks {
	hooks.BeforeDuplicate = append([]ridu.Hook(nil), hooks.BeforeDuplicate...)
	hooks.BeforeValidate = append([]ridu.Hook(nil), hooks.BeforeValidate...)
	hooks.BeforeChange = append([]ridu.Hook(nil), hooks.BeforeChange...)
	hooks.BeforeOperation = append([]ridu.Hook(nil), hooks.BeforeOperation...)
	hooks.BeforeRead = append([]ridu.Hook(nil), hooks.BeforeRead...)
	hooks.BeforeDelete = append([]ridu.Hook(nil), hooks.BeforeDelete...)
	hooks.AfterChange = append([]ridu.Hook(nil), hooks.AfterChange...)
	hooks.AfterRead = append([]ridu.Hook(nil), hooks.AfterRead...)
	hooks.AfterDelete = append([]ridu.Hook(nil), hooks.AfterDelete...)
	hooks.AfterOperation = append([]ridu.Hook(nil), hooks.AfterOperation...)
	hooks.AfterError = append([]ridu.Hook(nil), hooks.AfterError...)
	hooks.AfterCommit = append([]ridu.Hook(nil), hooks.AfterCommit...)
	return hooks
}

func adminComponent(component string, config any) field.ComponentRef {
	encoded, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	var value store.Value
	if err := json.Unmarshal(encoded, &value); err != nil {
		panic(err)
	}
	return field.PluginComponent(Key, component, value)
}

var (
	_ ridu.ConfigTransformer  = (*Plugin)(nil)
	_ ridu.DescriptorProvider = (*Plugin)(nil)
	_ ridu.EndpointProvider   = (*Plugin)(nil)
)
