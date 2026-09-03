package field

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Option is the sealed base contract implemented by every field option.
// Constructors accept narrower interfaces so incompatible options fail at
// compile time.
type Option interface{ apply(*builder) }

// StringOption can be passed to text, textarea, email, and date fields.
type StringOption interface {
	Option
	stringOption()
}

// NumberOption can be passed to number fields.
type NumberOption interface {
	Option
	numberOption()
}

// CheckboxOption can be passed to checkbox fields.
type CheckboxOption interface {
	Option
	checkboxOption()
}

// JSONOption can be passed to JSON fields.
type JSONOption interface {
	Option
	jsonOption()
}

// SelectOption can be passed to select fields.
type SelectOption interface {
	Option
	selectOption()
}

// RelationshipOption can be passed to relationship fields.
type RelationshipOption interface {
	Option
	relationshipOption()
}

// UploadOption can be passed to upload-reference fields.
type UploadOption interface {
	Option
	uploadOption()
}

// GroupOption can be passed to nested group fields.
type GroupOption interface {
	Option
	groupOption()
}

// ArrayOption can be passed to repeatable array fields.
type ArrayOption interface {
	Option
	arrayOption()
}

// BlocksOption can be passed to discriminated blocks fields.
type BlocksOption interface {
	Option
	blocksOption()
}

// RowLabelComponentOption selects a custom row-label renderer for repeatable
// array and blocks fields.
type RowLabelComponentOption interface {
	ArrayOption
	BlocksOption
}

// JoinOption can be passed to inverse join fields.
type JoinOption interface {
	Option
	joinOption()
}

// PluginOption can be passed to fields supplied by compiled plugins.
type PluginOption interface {
	Option
	pluginOption()
}

// CommonOption applies to every built-in and plugin field kind.
type CommonOption interface {
	StringOption
	NumberOption
	CheckboxOption
	JSONOption
	SelectOption
	RelationshipOption
	UploadOption
	GroupOption
	ArrayOption
	BlocksOption
	JoinOption
	PluginOption
}

// LocalizedOption applies to every stored field kind. Presentation-only join,
// UI, and virtual fields cannot own localized values.
type LocalizedOption interface {
	StringOption
	NumberOption
	CheckboxOption
	JSONOption
	SelectOption
	RelationshipOption
	UploadOption
	GroupOption
	ArrayOption
	BlocksOption
	PluginOption
}

// UniqueOption applies to field kinds with supported scalar/reference indexes.
type UniqueOption interface {
	StringOption
	NumberOption
	CheckboxOption
	SelectOption
	RelationshipOption
	UploadOption
}

// DefaultOption applies to fields with a concrete scalar default value.
type DefaultOption interface {
	StringOption
	NumberOption
	CheckboxOption
	SelectOption
}

// RelationshipOrUploadOption configures reference fields.
type RelationshipOrUploadOption interface {
	RelationshipOption
	UploadOption
}

// NestedOption configures group and array children.
type NestedOption interface {
	GroupOption
	ArrayOption
}

type commonOptionFunc func(*builder)

func (option commonOptionFunc) apply(builder *builder) { option(builder) }
func (commonOptionFunc) stringOption()                 {}
func (commonOptionFunc) numberOption()                 {}
func (commonOptionFunc) checkboxOption()               {}
func (commonOptionFunc) jsonOption()                   {}
func (commonOptionFunc) selectOption()                 {}
func (commonOptionFunc) relationshipOption()           {}
func (commonOptionFunc) uploadOption()                 {}
func (commonOptionFunc) groupOption()                  {}
func (commonOptionFunc) arrayOption()                  {}
func (commonOptionFunc) blocksOption()                 {}
func (commonOptionFunc) joinOption()                   {}
func (commonOptionFunc) pluginOption()                 {}

type uniqueOptionFunc func(*builder)

func (option uniqueOptionFunc) apply(builder *builder) { option(builder) }
func (uniqueOptionFunc) stringOption()                 {}
func (uniqueOptionFunc) numberOption()                 {}
func (uniqueOptionFunc) checkboxOption()               {}
func (uniqueOptionFunc) selectOption()                 {}
func (uniqueOptionFunc) relationshipOption()           {}
func (uniqueOptionFunc) uploadOption()                 {}

type localizedOptionFunc func(*builder)

func (option localizedOptionFunc) apply(builder *builder) { option(builder) }
func (localizedOptionFunc) stringOption()                 {}
func (localizedOptionFunc) numberOption()                 {}
func (localizedOptionFunc) checkboxOption()               {}
func (localizedOptionFunc) jsonOption()                   {}
func (localizedOptionFunc) selectOption()                 {}
func (localizedOptionFunc) relationshipOption()           {}
func (localizedOptionFunc) uploadOption()                 {}
func (localizedOptionFunc) groupOption()                  {}
func (localizedOptionFunc) arrayOption()                  {}
func (localizedOptionFunc) blocksOption()                 {}
func (localizedOptionFunc) pluginOption()                 {}

type defaultOption struct {
	value DefaultValue
	err   error
}

func (option defaultOption) apply(builder *builder) {
	if builder.defaultSet {
		builder.issue("duplicate_option", "options.default", "field default was configured more than once")
	}
	builder.defaultSet = true
	if option.err != nil {
		builder.issue("invalid_default", "options.default", option.err.Error())
		return
	}
	value := option.value
	builder.definition.defaultValue = &value
}
func (defaultOption) stringOption()   {}
func (defaultOption) numberOption()   {}
func (defaultOption) checkboxOption() {}
func (defaultOption) selectOption()   {}

type selectOptionFunc func(*builder)

func (option selectOptionFunc) apply(builder *builder) { option(builder) }
func (selectOptionFunc) selectOption()                 {}

type relationshipOptionFunc func(*builder)

func (option relationshipOptionFunc) apply(builder *builder) { option(builder) }
func (relationshipOptionFunc) relationshipOption()           {}

type referenceOptionFunc func(*builder)

func (option referenceOptionFunc) apply(builder *builder) { option(builder) }
func (referenceOptionFunc) relationshipOption()           {}
func (referenceOptionFunc) uploadOption()                 {}

type nestedOptionFunc func(*builder)

func (option nestedOptionFunc) apply(builder *builder) { option(builder) }
func (nestedOptionFunc) groupOption()                  {}
func (nestedOptionFunc) arrayOption()                  {}

type blocksOptionFunc func(*builder)

func (option blocksOptionFunc) apply(builder *builder) { option(builder) }
func (blocksOptionFunc) blocksOption()                 {}

type rowLabelComponentOptionFunc func(*builder)

func (option rowLabelComponentOptionFunc) apply(builder *builder) { option(builder) }
func (rowLabelComponentOptionFunc) arrayOption()                  {}
func (rowLabelComponentOptionFunc) blocksOption()                 {}

type joinOptionFunc func(*builder)

func (option joinOptionFunc) apply(builder *builder) { option(builder) }
func (joinOptionFunc) joinOption()                   {}

type pluginOptionFunc func(*builder)

func (option pluginOptionFunc) apply(builder *builder) { option(builder) }
func (pluginOptionFunc) pluginOption()                 {}

type builder struct {
	definition Definition

	labelSet                   bool
	labelTranslationsSet       bool
	requiredSet                bool
	uniqueSet                  bool
	indexSet                   bool
	localizedSet               bool
	defaultSet                 bool
	choicesSet                 bool
	selectManySet              bool
	selectDefaultsSet          bool
	toSet                      bool
	manySet                    bool
	fieldsSet                  bool
	blocksSet                  bool
	descriptionSet             bool
	descriptionTranslationsSet bool
	placeholderSet             bool
	placeholderTranslationsSet bool
	readOnlySet                bool
	hiddenSet                  bool
	sidebarSet                 bool
	columnsSet                 bool
	tabSet                     bool
	tabTranslationsSet         bool
	conditionSet               bool
	languageSet                bool
	dateAppearanceSet          bool
	minRowsSet                 bool
	maxRowsSet                 bool
	rowLabelSet                bool
	rowLabelComponentSet       bool
	rowLabelsSet               bool
	joinLimitSet               bool
	joinColumnsSet             bool
	joinSortSet                bool
	joinAllowCreateSet         bool
	onDeleteSet                bool
	minLengthSet               bool
	maxLengthSet               bool
	minimumSet                 bool
	maximumSet                 bool
	stepSet                    bool
	adminComponentSet          bool
}

// Localized stores an independent field value for each configured content
// locale. Required and unique validation are evaluated per locale.
func Localized() LocalizedOption {
	return localizedOptionFunc(func(builder *builder) {
		if builder.localizedSet {
			builder.issue("duplicate_option", "options.localized", "field localization was configured more than once")
		}
		builder.localizedSet = true
		builder.definition.localized = true
	})
}

// Description adds supporting text below the field in authoring interfaces.
func Description(value string) CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.descriptionSet {
			builder.issue("duplicate_option", "options.description", "field description was configured more than once")
		}
		builder.descriptionSet = true
		builder.definition.description = value
	})
}

// DescriptionTranslations adds localized supporting text for admin languages.
func DescriptionTranslations(translations map[string]string) CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.descriptionTranslationsSet {
			builder.issue("duplicate_option", "options.descriptionTranslations", "field description translations were configured more than once")
		}
		builder.descriptionTranslationsSet = true
		builder.definition.descriptionTranslations = cloneTranslations(translations)
	})
}

// Placeholder configures the empty-state prompt shown by compatible admin
// controls. It is presentation metadata and does not affect stored values.
func Placeholder(value string) CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.placeholderSet {
			builder.issue("duplicate_option", "options.placeholder", "field placeholder was configured more than once")
		}
		builder.placeholderSet = true
		builder.definition.placeholder = value
	})
}

// PlaceholderTranslations localizes the placeholder for admin interface
// languages. Placeholder must provide the canonical fallback text.
func PlaceholderTranslations(translations map[string]string) CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.placeholderTranslationsSet {
			builder.issue("duplicate_option", "options.placeholderTranslations", "field placeholder translations were configured more than once")
		}
		builder.placeholderTranslationsSet = true
		builder.definition.placeholderTranslations = cloneTranslations(translations)
	})
}

// ReadOnly prevents editing in the admin; it is presentation metadata, not authorization.
func ReadOnly() CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.readOnlySet {
			builder.issue("duplicate_option", "options.readOnly", "read-only presentation was configured more than once")
		}
		builder.readOnlySet = true
		builder.definition.readOnly = true
	})
}

// Hidden removes the field from the admin renderer without changing access,
// validation, defaults, storage, or submission behavior.
func Hidden() CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.hiddenSet {
			builder.issue("duplicate_option", "options.hidden", "hidden presentation was configured more than once")
		}
		builder.hiddenSet = true
		builder.definition.hidden = true
	})
}

// Sidebar places a root field in the document editor's responsive right rail.
// Nested sidebar placement is rejected while resolving application config.
func Sidebar() CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.sidebarSet {
			builder.issue("duplicate_option", "options.sidebar", "sidebar presentation was configured more than once")
		}
		builder.sidebarSet = true
		builder.definition.sidebar = true
	})
}

// Columns assigns the field one to twelve columns in the admin's row grid.
func Columns(value int) CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.columnsSet {
			builder.issue("duplicate_option", "options.columns", "field columns were configured more than once")
		}
		builder.columnsSet = true
		builder.definition.columns = value
		if value < 1 || value > 12 {
			builder.issue("invalid_columns", "options.columns", "field columns must be between 1 and 12")
		}
	})
}

// Tab places the field in a named admin form tab.
func Tab(label string) CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.tabSet {
			builder.issue("duplicate_option", "options.tab", "field tab was configured more than once")
		}
		builder.tabSet = true
		builder.definition.tab = label
	})
}

// TabTranslations localizes the direct admin tab configured by Tab.
func TabTranslations(translations map[string]string) CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.tabTranslationsSet {
			builder.issue("duplicate_option", "options.tabTranslations", "field tab translations were configured more than once")
		}
		builder.tabTranslationsSet = true
		builder.definition.tabTranslations = cloneTranslations(translations)
	})
}

// All requires every child condition to match.
func All(conditions ...Condition) Condition {
	return Condition{kind: ConditionKindAll, conditions: cloneConditions(conditions)}
}

// Any requires at least one child condition to match.
func Any(conditions ...Condition) Condition {
	return Condition{kind: ConditionKindAny, conditions: cloneConditions(conditions)}
}

// Not negates one child condition.
func Not(condition Condition) Condition {
	return Condition{kind: ConditionKindNot, conditions: []Condition{cloneCondition(condition)}}
}

// Document compares a scalar path resolved from the document root.
func Document[Value ConditionScalar](path string, operator ConditionOperator, values ...Value) Condition {
	return conditionPredicate(ConditionScopeDocument, path, operator, values...)
}

// Sibling compares a scalar path resolved from the current field's parent object or row.
func Sibling[Value ConditionScalar](path string, operator ConditionOperator, values ...Value) Condition {
	return conditionPredicate(ConditionScopeSibling, path, operator, values...)
}

func conditionPredicate[Value ConditionScalar](scope ConditionScope, path string, operator ConditionOperator, values ...Value) Condition {
	condition := Condition{kind: ConditionKindPredicate, scope: scope, path: path, operator: operator}
	condition.values = make([]DefaultValue, len(values))
	for index, value := range values {
		normalized, err := normalizeDefault(value)
		if err != nil {
			condition.issues = append(condition.issues, Issue{
				Code: "invalid_condition_value", Path: fmt.Sprintf("values[%d]", index), Message: err.Error(),
			})
			continue
		}
		condition.values[index] = normalized
	}
	return condition
}

// ShowWhenCondition attaches one typed condition expression to a field.
// Conditions control presentation only; they never grant field or collection access.
func ShowWhenCondition(condition Condition) CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.conditionSet {
			builder.issue("duplicate_option", "options.condition", "field condition was configured more than once")
		}
		builder.conditionSet = true
		cloned := cloneCondition(condition)
		builder.definition.condition = &cloned
		nodes := 0
		validateCondition(builder, condition, "options.condition", 0, &nodes)
	})
}

// ShowWhen is the concise string-equality form of ShowWhenCondition. Its path
// is sibling-scoped, so nested fields read from their current object or row.
func ShowWhen(path, equals string) CommonOption {
	return ShowWhenCondition(Sibling(path, ConditionEquals, equals))
}

func validateCondition(builder *builder, condition Condition, path string, depth int, nodes *int) {
	const (
		maximumDepth = 32
		maximumNodes = 256
	)
	*nodes++
	if depth > maximumDepth {
		builder.issue("condition_too_deep", path, fmt.Sprintf("field condition supports at most %d nested levels", maximumDepth))
		return
	}
	if *nodes > maximumNodes {
		builder.issue("condition_too_large", path, fmt.Sprintf("field condition supports at most %d nodes", maximumNodes))
		return
	}
	for _, issue := range condition.issues {
		builder.issue(issue.Code, joinOptionPath(path, issue.Path), issue.Message)
	}
	switch condition.kind {
	case ConditionKindAll, ConditionKindAny:
		if len(condition.conditions) < 2 {
			builder.issue("invalid_condition_group", path+".conditions", "all and any conditions require at least two children")
		}
		if condition.scope != "" || condition.path != "" || condition.operator != "" || len(condition.values) != 0 {
			builder.issue("invalid_condition_shape", path, "logical conditions cannot contain predicate properties")
		}
	case ConditionKindNot:
		if len(condition.conditions) != 1 {
			builder.issue("invalid_condition_group", path+".conditions", "not conditions require exactly one child")
		}
		if condition.scope != "" || condition.path != "" || condition.operator != "" || len(condition.values) != 0 {
			builder.issue("invalid_condition_shape", path, "not conditions cannot contain predicate properties")
		}
	case ConditionKindPredicate:
		if len(condition.conditions) != 0 {
			builder.issue("invalid_condition_shape", path, "predicate conditions cannot contain child conditions")
		}
		if condition.scope != ConditionScopeDocument && condition.scope != ConditionScopeSibling {
			builder.issue("invalid_condition_scope", path+".scope", fmt.Sprintf("unsupported condition scope %q", condition.scope))
		}
		if strings.TrimSpace(condition.path) == "" {
			builder.issue("invalid_condition_path", path+".path", "condition path must not be empty")
		}
		switch condition.operator {
		case ConditionEquals, ConditionNotEquals:
			if len(condition.values) != 1 {
				builder.issue("invalid_condition_values", path+".values", "equals and notEquals conditions require exactly one value")
			}
		case ConditionOneOf:
			if len(condition.values) == 0 {
				builder.issue("invalid_condition_values", path+".values", "oneOf conditions require at least one value")
			}
		default:
			builder.issue("invalid_condition_operator", path+".operator", fmt.Sprintf("unsupported condition operator %q", condition.operator))
		}
		seen := make(map[string]struct{}, len(condition.values))
		for index, value := range condition.values {
			key := string(value.Kind()) + "\x00" + value.String()
			if _, duplicate := seen[key]; duplicate {
				builder.issue("duplicate_condition_value", fmt.Sprintf("%s.values[%d]", path, index), "condition value was configured more than once")
			}
			seen[key] = struct{}{}
			if index > 0 && value.Kind() != condition.values[0].Kind() {
				builder.issue("mixed_condition_values", fmt.Sprintf("%s.values[%d]", path, index), "oneOf condition values must use one scalar type")
			}
		}
	default:
		builder.issue("invalid_condition_kind", path+".kind", fmt.Sprintf("unsupported condition kind %q", condition.kind))
	}
	for index, child := range condition.conditions {
		validateCondition(builder, child, fmt.Sprintf("%s.conditions[%d]", path, index), depth+1, nodes)
	}
}

func joinOptionPath(prefix, suffix string) string {
	if suffix == "" {
		return prefix
	}
	return prefix + "." + suffix
}

// Text defines a single-line string field.
func Text(name string, options ...StringOption) Definition {
	return build(KindText, name, options)
}

// Code defines a source-code string with an optional editor language hint.
func Code(name string, options ...StringOption) Definition {
	return build(KindCode, name, options)
}

// Textarea defines a multi-line plain-text field.
func Textarea(name string, options ...StringOption) Definition {
	return build(KindTextarea, name, options)
}

// Email defines a string field validated as an email address.
func Email(name string, options ...StringOption) Definition {
	return build(KindEmail, name, options)
}

// Date defines a string field containing a normalized date, time, or timestamp value.
func Date(name string, options ...StringOption) Definition {
	return build(KindDate, name, options)
}

// Number defines a numeric field.
func Number(name string, options ...NumberOption) Definition {
	return build(KindNumber, name, options)
}

// Checkbox defines a boolean field rendered as an on/off control in the admin.
func Checkbox(name string, options ...CheckboxOption) Definition {
	return build(KindCheckbox, name, options)
}

// JSON defines a field containing arbitrary JSON-compatible data.
func JSON(name string, options ...JSONOption) Definition {
	return build(KindJSON, name, options)
}

// Select defines a string field constrained to configured Choices.
func Select(name string, options ...SelectOption) Definition {
	return build(KindSelect, name, options)
}

// Radio defines a string constrained to choices and rendered as a radio group.
func Radio(name string, options ...SelectOption) Definition {
	return build(KindRadio, name, options)
}

// Point defines a GeoJSON-style longitude/latitude tuple.
func Point(name string, options ...JSONOption) Definition {
	return build(KindPoint, name, options)
}

// Relationship defines a reference to one or more documents in other collections.
func Relationship(name string, options ...RelationshipOption) Definition {
	return build(KindRelationship, name, options)
}

// Upload defines a reference to documents in an upload-enabled collection.
func Upload(name string, options ...UploadOption) Definition {
	return build(KindUpload, name, options)
}

// Group defines a nested object whose children are configured with Fields.
func Group(name string, options ...GroupOption) Definition {
	return build(KindGroup, name, options)
}

// Array defines a repeatable list of objects whose children are configured with Fields.
func Array(name string, options ...ArrayOption) Definition {
	return build(KindArray, name, options)
}

// Blocks defines a repeatable list of discriminated layouts configured with BlockTypes.
func Blocks(name string, options ...BlocksOption) Definition {
	return build(KindBlocks, name, options)
}

// NamedTab defines a data-bearing tab. Its children are stored beneath name,
// matching Payload's named-tab document shape.
func NamedTab(name, label string, fields ...Definition) TabDefinition {
	return TabDefinition{Name: strings.TrimSpace(name), Label: strings.TrimSpace(label), Fields: cloneDefinitions(fields)}
}

// UnnamedTab defines a presentation-only tab. Its children retain their
// ordinary document paths.
func UnnamedTab(label string, fields ...Definition) TabDefinition {
	return TabDefinition{Label: strings.TrimSpace(label), Fields: cloneDefinitions(fields)}
}

// Tabs groups named data-bearing and unnamed presentation-only authoring tabs.
func Tabs(tabs ...TabDefinition) Definition {
	return Definition{kind: KindTabs, tabs: cloneTabs(tabs)}
}

// Row groups fields into one presentation-only admin row. Its children retain
// their ordinary document paths and storage identities.
func Row(fields ...Definition) Definition {
	return cloneDefinitions([]Definition{{kind: KindRow, fields: cloneDefinitions(fields)}})[0]
}

// UI defines presentation-only content. It is omitted from stored and generated document values.
func UI(name string, options ...CommonOption) Definition {
	return build(KindUI, name, options)
}

// Collapsible groups fields in a presentation-only disclosure. Child fields retain their paths.
func Collapsible(name string, initiallyCollapsed bool, fields ...Definition) Definition {
	return Definition{kind: KindCollapsible, name: name, fields: cloneDefinitions(fields), initiallyCollapsed: initiallyCollapsed}
}

// Join defines a read-only inverse relationship populated from target documents.
func Join(name, collection, on string, options ...JoinOption) Definition {
	definition := build(KindJoin, name, options)
	definition.joinCollection = strings.TrimSpace(collection)
	definition.joinOn = strings.TrimSpace(on)
	if definition.joinLimit == 0 {
		definition.joinLimit = 10
	}
	if !definition.joinCreateConfigured {
		definition.joinAllowCreate = true
	}
	return cloneDefinitions([]Definition{definition})[0]
}

// Virtual declares a computed output field whose resolver lives on the collection or global.
func Virtual(name string, valueType ValueType, options ...CommonOption) Definition {
	definition := build(KindVirtual, name, options)
	definition.valueType = valueType
	return cloneDefinitions([]Definition{definition})[0]
}

// Plugin creates a declarative custom field owned by a compiled plugin.
func Plugin(name, pluginKey string, config json.RawMessage, options ...PluginOption) Definition {
	definition := build(KindPlugin, name, options)
	definition.pluginKey = pluginKey
	definition.pluginConfig = append(json.RawMessage(nil), config...)
	return cloneDefinitions([]Definition{definition})[0]
}

// CollectionReferenceKeys declares plugin-owned JSON properties whose string
// values are collection slugs at any nested depth. Every declared key may
// target any configured collection. Ridu uses this contract for semantic
// renames and fail-closed persisted-data migration safety across current values
// and version history; arbitrary plugin JSON is never guessed.
func CollectionReferenceKeys(keys ...string) PluginOption {
	return pluginOptionFunc(func(builder *builder) {
		seen := make(map[string]bool, len(keys))
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" || seen[key] {
				builder.issue("invalid_reference_key", "options.referenceKeys", "plugin collection reference keys must be distinct non-empty JSON property names")
				continue
			}
			seen[key] = true
			builder.definition.pluginReferenceKeys = append(builder.definition.pluginReferenceKeys, key)
		}
	})
}

// AdminComponent selects one component exported by a statically paired admin
// plugin while retaining this field's built-in storage and runtime semantics.
// Config must be deterministic JSON safe to expose in the schema manifest.
func AdminComponent(pluginKey, component string, config json.RawMessage) CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.adminComponentSet {
			builder.issue("duplicate_option", "options.adminComponent", "admin component was configured more than once")
		}
		builder.adminComponentSet = true
		if len(config) != 0 {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(config, &object); err != nil || object == nil {
				builder.issue("invalid_admin_component_config", "options.adminComponent.config", "admin component config must be a JSON object")
			}
		}
		builder.definition.adminPluginKey = strings.TrimSpace(pluginKey)
		builder.definition.adminComponent = strings.TrimSpace(component)
		builder.definition.adminComponentConfig = append(json.RawMessage(nil), config...)
		builder.definition.adminComponentConfigured = true
	})
}

// BlockType constructs one member of a discriminated blocks field.
func BlockType(key, label string, fields ...Definition) Block {
	return Block{Key: key, Label: label, Fields: cloneDefinitions(fields)}
}

// Label overrides the humanized field name shown to authors.
func Label(label string) CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.labelSet {
			builder.issue("duplicate_option", "options.label", "field label was configured more than once")
		}
		builder.labelSet = true
		builder.definition.label = label
	})
}

// LabelTranslations adds localized field labels for admin languages.
func LabelTranslations(translations map[string]string) CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.labelTranslationsSet {
			builder.issue("duplicate_option", "options.labelTranslations", "field label translations were configured more than once")
		}
		builder.labelTranslationsSet = true
		builder.definition.labelTranslations = cloneTranslations(translations)
	})
}

// Required rejects missing, null, and field-type-specific empty values during validation.
func Required() CommonOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.requiredSet {
			builder.issue("duplicate_option", "options.required", "required was configured more than once")
		}
		builder.requiredSet = true
		builder.definition.required = true
	})
}

// Unique requires values to be distinct within the collection.
func Unique() UniqueOption {
	return uniqueOptionFunc(func(builder *builder) {
		if builder.uniqueSet {
			builder.issue("duplicate_option", "options.unique", "unique was configured more than once")
		}
		builder.uniqueSet = true
		builder.definition.unique = true
	})
}

// Index requests a non-unique database index for a supported scalar or
// singular reference field. Unique fields already receive a unique index.
func Index() UniqueOption {
	return uniqueOptionFunc(func(builder *builder) {
		if builder.indexSet {
			builder.issue("duplicate_option", "options.index", "field index was configured more than once")
		}
		builder.indexSet = true
		builder.definition.index = true
	})
}

// MinLength sets the inclusive minimum Unicode code-point length for text,
// textarea, and code fields.
func MinLength(value int) StringOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.minLengthSet {
			builder.issue("duplicate_option", "options.minLength", "minimum length was configured more than once")
		}
		builder.minLengthSet = true
		builder.definition.minLength = &value
		if value < 0 {
			builder.issue("invalid_min_length", "options.minLength", "minimum length must not be negative")
		}
	})
}

// MaxLength sets the inclusive maximum Unicode code-point length for text,
// textarea, and code fields.
func MaxLength(value int) StringOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.maxLengthSet {
			builder.issue("duplicate_option", "options.maxLength", "maximum length was configured more than once")
		}
		builder.maxLengthSet = true
		builder.definition.maxLength = &value
		if value < 0 {
			builder.issue("invalid_max_length", "options.maxLength", "maximum length must not be negative")
		}
	})
}

// Min sets the inclusive minimum accepted by a number field.
func Min(value float64) NumberOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.minimumSet {
			builder.issue("duplicate_option", "options.min", "number minimum was configured more than once")
		}
		builder.minimumSet = true
		builder.definition.minimum = &value
		if math.IsNaN(value) || math.IsInf(value, 0) {
			builder.issue("invalid_min", "options.min", "number minimum must be finite")
		}
	})
}

// Max sets the inclusive maximum accepted by a number field.
func Max(value float64) NumberOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.maximumSet {
			builder.issue("duplicate_option", "options.max", "number maximum was configured more than once")
		}
		builder.maximumSet = true
		builder.definition.maximum = &value
		if math.IsNaN(value) || math.IsInf(value, 0) {
			builder.issue("invalid_max", "options.max", "number maximum must be finite")
		}
	})
}

// Step sets the positive increment exposed by number inputs. It does not add
// server-side divisibility validation.
func Step(value float64) NumberOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.stepSet {
			builder.issue("duplicate_option", "options.step", "number step was configured more than once")
		}
		builder.stepSet = true
		builder.definition.step = &value
		if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
			builder.issue("invalid_step", "options.step", "number step must be finite and positive")
		}
	})
}

// Default sets a typed string, number, or boolean field default.
func Default[Value ~string | ~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64](value Value) DefaultOption {
	normalized, err := normalizeDefault(value)
	return defaultOption{value: normalized, err: err}
}

// Choices supplies the complete allowed value and label set for a select field.
func Choices(choices ...Choice) SelectOption {
	return selectOptionFunc(func(builder *builder) {
		if builder.choicesSet {
			builder.issue("duplicate_option", "options.choices", "select choices were configured more than once")
		}
		builder.choicesSet = true
		builder.definition.choices = append([]Choice(nil), choices...)
	})
}

// OneOf derives author-facing labels from concise select values.
func OneOf(values ...string) SelectOption {
	choices := make([]Choice, len(values))
	for index, value := range values {
		choices[index] = Choice{Value: value}
	}
	return Choices(choices...)
}

// Multiple changes a select from one choice to an ordered list of choices.
func Multiple() SelectOption {
	return selectOptionFunc(func(builder *builder) {
		if builder.selectManySet {
			builder.issue("duplicate_option", "options.hasMany", "select cardinality was configured more than once")
		}
		builder.selectManySet = true
		builder.definition.selectMany = true
	})
}

// DefaultChoices supplies the ordered default value for a multi-select.
func DefaultChoices(values ...string) SelectOption {
	return selectOptionFunc(func(builder *builder) {
		if builder.defaultSet || builder.selectDefaultsSet {
			builder.issue("duplicate_option", "options.default", "field default was configured more than once")
		}
		builder.defaultSet = true
		builder.selectDefaultsSet = true
		builder.definition.selectDefaults = append([]string(nil), values...)
		if len(values) == 0 {
			builder.issue("invalid_default", "options.default", "multi-select default must contain at least one choice")
		}
	})
}

// Language configures the syntax language hint for a code editor.
func Language(value string) StringOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.languageSet {
			builder.issue("duplicate_option", "options.language", "code language was configured more than once")
		}
		builder.languageSet = true
		builder.definition.codeLanguage = strings.TrimSpace(value)
	})
}

// PickerAppearance configures a date field as day-only, day-and-time, or time-only.
// It mirrors Payload's pickerAppearance vocabulary while retaining Ridu's typed Go config.
func PickerAppearance(value DatePickerAppearance) StringOption {
	return commonOptionFunc(func(builder *builder) {
		if builder.dateAppearanceSet {
			builder.issue("duplicate_option", "options.pickerAppearance", "date picker appearance was configured more than once")
		}
		builder.dateAppearanceSet = true
		builder.definition.datePickerAppearance = value
		if value != DatePickerDayOnly && value != DatePickerDayAndTime && value != DatePickerTimeOnly {
			builder.issue("invalid_date_picker_appearance", "options.pickerAppearance", "date picker appearance must be dayOnly, dayAndTime, or timeOnly")
		}
	})
}

// To selects the one collection referenced by a relationship or upload field.
func To(collectionSlug string) RelationshipOrUploadOption {
	return referenceOptionFunc(func(builder *builder) {
		if builder.toSet {
			builder.issue("duplicate_option", "options.to", "relationship target was configured more than once")
		}
		builder.toSet = true
		builder.definition.relationTo = []string{collectionSlug}
	})
}

// ToAny permits a polymorphic relationship to reference any listed collection.
func ToAny(collectionSlugs ...string) RelationshipOption {
	return relationshipOptionFunc(func(builder *builder) {
		if builder.toSet {
			builder.issue("duplicate_option", "options.to", "relationship target was configured more than once")
		}
		builder.toSet = true
		builder.definition.relationTo = append([]string(nil), collectionSlugs...)
	})
}

// HasMany changes a reference field from one document to a list of documents.
func HasMany() RelationshipOrUploadOption {
	return referenceOptionFunc(func(builder *builder) {
		if builder.manySet {
			builder.issue("duplicate_option", "options.hasMany", "relationship cardinality was configured more than once")
		}
		builder.manySet = true
		builder.definition.relationMany = true
	})
}

// OnDelete configures current-document behavior when a referenced target is
// hard deleted. Nullify clears singular values and removes matching list
// members; Restrict rejects the target delete. Required references always
// resolve to Restrict and reject an explicit Nullify action so automatic
// reconciliation cannot create a value that ordinary validation forbids.
// Version snapshots are never rewritten.
func OnDelete(action ReferenceDeleteAction) RelationshipOrUploadOption {
	return referenceOptionFunc(func(builder *builder) {
		if builder.onDeleteSet {
			builder.issue("duplicate_option", "options.onDelete", "reference delete behavior was configured more than once")
		}
		builder.onDeleteSet = true
		builder.definition.referenceDeleteAction = action
		if action != ReferenceDeleteNullify && action != ReferenceDeleteRestrict {
			builder.issue("invalid_reference_delete_action", "options.onDelete", "reference delete action must be nullify or restrict")
		}
	})
}

// OptionFilter derives one relationship-choice predicate from current document data.
func OptionFilter(targetPath string, operator RelationshipFilterOperator, sourcePath string) RelationshipFilterRule {
	return RelationshipFilterRule{TargetPath: strings.TrimSpace(targetPath), Operator: operator, SourcePath: strings.TrimSpace(sourcePath)}
}

// OptionFilterFor limits an option-filter rule to one target of a polymorphic relationship.
func OptionFilterFor(collection, targetPath string, operator RelationshipFilterOperator, sourcePath string) RelationshipFilterRule {
	rule := OptionFilter(targetPath, operator, sourcePath)
	rule.Collection = strings.TrimSpace(collection)
	return rule
}

// OptionFilterValue compares a target field with one static scalar value.
// Literal filters are applied by both admin pickers and server-side reference admission.
func OptionFilterValue[Value ~string | ~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64](targetPath string, operator RelationshipFilterOperator, value Value) RelationshipFilterRule {
	literal, err := normalizeDefault(value)
	rule := RelationshipFilterRule{TargetPath: strings.TrimSpace(targetPath), Operator: operator}
	if err == nil {
		rule.Literal = &literal
	}
	return rule
}

// OptionFilterValueFor limits a static option filter to one polymorphic target.
func OptionFilterValueFor[Value ~string | ~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64](collection, targetPath string, operator RelationshipFilterOperator, value Value) RelationshipFilterRule {
	rule := OptionFilterValue(targetPath, operator, value)
	rule.Collection = strings.TrimSpace(collection)
	return rule
}

// FilterOptionRules adds combinable nested and target-specific reference
// predicates. The rules drive pickers and are revalidated server-side.
func FilterOptionRules(rules ...RelationshipFilterRule) RelationshipOrUploadOption {
	return referenceOptionFunc(func(builder *builder) {
		seen := make(map[string]struct{}, len(rules))
		for index, rule := range rules {
			path := fmt.Sprintf("options.filterOptionRules[%d]", index)
			hasSource := rule.SourcePath != ""
			hasLiteral := rule.Literal != nil
			if rule.TargetPath == "" || hasSource == hasLiteral {
				builder.issue("invalid_relationship_filter", path, "relationship option filter requires a target path and exactly one source path or literal value")
				continue
			}
			if !validRelationshipFilterOperator(rule.Operator) {
				builder.issue("invalid_relationship_filter_operator", path+".operator", fmt.Sprintf("unsupported relationship option filter operator %q", rule.Operator))
				continue
			}
			literalKey := ""
			if rule.Literal != nil {
				literalKey = string(rule.Literal.Kind()) + ":" + rule.Literal.String()
			}
			key := rule.Collection + "\x00" + rule.TargetPath + "\x00" + string(rule.Operator) + "\x00" + rule.SourcePath + "\x00" + literalKey
			if _, exists := seen[key]; exists {
				builder.issue("duplicate_relationship_filter", path, "relationship option filter rule was configured more than once")
				continue
			}
			seen[key] = struct{}{}
			builder.definition.relationshipFilters = append(builder.definition.relationshipFilters, rule)
		}
	})
}

func validRelationshipFilterOperator(operator RelationshipFilterOperator) bool {
	switch operator {
	case FilterEquals, FilterNotEquals, FilterLike, FilterContains, FilterGreaterThan, FilterGreaterThanEqual, FilterLessThan, FilterLessThanEqual:
		return true
	default:
		return false
	}
}

// ToMany is shorthand for To(collectionSlug) plus HasMany.
func ToMany(collectionSlug string) RelationshipOrUploadOption {
	return referenceOptionFunc(func(builder *builder) {
		To(collectionSlug).apply(builder)
		HasMany().apply(builder)
	})
}

// Fields configures the child definitions stored inside a group or each array item.
func Fields(fields ...Definition) NestedOption {
	return nestedOptionFunc(func(builder *builder) {
		if builder.fieldsSet {
			builder.issue("duplicate_option", "options.fields", "nested fields were configured more than once")
		}
		builder.fieldsSet = true
		builder.definition.fields = cloneDefinitions(fields)
	})
}

// MinRows sets the minimum accepted array length.
func MinRows(value int) ArrayOption {
	return nestedOptionFunc(func(builder *builder) {
		if builder.minRowsSet {
			builder.issue("duplicate_option", "options.minRows", "minimum rows was configured more than once")
		}
		builder.minRowsSet = true
		builder.definition.minRows = value
		if value < 0 {
			builder.issue("invalid_min_rows", "options.minRows", "minimum rows must not be negative")
		}
	})
}

// MaxRows sets the maximum accepted array length.
func MaxRows(value int) ArrayOption {
	return nestedOptionFunc(func(builder *builder) {
		if builder.maxRowsSet {
			builder.issue("duplicate_option", "options.maxRows", "maximum rows was configured more than once")
		}
		builder.maxRowsSet = true
		builder.definition.maxRows = value
		if value < 1 {
			builder.issue("invalid_max_rows", "options.maxRows", "maximum rows must be at least one")
		}
	})
}

// RowLabel selects a child property used for array row headings.
func RowLabel(path string) ArrayOption {
	return nestedOptionFunc(func(builder *builder) {
		if builder.rowLabelSet {
			builder.issue("duplicate_option", "options.rowLabel", "row label was configured more than once")
		}
		builder.rowLabelSet = true
		builder.definition.rowLabel = strings.TrimSpace(path)
		if builder.definition.rowLabel == "" {
			builder.issue("invalid_row_label", "options.rowLabel", "row label must not be empty")
		}
	})
}

// RowLabelComponent selects one row-label component exported by a statically
// paired admin plugin. Config must be deterministic JSON safe to expose in the
// schema manifest. The component changes presentation only; RowLabel remains
// available as its string fallback and for accessible action labels.
func RowLabelComponent(pluginKey, component string, config json.RawMessage) RowLabelComponentOption {
	return rowLabelComponentOptionFunc(func(builder *builder) {
		if builder.rowLabelComponentSet {
			builder.issue("duplicate_option", "options.rowLabelComponent", "row label component was configured more than once")
		}
		builder.rowLabelComponentSet = true
		if len(config) != 0 {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(config, &object); err != nil || object == nil {
				builder.issue("invalid_row_label_component_config", "options.rowLabelComponent.config", "row label component config must be a JSON object")
			}
		}
		builder.definition.rowLabelAdminPluginKey = strings.TrimSpace(pluginKey)
		builder.definition.rowLabelComponent = strings.TrimSpace(component)
		builder.definition.rowLabelComponentConfig = append(json.RawMessage(nil), config...)
		builder.definition.rowLabelComponentConfigured = true
	})
}

// ArrayRowLabels configures singular and plural display names for array rows.
// It is separate from RowLabel, which identifies a child value used as a row heading.
func ArrayRowLabels(labels RowLabels) ArrayOption {
	return nestedOptionFunc(func(builder *builder) {
		if builder.rowLabelsSet {
			builder.issue("duplicate_option", "options.rowLabels", "array row labels were configured more than once")
		}
		builder.rowLabelsSet = true
		builder.definition.rowLabels = cloneRowLabels(labels)
	})
}

// JoinLimit bounds the number of documents embedded by an inverse join.
func JoinLimit(value int) JoinOption {
	return joinOptionFunc(func(builder *builder) {
		if builder.joinLimitSet {
			builder.issue("duplicate_option", "options.limit", "join limit was configured more than once")
		}
		builder.joinLimitSet = true
		builder.definition.joinLimit = value
		if value < 1 || value > 100 {
			builder.issue("invalid_join_limit", "options.limit", "join limit must be between 1 and 100")
		}
	})
}

// JoinColumns selects target document fields shown in the inverse-join table.
func JoinColumns(paths ...string) JoinOption {
	return joinOptionFunc(func(builder *builder) {
		if builder.joinColumnsSet {
			builder.issue("duplicate_option", "options.defaultColumns", "join columns were configured more than once")
		}
		builder.joinColumnsSet = true
		builder.definition.joinDefaultColumns = make([]string, 0, len(paths))
		seen := make(map[string]struct{}, len(paths))
		for index, path := range paths {
			path = strings.TrimSpace(path)
			if path == "" {
				builder.issue("invalid_join_column", fmt.Sprintf("options.defaultColumns[%d]", index), "join column must not be empty")
				continue
			}
			if _, exists := seen[path]; exists {
				builder.issue("duplicate_join_column", fmt.Sprintf("options.defaultColumns[%d]", index), fmt.Sprintf("join column %q was configured more than once", path))
				continue
			}
			seen[path] = struct{}{}
			builder.definition.joinDefaultColumns = append(builder.definition.joinDefaultColumns, path)
		}
	})
}

// JoinDefaultSort selects the initial target sort. Prefix the path with "-" for descending order.
func JoinDefaultSort(value string) JoinOption {
	return joinOptionFunc(func(builder *builder) {
		if builder.joinSortSet {
			builder.issue("duplicate_option", "options.defaultSort", "join default sort was configured more than once")
		}
		builder.joinSortSet = true
		builder.definition.joinDefaultSort = strings.TrimSpace(value)
		if strings.TrimPrefix(builder.definition.joinDefaultSort, "-") == "" {
			builder.issue("invalid_join_sort", "options.defaultSort", "join default sort must name a field path")
		}
	})
}

// JoinAllowCreate controls whether the inverse-join browser offers inline target creation.
func JoinAllowCreate(value bool) JoinOption {
	return joinOptionFunc(func(builder *builder) {
		if builder.joinAllowCreateSet {
			builder.issue("duplicate_option", "options.allowCreate", "join create behavior was configured more than once")
		}
		builder.joinAllowCreateSet = true
		builder.definition.joinAllowCreate = value
		builder.definition.joinCreateConfigured = true
	})
}

// BlockTypes supplies the allowed discriminated layouts for a blocks field.
func BlockTypes(blocks ...Block) BlocksOption {
	return blocksOptionFunc(func(builder *builder) {
		if builder.blocksSet {
			builder.issue("duplicate_option", "options.blocks", "block types were configured more than once")
		}
		builder.blocksSet = true
		builder.definition.blocks = cloneBlocks(blocks)
	})
}

func build[FieldOption Option](kind Kind, name string, options []FieldOption) Definition {
	builder := &builder{definition: Definition{kind: kind, name: name}}
	for index, option := range options {
		if isNilOption(option) {
			builder.issue("nil_option", fmt.Sprintf("options[%d]", index), "field option must not be nil")
			continue
		}
		option.apply(builder)
	}
	builder.validateCompatibility()
	return cloneDefinitions([]Definition{builder.definition})[0]
}

func isNilOption(option any) bool {
	if option == nil {
		return true
	}
	value := reflect.ValueOf(option)
	return value.Kind() == reflect.Func && value.IsNil()
}

func (builder *builder) validateCompatibility() {
	switch builder.definition.kind {
	case KindText, KindCode, KindTextarea, KindEmail, KindDate, KindSelect, KindRadio:
		builder.rejectDefaultKind(DefaultString)
	case KindNumber:
		builder.rejectDefaultKind(DefaultNumber)
	case KindCheckbox:
		builder.rejectDefaultKind(DefaultBoolean)
	case KindJSON, KindPoint, KindRelationship, KindUpload, KindGroup, KindArray, KindBlocks, KindPlugin, KindUI, KindJoin, KindVirtual:
	default:
		builder.issue("unknown_field_kind", "type", fmt.Sprintf("unknown field kind %q", builder.definition.kind))
	}
	if builder.definition.kind != KindCode && builder.languageSet {
		builder.issue("incompatible_option", "options.language", "language can only be used with code fields")
	}
	if builder.definition.kind != KindDate && builder.dateAppearanceSet {
		builder.issue("incompatible_option", "options.pickerAppearance", "picker appearance can only be used with date fields")
	}
	if builder.definition.selectMany {
		if builder.definition.kind != KindSelect {
			builder.issue("invalid_radio_cardinality", "options.hasMany", "only select fields can store multiple choices")
		}
		if builder.definition.defaultValue != nil {
			builder.issue("invalid_default", "options.default", "multi-select defaults must use DefaultChoices")
		}
	}
	if builder.selectDefaultsSet && !builder.definition.selectMany {
		builder.issue("invalid_default", "options.default", "DefaultChoices requires Multiple")
	}
	if builder.placeholderSet || builder.placeholderTranslationsSet {
		switch builder.definition.kind {
		case KindText, KindTextarea, KindEmail, KindNumber, KindSelect, KindRelationship, KindUpload:
		default:
			builder.issue("incompatible_option", "options.placeholder", "placeholder can only be used with text, textarea, email, number, select, relationship, and upload fields")
		}
		if strings.TrimSpace(builder.definition.placeholder) == "" {
			builder.issue("invalid_placeholder", "options.placeholder", "placeholder must provide non-blank canonical text")
		}
	}
	if builder.definition.kind != KindText && builder.definition.kind != KindCode && builder.definition.kind != KindTextarea {
		if builder.minLengthSet {
			builder.issue("incompatible_option", "options.minLength", "minimum length can only be used with text, textarea, and code fields")
		}
		if builder.maxLengthSet {
			builder.issue("incompatible_option", "options.maxLength", "maximum length can only be used with text, textarea, and code fields")
		}
	}
	if builder.definition.minLength != nil && builder.definition.maxLength != nil && *builder.definition.minLength > *builder.definition.maxLength {
		builder.issue("invalid_length_bounds", "options.minLength", "minimum length must not exceed maximum length")
	}
	if builder.definition.minimum != nil && builder.definition.maximum != nil && *builder.definition.minimum > *builder.definition.maximum {
		builder.issue("invalid_number_bounds", "options.min", "number minimum must not exceed maximum")
	}
	if defaultValue := builder.definition.defaultValue; defaultValue != nil {
		switch defaultValue.Kind() {
		case DefaultString:
			length := utf8.RuneCountInString(defaultValue.String())
			if builder.definition.minLength != nil && length < *builder.definition.minLength {
				builder.issue("invalid_default", "options.default", "field default is shorter than the configured minimum length")
			}
			if builder.definition.maxLength != nil && length > *builder.definition.maxLength {
				builder.issue("invalid_default", "options.default", "field default is longer than the configured maximum length")
			}
		case DefaultNumber:
			number, err := strconv.ParseFloat(defaultValue.String(), 64)
			if err == nil && builder.definition.minimum != nil && number < *builder.definition.minimum {
				builder.issue("invalid_default", "options.default", "number default is below the configured minimum")
			}
			if err == nil && builder.definition.maximum != nil && number > *builder.definition.maximum {
				builder.issue("invalid_default", "options.default", "number default is above the configured maximum")
			}
		}
	}
	if builder.definition.kind == KindArray && builder.definition.maxRows > 0 && builder.definition.minRows > builder.definition.maxRows {
		builder.issue("invalid_row_bounds", "options.minRows", "minimum rows must not exceed maximum rows")
	}
	if (builder.definition.kind == KindUI || builder.definition.kind == KindJoin || builder.definition.kind == KindVirtual) && builder.requiredSet {
		builder.issue("incompatible_option", "options.required", "presentation and computed fields cannot be required")
	}
}

func (builder *builder) rejectDefaultKind(expected DefaultKind) {
	if builder.definition.defaultValue != nil && builder.definition.defaultValue.Kind() != expected {
		builder.issue("invalid_default_type", "options.default", fmt.Sprintf("%s field default must be a %s", builder.definition.kind, expected))
	}
}

func normalizeDefault[Value ~string | ~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64](value Value) (DefaultValue, error) {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.String:
		return DefaultValue{kind: DefaultString, text: reflected.String()}, nil
	case reflect.Bool:
		return DefaultValue{kind: DefaultBoolean, text: strconv.FormatBool(reflected.Bool())}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return DefaultValue{kind: DefaultNumber, text: strconv.FormatInt(reflected.Int(), 10)}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return DefaultValue{kind: DefaultNumber, text: strconv.FormatUint(reflected.Uint(), 10)}, nil
	case reflect.Float32, reflect.Float64:
		number := reflected.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return DefaultValue{}, fmt.Errorf("number field default must be finite")
		}
		return DefaultValue{kind: DefaultNumber, text: strconv.FormatFloat(number, 'g', -1, reflected.Type().Bits())}, nil
	default:
		return DefaultValue{}, fmt.Errorf("unsupported default type %T", value)
	}
}

func (builder *builder) issue(code, path, message string) {
	builder.definition.issues = append(builder.definition.issues, Issue{Code: code, Path: path, Message: message})
}
