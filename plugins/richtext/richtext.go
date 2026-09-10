// Package richtext provides Ridu's official versioned rich-text field plugin.
package richtext

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"sort"
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const (
	Key                       = "richtext"
	DocumentVersion           = 1
	AdminPluginPairingVersion = 1
)

type Feature string

const (
	FeatureLinks          Feature = "links"
	FeatureLists          Feature = "lists"
	FeatureCode           Feature = "code"
	FeatureHorizontalRule Feature = "horizontal-rule"
	FeatureUploads        Feature = "uploads"
	FeatureRelationships  Feature = "relationships"
	FeatureBlocks         Feature = "blocks"
)

type Config struct {
	// Blocks is the fixed executable schema allowlist. Fields are resolved
	// through the public embedded-fields contract, never serialized as settings.
	Blocks []field.Block `json:"-"`
	// BlockReferences selects Config.Blocks definitions; it is exclusive with Blocks.
	BlockReferences         []string  `json:"-"`
	Features                []Feature `json:"features"`
	UploadCollections       []string  `json:"uploadCollections,omitempty"`
	RelationshipCollections []string  `json:"relationshipCollections,omitempty"`
}

var defaultFeatures = []Feature{
	FeatureLinks,
	FeatureLists,
	FeatureCode,
	FeatureHorizontalRule,
	FeatureUploads,
	FeatureRelationships,
}

//go:embed document-schema.json
var documentSchema []byte

type plugin struct{}

func New() ridu.Plugin     { return plugin{} }
func (plugin) Key() string { return Key }
func (plugin) Descriptor() ridu.PluginDescriptor {
	admin := ridu.AdminPluginMetadata{
		Package: "@riducms/plugin-richtext", Export: "richTextAdminPlugin",
		APIVersion: ridu.AdminPluginAPIVersion, PairingVersion: AdminPluginPairingVersion,
	}
	return ridu.PluginDescriptor{
		Version:    ridu.FrameworkVersion,
		GoPackage:  "github.com/riducms/ridu/plugins/richtext",
		APIVersion: ridu.PluginAPIVersion,
		Ridu:       ridu.RiduCompatibility{Minimum: ridu.FrameworkVersion},
		Admin:      &admin,
		FieldTypes: []ridu.PluginFieldType{{
			Key: Key, TypeScriptPackage: "@riducms/plugin-richtext/document",
			TypeScriptOutput: "RichTextDocument", TypeScriptInput: "RichTextDocumentInput",
			EmbeddedTypes: []string{"blocks.block"},
			GoPackage:     "github.com/riducms/ridu/plugins/richtext", GoType: "Document",
			JSONSchema: append([]byte(nil), documentSchema...),
		}},
	}
}
func (plugin) FieldValidators() map[string]ridu.PluginFieldValidator {
	return map[string]ridu.PluginFieldValidator{Key: validate}
}

// DefaultConfig returns a defensive copy of Ridu's recommended authoring defaults.
func DefaultConfig() Config {
	return Config{Features: append([]Feature(nil), defaultFeatures...)}
}

// Field constructs a rich-text field with optional configuration. With no Config,
// it uses the recommended defaults. A supplied Config with omitted Features also
// inherits those defaults; an explicitly empty Features slice disables them.
// Embedded block fields retain their attached behavior and presentation.
// Field panics if more than one Config is supplied.
func Field(name string, configs ...Config) field.PluginField {
	if len(configs) > 1 {
		panic(fmt.Sprintf("richtext.Field(%q): expected at most one Config, got %d", name, len(configs)))
	}
	var config Config
	if len(configs) == 1 {
		config = configs[0]
	}
	if config.Features == nil {
		config.Features = DefaultConfig().Features
		if len(config.Blocks) > 0 || len(config.BlockReferences) > 0 {
			config.Features = append(config.Features, FeatureBlocks)
		}
	}
	config.Features = normalizedFeatures(config.Features)
	config.UploadCollections = normalizedStrings(config.UploadCollections)
	config.RelationshipCollections = normalizedStrings(config.RelationshipCollections)
	encoded, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	return field.Plugin(name, Key, encoded).
		CollectionReferenceKeys("relationTo").
		EmbeddedTrees(field.EmbeddedTree{
			Key: "blocks", Root: []string{"root"}, Children: "children", Tag: "type",
			Cases: []field.EmbeddedTreeCase{{TagValue: "block", Payload: "fields", Discriminator: "blockType", Identity: "_key", Types: config.Blocks, BlockReferences: config.BlockReferences}},
		})
}

func normalizedFeatures(features []Feature) []Feature {
	seen := make(map[Feature]struct{}, len(features))
	result := make([]Feature, 0, len(features))
	for _, feature := range features {
		if _, exists := seen[feature]; exists {
			continue
		}
		seen[feature] = struct{}{}
		result = append(result, feature)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

func normalizedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func validate(ctx ridu.PluginFieldValidationContext) []schema.Issue {
	path := ctx.RuntimePath
	if path == "" {
		path = ctx.Field.Path.String()
	}
	config := Config{}
	if ctx.Field.Plugin == nil || json.Unmarshal(ctx.Field.Plugin.Config, &config) != nil {
		return []schema.Issue{{Code: "invalid_richtext_config", Path: path, Message: "rich-text field configuration is invalid"}}
	}
	document := ctx.Value
	if document.Kind() != store.ValueObject {
		return []schema.Issue{{Code: "invalid_richtext", Path: path, Message: "rich-text value must be an object"}}
	}
	version, valid := document.Get("version").NumberValue()
	if !valid || version != DocumentVersion {
		return []schema.Issue{{Code: "unsupported_richtext_version", Path: path + ".version", Message: fmt.Sprintf("rich-text document version must be %d", DocumentVersion)}}
	}
	root := document.Get("root")
	if root.Kind() != store.ValueObject {
		return []schema.Issue{{Code: "invalid_richtext_root", Path: path + ".root", Message: "rich-text root must be an object"}}
	}
	if rootType, _ := root.Get("type").StringValue(); rootType != "root" {
		return []schema.Issue{{Code: "invalid_richtext_root", Path: path + ".root.type", Message: "rich-text document root must have type root"}}
	}
	for _, key := range sortedValueKeys(document) {
		if key != "version" && key != "root" {
			return []schema.Issue{{Code: "invalid_richtext_property", Path: path + "." + key, Message: "unsupported document property; retain the original JSON for export or migration"}}
		}
	}
	features := make(map[Feature]bool, len(config.Features))
	for _, feature := range config.Features {
		features[feature] = true
	}
	count := 0
	return validateNode(root, path+".root", config, features, 0, &count)
}

func validateNode(node store.Value, path string, config Config, features map[Feature]bool, depth int, count *int) []schema.Issue {
	*count++
	if depth > 64 || *count > 10_000 {
		return []schema.Issue{{Code: "richtext_too_complex", Path: path, Message: "rich-text document exceeds its complexity limit"}}
	}
	nodeType, valid := node.Get("type").StringValue()
	if !valid {
		return []schema.Issue{{Code: "invalid_richtext_node", Path: path + ".type", Message: "rich-text node type is required"}}
	}
	allowed := nodeType == "root" || nodeType == "paragraph" || nodeType == "heading" || nodeType == "quote" || nodeType == "text" || nodeType == "linebreak" ||
		nodeType == "link" && features[FeatureLinks] ||
		(nodeType == "list" || nodeType == "listitem") && features[FeatureLists] ||
		nodeType == "code" && features[FeatureCode] || nodeType == "horizontalrule" && features[FeatureHorizontalRule] ||
		nodeType == "upload" && features[FeatureUploads] ||
		nodeType == "relationship" && features[FeatureRelationships] || nodeType == "block" && features[FeatureBlocks]
	if !allowed {
		return []schema.Issue{{Code: "unsupported_richtext_node", Path: path + ".type", Message: fmt.Sprintf("rich-text node %q is not enabled", nodeType)}}
	}
	if nodeType == "root" && depth != 0 {
		return []schema.Issue{{Code: "invalid_richtext_root", Path: path + ".type", Message: "root nodes cannot be nested inside a rich-text document"}}
	}
	if nodeType != "block" {
		if version, exists := node.Lookup("version"); exists {
			if number, valid := version.NumberValue(); !valid || number != DocumentVersion {
				return []schema.Issue{{Code: "unsupported_richtext_node_version", Path: path + ".version", Message: "rich-text node version must be 1 when provided"}}
			}
		}
		if issues := validateNodeProperties(node, nodeType, path); len(issues) > 0 {
			return issues
		}
	}
	if nodeType == "text" {
		if _, valid := node.Get("text").StringValue(); !valid {
			return []schema.Issue{{Code: "invalid_richtext_text", Path: path + ".text", Message: "text node content must be a string"}}
		}
		return nil
	}
	if nodeType == "heading" {
		tag, _ := node.Get("tag").StringValue()
		if !containsString([]string{"h1", "h2", "h3", "h4", "h5", "h6"}, tag) {
			return []schema.Issue{{Code: "invalid_richtext_heading", Path: path + ".tag", Message: "heading tag must be h1 through h6"}}
		}
	}
	if nodeType == "list" {
		listType, _ := node.Get("listType").StringValue()
		if !containsString([]string{"bullet", "number", "check"}, listType) {
			return []schema.Issue{{Code: "invalid_richtext_list", Path: path + ".listType", Message: "list type must be bullet, number or check"}}
		}
	}
	if nodeType == "link" {
		if _, valid := node.Get("url").StringValue(); !valid {
			return []schema.Issue{{Code: "invalid_richtext_link", Path: path + ".url", Message: "link node URL must be a string"}}
		}
	}
	if nodeType == "upload" || nodeType == "relationship" {
		relationTo, valid := node.Get("relationTo").StringValue()
		if !valid {
			return []schema.Issue{{Code: "invalid_richtext_reference", Path: path + ".relationTo", Message: "reference node collection is required"}}
		}
		allowedCollections := config.RelationshipCollections
		kind := "relationship"
		if nodeType == "upload" {
			allowedCollections = config.UploadCollections
			kind = "upload"
		}
		if len(allowedCollections) > 0 && !containsString(allowedCollections, relationTo) {
			return []schema.Issue{{Code: "invalid_richtext_reference", Path: path + ".relationTo", Message: fmt.Sprintf("%s collection %q is not enabled", kind, relationTo)}}
		}
		if _, valid := node.Get("id").StringValue(); !valid {
			return []schema.Issue{{Code: "invalid_richtext_reference", Path: path + ".id", Message: "reference node ID is required"}}
		}
		if caption, exists := node.Lookup("caption"); nodeType == "upload" && exists {
			if _, valid := caption.StringValue(); !valid {
				return []schema.Issue{{Code: "invalid_richtext_upload_caption", Path: path + ".caption", Message: "upload caption must be a string"}}
			}
		}
	}
	if nodeType == "block" {
		// Ordinary traversal owns payload shape, defaults, validation, access and
		// identity. The plugin validates only its atomic editor envelope.
		var issues []schema.Issue
		version, ok := node.Get("version").NumberValue()
		if !ok || version != DocumentVersion {
			issues = append(issues, schema.Issue{Code: "unsupported_richtext_block_version", Path: path + ".version", Message: "block node version must be 1"})
		}
		for _, key := range sortedValueKeys(node) {
			if key != "type" && key != "version" && key != "fields" {
				issues = append(issues, schema.Issue{Code: "invalid_richtext_block", Path: path + "." + key, Message: "block nodes contain only type, version and fields; migrate experimental top-level payloads explicitly"})
			}
		}
		return issues
	}
	children := node.Get("children")
	if children.Kind() != store.ValueList && (nodeType == "root" || nodeType == "paragraph" || nodeType == "heading" || nodeType == "quote" || nodeType == "link" || nodeType == "list" || nodeType == "listitem" || nodeType == "code") {
		return []schema.Issue{{Code: "invalid_richtext_children", Path: path + ".children", Message: "rich-text node children must be an array"}}
	}
	var issues []schema.Issue
	index := 0
	for child := range children.Elements() {
		childPath := fmt.Sprintf("%s.children.%d", path, index)
		index++
		if child.Kind() != store.ValueObject {
			issues = append(issues, schema.Issue{Code: "invalid_richtext_node", Path: childPath, Message: "rich-text child must be an object"})
			continue
		}
		issues = append(issues, validateNode(child, childPath, config, features, depth+1, count)...)
	}
	return issues
}

// Keep the admitted editor envelope within the portable Node JSON vocabulary.
// Payload fields are deliberately excluded: ordinary embedded traversal owns them.
func validateNodeProperties(node store.Value, nodeType, path string) []schema.Issue {
	var issues []schema.Issue
	for _, key := range sortedValueKeys(node) {
		if !nodePropertyAllowed(nodeType, key) {
			issues = append(issues, schema.Issue{Code: "invalid_richtext_property", Path: path + "." + key, Message: "unsupported node property; retain the original JSON for export or migration"})
			continue
		}
		value := node.Get(key)
		valid := true
		switch key {
		case "type", "version":
			continue // Required type and optional version are checked by the caller.
		case "children":
			valid = value.Kind() == store.ValueList && isContainerNode(nodeType)
		case "text", "style", "textStyle", "tag", "url", "listType", "language", "theme", "relationTo", "id", "caption":
			_, valid = value.StringValue()
		case "mode":
			mode, ok := value.StringValue()
			valid = ok && containsString([]string{"normal", "token", "segmented"}, mode)
		case "direction":
			direction, ok := value.StringValue()
			valid = value.Kind() == store.ValueNull || ok && (direction == "ltr" || direction == "rtl")
		case "target", "rel", "title":
			_, valid = value.StringValue()
			valid = valid || value.Kind() == store.ValueNull
		case "detail", "textFormat", "indent", "start", "value":
			number, ok := value.NumberValue()
			valid = ok && number == float64(int64(number))
		case "checked":
			_, valid = value.BooleanValue()
		case "format":
			if nodeType == "text" {
				number, ok := value.NumberValue()
				valid = ok && number == float64(int64(number))
			} else {
				format, ok := value.StringValue()
				valid = ok && containsString([]string{"", "left", "center", "right", "justify", "start", "end"}, format)
			}
		default:
			issues = append(issues, schema.Issue{Code: "invalid_richtext_property", Path: path + "." + key, Message: "unsupported node property; retain the original JSON for export or migration"})
			continue
		}
		if !valid {
			issues = append(issues, schema.Issue{Code: "invalid_richtext_property", Path: path + "." + key, Message: "rich-text node property does not match its portable JSON contract"})
		}
	}
	return issues
}

func nodePropertyAllowed(nodeType, key string) bool {
	if key == "type" || key == "version" {
		return true
	}
	if isContainerNode(nodeType) {
		switch key {
		case "children", "direction", "format", "indent":
			return true
		case "textFormat", "textStyle":
			return nodeType != "root"
		}
	}
	switch nodeType {
	case "text":
		return key == "text" || key == "format" || key == "detail" || key == "mode" || key == "style"
	case "heading":
		return key == "tag"
	case "link":
		return key == "url" || key == "target" || key == "rel" || key == "title"
	case "list":
		return key == "listType" || key == "start" || key == "tag"
	case "listitem":
		return key == "value" || key == "checked"
	case "code":
		return key == "language" || key == "theme"
	case "upload", "relationship":
		return key == "relationTo" || key == "id" || key == "format" || nodeType == "upload" && key == "caption"
	case "block":
		return key == "fields"
	default:
		return false
	}
}

func isContainerNode(nodeType string) bool {
	switch nodeType {
	case "root", "paragraph", "heading", "quote", "link", "list", "listitem", "code":
		return true
	default:
		return false
	}
}

func sortedValueKeys(values store.Value) []string {
	keys := make([]string, 0, values.Len())
	for key := range values.Entries() {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

// RenderHTML renders the portable core nodes. Custom feature nodes use a
// caller-owned renderer, keeping application presentation outside storage.
func RenderHTML(value store.Value, renderers map[string]func(store.Values) (string, error)) (string, error) {
	document := value
	if document.Kind() != store.ValueObject {
		return "", fmt.Errorf("rich-text value must be an object")
	}
	root := document.Get("root")
	if root.Kind() != store.ValueObject {
		return "", fmt.Errorf("rich-text root must be an object")
	}
	count := 0
	return renderNode(root, renderers, 0, &count)
}

func renderNode(node store.Value, renderers map[string]func(store.Values) (string, error), depth int, count *int) (string, error) {
	*count++
	if depth > 64 || *count > 10_000 {
		return "", fmt.Errorf("rich-text rendering exceeds its depth or node budget")
	}
	nodeType, _ := node.Get("type").StringValue()
	if renderer := renderers[nodeType]; renderer != nil {
		detached, _ := node.CopyObject()
		return renderer(detached)
	}
	if nodeType == "text" {
		text, _ := node.Get("text").StringValue()
		rendered := html.EscapeString(text)
		format, _ := node.Get("format").NumberValue()
		for _, wrapper := range []struct {
			mask float64
			tag  string
		}{
			{mask: 64, tag: "sup"},
			{mask: 32, tag: "sub"},
			{mask: 16, tag: "code"},
			{mask: 8, tag: "u"},
			{mask: 4, tag: "s"},
			{mask: 2, tag: "em"},
			{mask: 1, tag: "strong"},
		} {
			if int(format)&int(wrapper.mask) != 0 {
				rendered = "<" + wrapper.tag + ">" + rendered + "</" + wrapper.tag + ">"
			}
		}
		return rendered, nil
	}
	if nodeType == "linebreak" {
		return "<br>", nil
	}
	if nodeType == "horizontalrule" {
		return "<hr>", nil
	}
	children := node.Get("children")
	var output strings.Builder
	for child := range children.Elements() {
		if child.Kind() != store.ValueObject {
			return "", fmt.Errorf("rich-text child must be an object")
		}
		rendered, err := renderNode(child, renderers, depth+1, count)
		if err != nil {
			return "", err
		}
		output.WriteString(rendered)
	}
	switch nodeType {
	case "root":
		return output.String(), nil
	case "paragraph":
		return "<p" + renderElementAttributes(node) + ">" + output.String() + "</p>", nil
	case "heading":
		tag, _ := node.Get("tag").StringValue()
		if tag != "h1" && tag != "h2" && tag != "h3" && tag != "h4" && tag != "h5" && tag != "h6" {
			tag = "h2"
		}
		return "<" + tag + renderElementAttributes(node) + ">" + output.String() + "</" + tag + ">", nil
	case "quote":
		return "<blockquote" + renderElementAttributes(node) + ">" + output.String() + "</blockquote>", nil
	case "list":
		listType, _ := node.Get("listType").StringValue()
		tag := "ul"
		if listType == "number" {
			tag = "ol"
		}
		attribute := ""
		if listType == "check" {
			attribute = ` data-list-type="check"`
		}
		return "<" + tag + attribute + renderElementAttributes(node) + ">" + output.String() + "</" + tag + ">", nil
	case "listitem":
		attribute := ""
		if checked, valid := node.Get("checked").BooleanValue(); valid {
			attribute = fmt.Sprintf(` data-checked="%t"`, checked)
		}
		return "<li" + attribute + ">" + output.String() + "</li>", nil
	case "code":
		return "<pre><code>" + output.String() + "</code></pre>", nil
	case "link":
		url, _ := node.Get("url").StringValue()
		if !safeLink(url) {
			return "", fmt.Errorf("unsafe rich-text link URL")
		}
		return `<a href="` + html.EscapeString(url) + `">` + output.String() + "</a>", nil
	default:
		return "", fmt.Errorf("no renderer registered for rich-text node %q", nodeType)
	}
}

func renderElementAttributes(node store.Value) string {
	var attributes strings.Builder
	if alignment, valid := node.Get("format").StringValue(); valid {
		switch alignment {
		case "center", "right", "justify", "start", "end":
			attributes.WriteString(` data-align="` + alignment + `"`)
		}
	}
	if indent, valid := node.Get("indent").NumberValue(); valid && indent > 0 {
		if indent > 8 {
			indent = 8
		}
		attributes.WriteString(fmt.Sprintf(` data-indent="%d"`, int(indent)))
	}
	return attributes.String()
}

func safeLink(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if parsed.Scheme == "" {
		return !strings.HasPrefix(strings.TrimSpace(raw), "//")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "mailto", "tel":
		return true
	default:
		return false
	}
}

var _ ridu.FieldValidatorProvider = plugin{}
var _ ridu.DescriptorProvider = plugin{}
