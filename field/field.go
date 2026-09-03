package field

import "encoding/json"

// Kind identifies a built-in authoring field definition.
type Kind string

const (
	KindText         Kind = "text"
	KindCode         Kind = "code"
	KindSelect       Kind = "select"
	KindRadio        Kind = "radio"
	KindPoint        Kind = "point"
	KindRelationship Kind = "relationship"
	KindUpload       Kind = "upload"
	KindGroup        Kind = "group"
	KindTextarea     Kind = "textarea"
	KindEmail        Kind = "email"
	KindDate         Kind = "date"
	KindNumber       Kind = "number"
	KindCheckbox     Kind = "checkbox"
	KindJSON         Kind = "json"
	KindArray        Kind = "array"
	KindBlocks       Kind = "blocks"
	KindTabs         Kind = "tabs"
	KindRow          Kind = "row"
	KindUI           Kind = "ui"
	KindCollapsible  Kind = "collapsible"
	KindJoin         Kind = "join"
	KindVirtual      Kind = "virtual"
	KindPlugin       Kind = "plugin"
)

// Category describes the broad behavior a field contributes to a resolved
// schema. Schema manifests use categories to avoid treating every field as an
// unstructured type string.
type Category string

const (
	CategoryScalar       Category = "scalar"
	CategoryNested       Category = "nested"
	CategoryPresentation Category = "presentation"
	CategoryRelationship Category = "relationship"
	CategoryUpload       Category = "upload"
	CategoryPlugin       Category = "plugin"
)

// Choice is one allowed value and its author-facing label for a select field.
type Choice struct {
	// Value is the exact string stored in documents and sent over APIs.
	Value string
	// Label is the author-facing name shown in selection controls.
	Label string
	// LabelTranslations overrides Label for configured admin interface languages.
	LabelTranslations map[string]string
}

// WithLabelTranslations returns a detached choice with localized display labels.
func (choice Choice) WithLabelTranslations(translations map[string]string) Choice {
	choice.LabelTranslations = cloneTranslations(translations)
	return choice
}

// Block is one discriminated layout allowed by a blocks field.
type Block struct {
	// Key is the stable discriminator stored with block values.
	Key string
	// Label is the author-facing block type name.
	Label string
	// LabelTranslations overrides Label for configured admin interface languages.
	LabelTranslations map[string]string
	// Fields defines the values stored by this block type.
	Fields []Definition
}

// WithLabelTranslations returns a detached block type with localized display labels.
func (block Block) WithLabelTranslations(translations map[string]string) Block {
	block.LabelTranslations = cloneTranslations(translations)
	block.Fields = cloneDefinitions(block.Fields)
	return block
}

// TabDefinition is one authoring section inside a Tabs presentation field. Tabs with a
// Name store their children beneath that document property; unnamed tabs only
// affect the admin layout and leave child document paths unchanged.
type TabDefinition struct {
	// Name is the optional document property contributed by a data-bearing tab.
	Name string
	// Label is the author-facing tab trigger.
	Label string
	// LabelTranslations overrides Label for configured admin interface languages.
	LabelTranslations map[string]string
	// Fields defines the values shown inside the tab.
	Fields []Definition
}

// WithLabelTranslations returns a detached tab with localized trigger labels.
func (tab TabDefinition) WithLabelTranslations(translations map[string]string) TabDefinition {
	tab.LabelTranslations = cloneTranslations(translations)
	tab.Fields = cloneDefinitions(tab.Fields)
	return tab
}

// RowLabels contains optional singular and plural display names for array rows.
// RowLabel remains the separate child-field path used to derive each row heading.
type RowLabels struct {
	Singular             string
	Plural               string
	SingularTranslations map[string]string
	PluralTranslations   map[string]string
}

// ConditionKind identifies one node in a presentation-only condition tree.
type ConditionKind string

const (
	ConditionKindAll       ConditionKind = "all"
	ConditionKindAny       ConditionKind = "any"
	ConditionKindNot       ConditionKind = "not"
	ConditionKindPredicate ConditionKind = "predicate"
)

// ConditionScope selects the value tree used by a condition predicate.
type ConditionScope string

const (
	// ConditionScopeDocument resolves paths from the document root.
	ConditionScopeDocument ConditionScope = "document"
	// ConditionScopeSibling resolves paths from the current field's parent object or row.
	ConditionScopeSibling ConditionScope = "sibling"
)

// ConditionOperator identifies one scalar predicate operation.
type ConditionOperator string

const (
	ConditionEquals    ConditionOperator = "equals"
	ConditionNotEquals ConditionOperator = "notEquals"
	ConditionOneOf     ConditionOperator = "oneOf"
)

// ConditionScalar is the finite scalar vocabulary accepted by condition predicates.
type ConditionScalar interface {
	~string | ~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64
}

// Condition is an immutable, deterministic presentation expression. It never
// grants access or removes a hidden value from a document.
type Condition struct {
	kind       ConditionKind
	conditions []Condition
	scope      ConditionScope
	path       string
	operator   ConditionOperator
	values     []DefaultValue
	issues     []Issue
}

// Kind reports whether this node is a logical group, negation, or predicate.
func (condition Condition) Kind() ConditionKind { return condition.kind }

// Conditions returns detached child expressions for All, Any, and Not nodes.
func (condition Condition) Conditions() []Condition { return cloneConditions(condition.conditions) }

// Scope reports which value tree a predicate reads.
func (condition Condition) Scope() ConditionScope { return condition.scope }

// Path returns the predicate path relative to its configured scope.
func (condition Condition) Path() string { return condition.path }

// Operator returns the predicate's scalar comparison operator.
func (condition Condition) Operator() ConditionOperator { return condition.operator }

// Values returns detached, canonically encoded predicate operands.
func (condition Condition) Values() []DefaultValue {
	return append([]DefaultValue(nil), condition.values...)
}

// ValueType identifies the generated and runtime value contract of a virtual field.
type ValueType string

const (
	ValueString  ValueType = "string"
	ValueNumber  ValueType = "number"
	ValueBoolean ValueType = "boolean"
	ValueJSON    ValueType = "json"
)

// RelationshipFilterOperator is the finite predicate vocabulary used to narrow admin choices.
type RelationshipFilterOperator string

const (
	FilterEquals           RelationshipFilterOperator = "equals"
	FilterNotEquals        RelationshipFilterOperator = "notEquals"
	FilterLike             RelationshipFilterOperator = "like"
	FilterContains         RelationshipFilterOperator = "contains"
	FilterGreaterThan      RelationshipFilterOperator = "greaterThan"
	FilterGreaterThanEqual RelationshipFilterOperator = "greaterThanEqual"
	FilterLessThan         RelationshipFilterOperator = "lessThan"
	FilterLessThanEqual    RelationshipFilterOperator = "lessThanEqual"
)

// RelationshipFilterRule derives one target predicate from current document data.
// Collection optionally limits the rule to one target of a polymorphic relationship.
type RelationshipFilterRule struct {
	Collection string
	TargetPath string
	Operator   RelationshipFilterOperator
	SourcePath string
	Literal    *DefaultValue
}

// ReferenceDeleteAction controls how a hard-deleted target affects current
// relationship and upload values that point at it. Historical version
// snapshots are immutable and are not reconciled by this policy.
type ReferenceDeleteAction string

const (
	// ReferenceDeleteNullify clears a singular reference or removes matching
	// members from a has-many reference.
	ReferenceDeleteNullify ReferenceDeleteAction = "nullify"
	// ReferenceDeleteRestrict rejects the target hard delete while a current
	// document still references it.
	ReferenceDeleteRestrict ReferenceDeleteAction = "restrict"
)

// Issue records a definition problem relative to the field. Config resolution
// prefixes Path with the field's exact location in the application config.
type Issue struct {
	// Code is a stable machine-readable problem identifier.
	Code string
	// Path locates the invalid property within the field definition.
	Path string
	// Message explains how to correct the problem.
	Message string
}

// DefaultKind identifies the scalar type carried by a field default.
type DefaultKind string

const (
	DefaultString  DefaultKind = "string"
	DefaultNumber  DefaultKind = "number"
	DefaultBoolean DefaultKind = "boolean"
)

// DefaultValue is the normalized scalar value produced by Default.
type DefaultValue struct {
	kind DefaultKind
	text string
}

// DatePickerAppearance controls the native date control shown by the admin.
// Values remain strings on the wire: dates use YYYY-MM-DD, times use HH:mm,
// and date-times use RFC 3339 timestamps.
type DatePickerAppearance string

const (
	DatePickerDayOnly    DatePickerAppearance = "dayOnly"
	DatePickerDayAndTime DatePickerAppearance = "dayAndTime"
	DatePickerTimeOnly   DatePickerAppearance = "timeOnly"
)

// Kind reports whether the normalized default is a string, number, or boolean.
func (value DefaultValue) Kind() DefaultKind { return value.kind }

// String returns the canonical textual encoding written to the manifest.
func (value DefaultValue) String() string { return value.text }

// Definition is an immutable authoring description created by a field
// constructor. Slice-bearing accessors return copies so plugins and callers
// cannot mutate a definition after construction.
type Definition struct {
	name                        string
	kind                        Kind
	label                       string
	labelTranslations           map[string]string
	required                    bool
	unique                      bool
	index                       bool
	localized                   bool
	minLength                   *int
	maxLength                   *int
	minimum                     *float64
	maximum                     *float64
	step                        *float64
	defaultValue                *DefaultValue
	slugSource                  string
	slugConfigured              bool
	choices                     []Choice
	selectMany                  bool
	selectDefaults              []string
	relationTo                  []string
	relationMany                bool
	fields                      []Definition
	blocks                      []Block
	tabs                        []TabDefinition
	pluginKey                   string
	pluginConfig                json.RawMessage
	pluginReferenceKeys         []string
	adminPluginKey              string
	adminComponent              string
	adminComponentConfig        json.RawMessage
	adminComponentConfigured    bool
	description                 string
	descriptionTranslations     map[string]string
	placeholder                 string
	placeholderTranslations     map[string]string
	readOnly                    bool
	hidden                      bool
	sidebar                     bool
	columns                     int
	tab                         string
	tabTranslations             map[string]string
	condition                   *Condition
	codeLanguage                string
	datePickerAppearance        DatePickerAppearance
	minRows                     int
	maxRows                     int
	rowLabel                    string
	rowLabelAdminPluginKey      string
	rowLabelComponent           string
	rowLabelComponentConfig     json.RawMessage
	rowLabelComponentConfigured bool
	rowLabels                   RowLabels
	initiallyCollapsed          bool
	joinCollection              string
	joinOn                      string
	joinLimit                   int
	joinDefaultColumns          []string
	joinDefaultSort             string
	joinAllowCreate             bool
	joinCreateConfigured        bool
	valueType                   ValueType
	relationshipFilters         []RelationshipFilterRule
	referenceDeleteAction       ReferenceDeleteAction
	issues                      []Issue
}

// Name returns the document property name authored for the field.
func (d Definition) Name() string { return d.name }

// Kind returns the concrete authoring field kind.
func (d Definition) Kind() Kind { return d.kind }

// Label returns the explicit author-facing label, if one was configured.
func (d Definition) Label() string { return d.label }

// LabelTranslations returns localized author-facing labels by admin language.
func (d Definition) LabelTranslations() map[string]string {
	return cloneTranslations(d.labelTranslations)
}

// Required reports whether validation rejects a missing, null, or field-type-specific empty value.
func (d Definition) Required() bool { return d.required }

// Unique reports whether values must be distinct within the collection.
func (d Definition) Unique() bool { return d.unique }

// Index reports whether the field should have a non-unique database index.
// Unique fields already receive a unique index even when this is false.
func (d Definition) Index() bool { return d.index }

// Localized reports whether the field stores an independent value for each
// configured application locale.
func (d Definition) Localized() bool { return d.localized }

// RelationshipTarget returns the first configured target slug, or an empty string.
func (d Definition) RelationshipTarget() string {
	if len(d.relationTo) == 0 {
		return ""
	}
	return d.relationTo[0]
}

// RelationshipTargets returns a copy of all configured target slugs.
func (d Definition) RelationshipTargets() []string { return append([]string(nil), d.relationTo...) }

// RelationshipHasMany reports whether the field stores multiple references.
func (d Definition) RelationshipHasMany() bool { return d.relationMany }

// SelectHasMany reports whether a select stores an ordered list of choices.
func (d Definition) SelectHasMany() bool { return d.selectMany }

// SelectDefaults returns the configured ordered default choices for a multi-select.
func (d Definition) SelectDefaults() []string {
	return append([]string(nil), d.selectDefaults...)
}

// Category returns the behavioral category for the definition's field kind.
func (d Definition) Category() Category {
	switch d.kind {
	case KindText, KindCode, KindSelect, KindRadio, KindPoint, KindTextarea, KindEmail, KindDate, KindNumber, KindCheckbox, KindJSON:
		return CategoryScalar
	case KindRelationship:
		return CategoryRelationship
	case KindUpload:
		return CategoryUpload
	case KindGroup, KindArray, KindBlocks:
		return CategoryNested
	case KindTabs, KindRow, KindUI, KindCollapsible, KindJoin, KindVirtual:
		return CategoryPresentation
	default:
		return CategoryPlugin
	}
}

// Default returns the configured typed scalar default.
func (d Definition) Default() (DefaultValue, bool) {
	if d.defaultValue == nil {
		return DefaultValue{}, false
	}
	return *d.defaultValue, true
}

// SlugSource returns the source field path for a text-backed slug helper.
// The boolean distinguishes an invalid empty source from an ordinary text field.
func (d Definition) SlugSource() (string, bool) { return d.slugSource, d.slugConfigured }

// Choices returns a copy of the select choices.
func (d Definition) Choices() []Choice {
	return cloneChoices(d.choices)
}

// Fields returns a deep copy of the nested child definitions.
func (d Definition) Fields() []Definition {
	return cloneDefinitions(d.fields)
}

// Blocks returns a deep copy of the allowed block types.
func (d Definition) Blocks() []Block { return cloneBlocks(d.blocks) }

// Tabs returns a deep copy of the configured named and unnamed tabs.
func (d Definition) Tabs() []TabDefinition { return cloneTabs(d.tabs) }

// PluginKey returns the compiled plugin responsible for a custom field.
func (d Definition) PluginKey() string { return d.pluginKey }

// AdminComponent returns the optional statically registered admin plugin
// renderer selected for this field. The returned configuration is detached
// from the immutable definition.
func (d Definition) AdminComponent() (pluginKey, component string, config json.RawMessage, ok bool) {
	if !d.adminComponentConfigured {
		return "", "", nil, false
	}
	return d.adminPluginKey, d.adminComponent, append(json.RawMessage(nil), d.adminComponentConfig...), true
}

// Description returns the configured author-facing supporting text.
func (d Definition) Description() string { return d.description }

// DescriptionTranslations returns localized supporting text by admin language.
func (d Definition) DescriptionTranslations() map[string]string {
	return cloneTranslations(d.descriptionTranslations)
}

// Placeholder returns the canonical empty-state prompt for compatible admin controls.
func (d Definition) Placeholder() string { return d.placeholder }

// PlaceholderTranslations returns localized placeholder text by admin language.
func (d Definition) PlaceholderTranslations() map[string]string {
	return cloneTranslations(d.placeholderTranslations)
}

// ReadOnly reports whether the admin should prevent editing this field.
func (d Definition) ReadOnly() bool { return d.readOnly }

// Hidden reports whether the admin should omit this field from presentation.
func (d Definition) Hidden() bool { return d.hidden }

// Sidebar reports whether a root field belongs in the document editor's right rail.
func (d Definition) Sidebar() bool { return d.sidebar }

// Columns returns the requested one-to-twelve-column admin grid width, or zero.
func (d Definition) Columns() int { return d.columns }

// Tab returns the named admin form tab, or an empty string.
func (d Definition) Tab() string { return d.tab }

// TabTranslations returns localized direct-tab labels by admin language.
func (d Definition) TabTranslations() map[string]string { return cloneTranslations(d.tabTranslations) }

// Condition returns a copy of the configured presentation condition, if any.
func (d Definition) Condition() *Condition {
	if d.condition == nil {
		return nil
	}
	condition := cloneCondition(*d.condition)
	return &condition
}

// CodeLanguage is the editor language hint for a code field.
func (d Definition) CodeLanguage() string { return d.codeLanguage }

// MinLength returns the minimum accepted Unicode code-point length.
func (d Definition) MinLength() (int, bool) {
	if d.minLength == nil {
		return 0, false
	}
	return *d.minLength, true
}

// MaxLength returns the maximum accepted Unicode code-point length.
func (d Definition) MaxLength() (int, bool) {
	if d.maxLength == nil {
		return 0, false
	}
	return *d.maxLength, true
}

// Min returns the inclusive minimum accepted by a number field.
func (d Definition) Min() (float64, bool) {
	if d.minimum == nil {
		return 0, false
	}
	return *d.minimum, true
}

// Max returns the inclusive maximum accepted by a number field.
func (d Definition) Max() (float64, bool) {
	if d.maximum == nil {
		return 0, false
	}
	return *d.maximum, true
}

// Step returns the positive admin input increment for a number field. It is
// presentation metadata and does not impose divisibility validation.
func (d Definition) Step() (float64, bool) {
	if d.step == nil {
		return 0, false
	}
	return *d.step, true
}

// DatePickerAppearance returns the configured date control, defaulting to day-only.
func (d Definition) DatePickerAppearance() DatePickerAppearance {
	if d.datePickerAppearance == "" {
		return DatePickerDayOnly
	}
	return d.datePickerAppearance
}

// MinRows is the minimum accepted length for an array.
func (d Definition) MinRows() int { return d.minRows }

// MaxRows is the maximum accepted length for an array, or zero when unbounded.
func (d Definition) MaxRows() int { return d.maxRows }

// RowLabel is the child property used to label array rows in the admin.
func (d Definition) RowLabel() string { return d.rowLabel }

// RowLabelComponent returns the optional statically registered admin plugin
// component selected for array or blocks row headings. The returned
// configuration is detached from the immutable definition.
func (d Definition) RowLabelComponent() (pluginKey, component string, config json.RawMessage, ok bool) {
	if !d.rowLabelComponentConfigured {
		return "", "", nil, false
	}
	return d.rowLabelAdminPluginKey, d.rowLabelComponent, append(json.RawMessage(nil), d.rowLabelComponentConfig...), true
}

// RowLabels returns optional singular and plural author-facing array row names.
func (d Definition) RowLabels() RowLabels { return cloneRowLabels(d.rowLabels) }

// WithLabelTranslations returns an immutable copy with localized labels. It is
// useful for presentation definitions such as Collapsible that do not accept options.
func (d Definition) WithLabelTranslations(translations map[string]string) Definition {
	d.labelTranslations = cloneTranslations(translations)
	return cloneDefinitions([]Definition{d})[0]
}

// InitiallyCollapsed reports whether a collapsible presentation group starts closed.
func (d Definition) InitiallyCollapsed() bool { return d.initiallyCollapsed }

// JoinCollection is the collection queried by an inverse join.
func (d Definition) JoinCollection() string { return d.joinCollection }

// JoinOn is the target relationship path matched against the source document ID.
func (d Definition) JoinOn() string { return d.joinOn }

// JoinLimit is the maximum related documents returned for an inverse join.
func (d Definition) JoinLimit() int { return d.joinLimit }

// JoinDefaultColumns returns the target fields shown by the inverse-join table.
func (d Definition) JoinDefaultColumns() []string {
	return append([]string(nil), d.joinDefaultColumns...)
}

// JoinDefaultSort returns the target sort expression, including an optional descending prefix.
func (d Definition) JoinDefaultSort() string { return d.joinDefaultSort }

// JoinAllowCreate reports whether the admin may offer inline target creation.
func (d Definition) JoinAllowCreate() bool { return d.joinAllowCreate }

// ValueType is the declared output contract for a virtual field.
func (d Definition) ValueType() ValueType { return d.valueType }

// RelationshipFilters returns configured multi-rule and polymorphic option filters.
func (d Definition) RelationshipFilters() []RelationshipFilterRule {
	return cloneRelationshipFilterRules(d.relationshipFilters)
}

// ReferenceDeleteAction returns the explicitly authored hard-delete policy.
// An empty action means config resolution must apply the deterministic default
// for the reference shape.
func (d Definition) ReferenceDeleteAction() ReferenceDeleteAction {
	return d.referenceDeleteAction
}

// PluginConfig returns a copy of the plugin-owned serialized configuration.
func (d Definition) PluginConfig() json.RawMessage {
	return append(json.RawMessage(nil), d.pluginConfig...)
}

// PluginReferenceKeys returns JSON property names whose string values contain
// public collection slugs and therefore participate in content renames and
// persisted reference-shape migration safety.
func (d Definition) PluginReferenceKeys() []string {
	return append([]string(nil), d.pluginReferenceKeys...)
}

// Issues returns a copy of constructor and option compatibility issues.
func (d Definition) Issues() []Issue {
	return append([]Issue(nil), d.issues...)
}

func cloneDefinitions(definitions []Definition) []Definition {
	if definitions == nil {
		return nil
	}

	cloned := make([]Definition, len(definitions))
	for index, definition := range definitions {
		cloned[index] = definition
		cloned[index].labelTranslations = cloneTranslations(definition.labelTranslations)
		cloned[index].descriptionTranslations = cloneTranslations(definition.descriptionTranslations)
		cloned[index].placeholderTranslations = cloneTranslations(definition.placeholderTranslations)
		cloned[index].tabTranslations = cloneTranslations(definition.tabTranslations)
		cloned[index].rowLabels = cloneRowLabels(definition.rowLabels)
		cloned[index].choices = cloneChoices(definition.choices)
		cloned[index].selectDefaults = append([]string(nil), definition.selectDefaults...)
		cloned[index].relationTo = append([]string(nil), definition.relationTo...)
		cloned[index].joinDefaultColumns = append([]string(nil), definition.joinDefaultColumns...)
		cloned[index].relationshipFilters = cloneRelationshipFilterRules(definition.relationshipFilters)
		cloned[index].fields = cloneDefinitions(definition.fields)
		cloned[index].blocks = cloneBlocks(definition.blocks)
		cloned[index].tabs = cloneTabs(definition.tabs)
		cloned[index].pluginConfig = append(json.RawMessage(nil), definition.pluginConfig...)
		cloned[index].pluginReferenceKeys = append([]string(nil), definition.pluginReferenceKeys...)
		cloned[index].adminComponentConfig = append(json.RawMessage(nil), definition.adminComponentConfig...)
		cloned[index].rowLabelComponentConfig = append(json.RawMessage(nil), definition.rowLabelComponentConfig...)
		if definition.condition != nil {
			condition := cloneCondition(*definition.condition)
			cloned[index].condition = &condition
		}
		cloned[index].issues = append([]Issue(nil), definition.issues...)
		if definition.defaultValue != nil {
			value := *definition.defaultValue
			cloned[index].defaultValue = &value
		}
		if definition.minLength != nil {
			value := *definition.minLength
			cloned[index].minLength = &value
		}
		if definition.maxLength != nil {
			value := *definition.maxLength
			cloned[index].maxLength = &value
		}
		if definition.minimum != nil {
			value := *definition.minimum
			cloned[index].minimum = &value
		}
		if definition.maximum != nil {
			value := *definition.maximum
			cloned[index].maximum = &value
		}
		if definition.step != nil {
			value := *definition.step
			cloned[index].step = &value
		}
	}
	return cloned
}

func cloneCondition(condition Condition) Condition {
	condition.conditions = cloneConditions(condition.conditions)
	condition.values = append([]DefaultValue(nil), condition.values...)
	condition.issues = append([]Issue(nil), condition.issues...)
	return condition
}

func cloneConditions(conditions []Condition) []Condition {
	if conditions == nil {
		return nil
	}
	cloned := make([]Condition, len(conditions))
	for index, condition := range conditions {
		cloned[index] = cloneCondition(condition)
	}
	return cloned
}

func cloneRelationshipFilterRules(rules []RelationshipFilterRule) []RelationshipFilterRule {
	cloned := append([]RelationshipFilterRule(nil), rules...)
	for index, rule := range rules {
		if rule.Literal != nil {
			literal := *rule.Literal
			cloned[index].Literal = &literal
		}
	}
	return cloned
}

func cloneTabs(tabs []TabDefinition) []TabDefinition {
	if tabs == nil {
		return nil
	}
	cloned := make([]TabDefinition, len(tabs))
	for index, tab := range tabs {
		cloned[index] = tab
		cloned[index].LabelTranslations = cloneTranslations(tab.LabelTranslations)
		cloned[index].Fields = cloneDefinitions(tab.Fields)
	}
	return cloned
}

func cloneBlocks(blocks []Block) []Block {
	cloned := make([]Block, len(blocks))
	for index, block := range blocks {
		cloned[index] = block
		cloned[index].LabelTranslations = cloneTranslations(block.LabelTranslations)
		cloned[index].Fields = cloneDefinitions(block.Fields)
	}
	return cloned
}

func cloneChoices(choices []Choice) []Choice {
	cloned := make([]Choice, len(choices))
	for index, choice := range choices {
		cloned[index] = choice
		cloned[index].LabelTranslations = cloneTranslations(choice.LabelTranslations)
	}
	return cloned
}

func cloneTranslations(translations map[string]string) map[string]string {
	if translations == nil {
		return nil
	}
	cloned := make(map[string]string, len(translations))
	for language, value := range translations {
		cloned[language] = value
	}
	return cloned
}

func cloneRowLabels(labels RowLabels) RowLabels {
	labels.SingularTranslations = cloneTranslations(labels.SingularTranslations)
	labels.PluralTranslations = cloneTranslations(labels.PluralTranslations)
	return labels
}
