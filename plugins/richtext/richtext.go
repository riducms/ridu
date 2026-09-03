// Package richtext provides Ridu's official versioned rich-text field plugin.
package richtext

import (
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

// Document is the serialized Lexical document stored by the rich-text field.
// Node shapes remain extensible and are validated against the field's enabled
// feature configuration at runtime.
// Document is the exact generated Go value type for a versioned rich-text document.
type Document map[string]any

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
			Key: Key, TypeScriptPackage: "@riducms/plugin-richtext",
			TypeScriptOutput: "RichTextDocument", TypeScriptInput: "RichTextDocument",
			GoPackage: "github.com/riducms/ridu/plugins/richtext", GoType: "Document",
			JSONSchema: []byte(`{"type":"object","required":["version","root"],"properties":{"version":{"const":1},"root":{"type":"object"}},"additionalProperties":false}`),
		}},
	}
}
func (plugin) FieldValidators() map[string]ridu.PluginFieldValidator {
	return map[string]ridu.PluginFieldValidator{Key: validate}
}

// Field constructs a rich-text field with Ridu's recommended authoring defaults.
func Field(name string, options ...field.PluginOption) field.Definition {
	return FieldWithConfig(name, DefaultConfig(), options...)
}

// DefaultConfig returns a defensive copy of Ridu's recommended authoring defaults.
func DefaultConfig() Config {
	return Config{Features: append([]Feature(nil), defaultFeatures...)}
}

func FieldWithConfig(name string, config Config, options ...field.PluginOption) field.Definition {
	if config.Features == nil {
		config.Features = DefaultConfig().Features
	}
	config.Features = normalizedFeatures(config.Features)
	config.UploadCollections = normalizedStrings(config.UploadCollections)
	config.RelationshipCollections = normalizedStrings(config.RelationshipCollections)
	encoded, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	return field.Plugin(name, Key, encoded, append([]field.PluginOption{field.CollectionReferenceKeys("relationTo")}, options...)...)
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
	document, valid := ctx.Value.ObjectValue()
	if !valid {
		return []schema.Issue{{Code: "invalid_richtext", Path: path, Message: "rich-text value must be an object"}}
	}
	version, valid := document["version"].NumberValue()
	if !valid || version != DocumentVersion {
		return []schema.Issue{{Code: "unsupported_richtext_version", Path: path + ".version", Message: fmt.Sprintf("rich-text document version must be %d", DocumentVersion)}}
	}
	root, valid := document["root"].ObjectValue()
	if !valid {
		return []schema.Issue{{Code: "invalid_richtext_root", Path: path + ".root", Message: "rich-text root must be an object"}}
	}
	features := make(map[Feature]bool, len(config.Features))
	for _, feature := range config.Features {
		features[feature] = true
	}
	count := 0
	return validateNode(root, path+".root", config, features, 0, &count)
}

func validateNode(node store.Values, path string, config Config, features map[Feature]bool, depth int, count *int) []schema.Issue {
	*count++
	if depth > 64 || *count > 10_000 {
		return []schema.Issue{{Code: "richtext_too_complex", Path: path, Message: "rich-text document exceeds its complexity limit"}}
	}
	nodeType, valid := node["type"].StringValue()
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
	if nodeType == "text" {
		if _, valid := node["text"].StringValue(); !valid {
			return []schema.Issue{{Code: "invalid_richtext_text", Path: path + ".text", Message: "text node content must be a string"}}
		}
		return nil
	}
	if nodeType == "link" {
		if _, valid := node["url"].StringValue(); !valid {
			return []schema.Issue{{Code: "invalid_richtext_link", Path: path + ".url", Message: "link node URL must be a string"}}
		}
	}
	if nodeType == "upload" || nodeType == "relationship" {
		relationTo, valid := node["relationTo"].StringValue()
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
		if _, valid := node["id"].StringValue(); !valid {
			return []schema.Issue{{Code: "invalid_richtext_reference", Path: path + ".id", Message: "reference node ID is required"}}
		}
		if caption, exists := node["caption"]; nodeType == "upload" && exists {
			if _, valid := caption.StringValue(); !valid {
				return []schema.Issue{{Code: "invalid_richtext_upload_caption", Path: path + ".caption", Message: "upload caption must be a string"}}
			}
		}
	}
	if nodeType == "block" {
		if _, valid := node["blockType"].StringValue(); !valid {
			return []schema.Issue{{Code: "invalid_richtext_block", Path: path + ".blockType", Message: "block node type is required"}}
		}
	}
	children, valid := node["children"].Values()
	if !valid && (nodeType == "root" || nodeType == "paragraph" || nodeType == "heading" || nodeType == "quote" || nodeType == "link" || nodeType == "list" || nodeType == "listitem" || nodeType == "code") {
		return []schema.Issue{{Code: "invalid_richtext_children", Path: path + ".children", Message: "rich-text node children must be an array"}}
	}
	var issues []schema.Issue
	for index, child := range children {
		object, valid := child.ObjectValue()
		if !valid {
			issues = append(issues, schema.Issue{Code: "invalid_richtext_node", Path: fmt.Sprintf("%s.children.%d", path, index), Message: "rich-text child must be an object"})
			continue
		}
		issues = append(issues, validateNode(object, fmt.Sprintf("%s.children.%d", path, index), config, features, depth+1, count)...)
	}
	return issues
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
	document, valid := value.ObjectValue()
	if !valid {
		return "", fmt.Errorf("rich-text value must be an object")
	}
	root, valid := document["root"].ObjectValue()
	if !valid {
		return "", fmt.Errorf("rich-text root must be an object")
	}
	return renderNode(root, renderers)
}

func renderNode(node store.Values, renderers map[string]func(store.Values) (string, error)) (string, error) {
	nodeType, _ := node["type"].StringValue()
	if renderer := renderers[nodeType]; renderer != nil {
		return renderer(node)
	}
	if nodeType == "text" {
		text, _ := node["text"].StringValue()
		rendered := html.EscapeString(text)
		format, _ := node["format"].NumberValue()
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
	children, _ := node["children"].Values()
	var output strings.Builder
	for _, child := range children {
		object, valid := child.ObjectValue()
		if !valid {
			continue
		}
		rendered, err := renderNode(object, renderers)
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
		tag, _ := node["tag"].StringValue()
		if tag != "h1" && tag != "h2" && tag != "h3" && tag != "h4" && tag != "h5" && tag != "h6" {
			tag = "h2"
		}
		return "<" + tag + renderElementAttributes(node) + ">" + output.String() + "</" + tag + ">", nil
	case "quote":
		return "<blockquote" + renderElementAttributes(node) + ">" + output.String() + "</blockquote>", nil
	case "list":
		listType, _ := node["listType"].StringValue()
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
		if checked, valid := node["checked"].BooleanValue(); valid {
			attribute = fmt.Sprintf(` data-checked="%t"`, checked)
		}
		return "<li" + attribute + ">" + output.String() + "</li>", nil
	case "code":
		return "<pre><code>" + output.String() + "</code></pre>", nil
	case "link":
		url, _ := node["url"].StringValue()
		if !safeLink(url) {
			return "", fmt.Errorf("unsafe rich-text link URL")
		}
		return `<a href="` + html.EscapeString(url) + `">` + output.String() + "</a>", nil
	default:
		return "", fmt.Errorf("no renderer registered for rich-text node %q", nodeType)
	}
}

func renderElementAttributes(node store.Values) string {
	var attributes strings.Builder
	if alignment, valid := node["format"].StringValue(); valid {
		switch alignment {
		case "center", "right", "justify", "start", "end":
			attributes.WriteString(` data-align="` + alignment + `"`)
		}
	}
	if indent, valid := node["indent"].NumberValue(); valid && indent > 0 {
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
