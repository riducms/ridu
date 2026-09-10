package field

import "encoding/json"

// Kind identifies a built-in authoring field definition.
type Kind string

const (
	KindText         Kind = "text"
	KindTextList     Kind = "text-list"
	KindNumberList   Kind = "number-list"
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

// Option is one allowed value and its author-facing label for a select field.
type Option struct {
	// Value is the exact string stored in documents and sent over APIs.
	Value string
	// Label is the author-facing name shown in selection controls.
	// When omitted, resolution generates it from Value.
	Label string
	// LabelTranslations overrides Label for configured admin interface languages.
	LabelTranslations map[string]string
}

// BlockAdmin configures the ordinary block row heading.
type BlockAdmin struct {
	// RowLabelPath names a direct stored scalar child; empty values use the block type label.
	RowLabelPath string
}

// BlockLabels overrides the singular and plural names derived from a block slug.
// Translations select admin interface language, independently of content locale.
type BlockLabels struct {
	Singular             string
	Plural               string
	SingularTranslations map[string]string
	PluralTranslations   map[string]string
}

// Block is one discriminated layout allowed by a blocks field.
type Block struct {
	// TypeName optionally names a reusable generated type family; it never changes the stored slug.
	TypeName string
	// Slug is the stable discriminator stored with block values.
	Slug string
	// Labels overrides the singular and plural names shown to authors.
	Labels BlockLabels
	// Fields defines the values stored by this block type.
	Fields Fields
	// Admin configures this block type in the framework admin.
	Admin BlockAdmin
}

// Snapshot returns a detached block, copying child fields and label translations.
// Executable callbacks retain their original behavior.
func (block Block) Snapshot() Block {
	block.Fields = block.Fields.Snapshot()
	block.Labels = cloneBlockLabels(block.Labels)
	return block
}

// RowLabels contains optional singular and plural display names for array rows.
// RowLabelPath remains the separate child-field path used to derive each row heading.
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
	reference  Reference
	operator   ConditionOperator
	values     []DefaultValue
	issues     []Issue
}

// Kind reports whether this node is a logical group, negation, or predicate.
func (condition Condition) Kind() ConditionKind { return condition.kind }

// Conditions returns detached child expressions for All, Any, and Not nodes.
func (condition Condition) Conditions() []Condition { return cloneConditions(condition.conditions) }

// Reference returns the predicate's authored field selector.
func (condition Condition) Reference() Reference { return condition.reference }

// IsZero reports that no visibility condition was supplied.
func (condition Condition) IsZero() bool { return condition.kind == "" }

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

// RelationshipFilterOperator is the finite predicate vocabulary used to narrow admin options.
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

// DefaultKind identifies the logical value carried by a field default.
type DefaultKind string

const (
	DefaultList    DefaultKind = "list"
	DefaultString  DefaultKind = "string"
	DefaultNumber  DefaultKind = "number"
	DefaultBoolean DefaultKind = "boolean"
)

// DefaultValue is the normalized literal value produced by Default.
type DefaultValue struct {
	kind DefaultKind
	text string
}

// DateFormat selects a date field's accepted value contract. The admin control
// follows this format; changing presentation never changes accepted API values.
type DateFormat string

const (
	DateOnly DateFormat = "date"
	DateTime DateFormat = "date-time"
	TimeOnly DateFormat = "time"
)

// Kind reports whether the normalized default is a string, number, boolean, or primitive list.
func (value DefaultValue) Kind() DefaultKind { return value.kind }

// String returns the canonical textual encoding written to the manifest.
func (value DefaultValue) String() string { return value.text }

// View is a read-only snapshot of a canonical immutable node. Its readers
// detach mutable values; author fields through their concrete fluent facades.
type View struct{ nodeData }

// nodeData is the canonical, privately owned field representation.
// Concrete facades and read-only views share this representation.
type nodeData struct {
	graph                 *graphPolicies
	name                  string
	kind                  Kind
	label                 string
	required              bool
	unique                bool
	index                 bool
	localized             bool
	minLength             *int
	maxLength             *int
	minimum               *float64
	maximum               *float64
	step                  *float64
	defaultValue          *DefaultValue
	dateFormat            DateFormat
	slugSource            string
	slugConfigured        bool
	options               []Option
	selectMany            bool
	selectDefaults        []string
	relationTo            []string
	relationMany          bool
	fields                Fields
	blocks                []Block
	blockReferences       []string
	referencesBound       bool
	pluginKey             string
	pluginConfig          json.RawMessage
	pluginReferenceKeys   []string
	pluginTrees           []EmbeddedTree
	minRows               int
	maxRows               int
	joinCollection        string
	joinOn                string
	joinLimit             int
	joinDefaultColumns    []string
	joinDefaultSort       string
	joinAllowCreate       bool
	joinCreateConfigured  bool
	valueType             ValueType
	relationshipFilters   []RelationshipFilterRule
	referenceDeleteAction ReferenceDeleteAction
	issues                []Issue
	admin                 Admin
}

// Name returns the document property name authored for the field.
func (d View) Name() string { return d.name }

// Kind returns the concrete authoring field kind.
func (d View) Kind() Kind { return d.kind }

// Label returns the explicit author-facing label, if one was configured.
func (d View) Label() string { return d.label }

// LabelTranslations returns localized author-facing labels by admin language.
func (d View) LabelTranslations() map[string]string {
	return cloneTranslations(d.admin.LabelTranslations)
}

// Required reports whether validation rejects a missing, null, or field-type-specific empty value.
func (d View) Required() bool { return d.required }

// Unique reports whether values must be distinct within the collection.
func (d View) Unique() bool { return d.unique }

// Index reports whether the field should have a non-unique database index.
// Unique fields already receive a unique index even when this is false.
func (d View) Index() bool { return d.index }

// Localized reports whether the field stores an independent value for each
// configured application locale.
func (d View) Localized() bool { return d.localized }

// RelationshipTarget returns the first configured target slug, or an empty string.
func (d View) RelationshipTarget() string {
	if len(d.relationTo) == 0 {
		return ""
	}
	return d.relationTo[0]
}

// RelationshipTargets returns a copy of all configured target slugs.
func (d View) RelationshipTargets() []string { return append([]string(nil), d.relationTo...) }

// RelationshipHasMany reports whether the field stores multiple references.
func (d View) RelationshipHasMany() bool { return d.relationMany }

// SelectHasMany reports whether a select stores an ordered list of options.
func (d View) SelectHasMany() bool { return d.selectMany }

// SelectDefaults returns the configured literal default options for a multi-select.
// Dynamic defaults are executable policies and are not evaluated by this reader.
func (d View) SelectDefaults() []string {
	return append([]string(nil), d.selectDefaults...)
}

// Category returns the behavioral category for the definition's field kind.
func (d View) Category() Category {
	switch d.kind {
	case KindText, KindTextList, KindNumberList, KindCode, KindSelect, KindRadio, KindPoint, KindTextarea, KindEmail, KindDate, KindNumber, KindCheckbox, KindJSON:
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

// Default returns the configured typed literal default. List defaults use canonical JSON arrays.
// Dynamic defaults are executable policies and are not evaluated by this reader.
func (d View) Default() (DefaultValue, bool) {
	if d.defaultValue == nil {
		return DefaultValue{}, false
	}
	return *d.defaultValue, true
}

// SlugSource returns the source field path for a text-backed slug helper.
// The boolean distinguishes an invalid empty source from an ordinary text field.
func (d View) SlugSource() (string, bool) { return d.slugSource, d.slugConfigured }

// Options returns a copy of the select options.
func (d View) Options() []Option {
	return cloneOptions(d.options)
}

// Fields returns a deep copy of the nested child definitions.
func (d View) Fields() Fields {
	return d.fields.Snapshot()
}

// Blocks returns a deep copy of the allowed block types.
func (d View) Blocks() []Block { return cloneBlocks(d.blocks) }

// PluginKey returns the compiled plugin responsible for a custom field.
func (d View) PluginKey() string { return d.pluginKey }

// AdminComponent returns the optional statically registered admin plugin
// renderer selected for this field. The returned configuration is detached
// from the immutable definition.
func (d View) AdminComponent() (pluginKey, component string, config json.RawMessage, ok bool) {
	c := d.admin.Editor
	if c.PluginKey == "" {
		return "", "", nil, false
	}
	return c.PluginKey, c.Key, componentConfig(c), true
}

// Description returns the configured author-facing supporting text.
func (d View) Description() string { return d.admin.Description }

// DescriptionTranslations returns localized supporting text by admin language.
func (d View) DescriptionTranslations() map[string]string {
	return cloneTranslations(d.admin.DescriptionTranslations)
}

// Placeholder returns the canonical empty-state prompt for compatible admin controls.
func (d View) Placeholder() string { return d.admin.Placeholder }

// PlaceholderTranslations returns localized placeholder text by admin language.
func (d View) PlaceholderTranslations() map[string]string {
	return cloneTranslations(d.admin.PlaceholderTranslations)
}

// ReadOnly reports whether the admin should prevent editing this field.
func (d View) ReadOnly() bool { return d.admin.ReadOnly }

// Hidden reports whether the admin should omit this field from presentation.
func (d View) Hidden() bool { return d.admin.Hidden }

// Sidebar reports whether a root field belongs in the document editor's right rail.
func (d View) Sidebar() bool { return d.admin.Sidebar }

// Columns returns the requested one-to-twelve-column admin grid width, or zero.
func (d View) Columns() int { return d.admin.Columns }

// Tab returns the named admin form tab, or an empty string.
func (d View) Tab() string { return d.admin.Tab }

// TabTranslations returns localized direct-tab labels by admin language.
func (d View) TabTranslations() map[string]string { return cloneTranslations(d.admin.TabTranslations) }

// CodeLanguage is the editor language hint for a code field.
func (d View) CodeLanguage() string { return d.admin.CodeLanguage }

// MinLength returns the minimum accepted Unicode code-point length.
func (d View) MinLength() (int, bool) {
	if d.minLength == nil {
		return 0, false
	}
	return *d.minLength, true
}

// MaxLength returns the maximum accepted Unicode code-point length.
func (d View) MaxLength() (int, bool) {
	if d.maxLength == nil {
		return 0, false
	}
	return *d.maxLength, true
}

// Min returns the inclusive minimum accepted by a number field.
func (d View) Min() (float64, bool) {
	if d.minimum == nil {
		return 0, false
	}
	return *d.minimum, true
}

// Max returns the inclusive maximum accepted by a number field.
func (d View) Max() (float64, bool) {
	if d.maximum == nil {
		return 0, false
	}
	return *d.maximum, true
}

// Step returns the positive admin input increment for a number field. It is
// presentation metadata and does not impose divisibility validation.
func (d View) Step() (float64, bool) {
	if d.step == nil {
		return 0, false
	}
	return *d.step, true
}

// DateFormat returns the accepted date value format, defaulting to DateOnly.
func (d View) DateFormat() DateFormat {
	if d.dateFormat == "" {
		return DateOnly
	}
	return d.dateFormat
}

// MinRows is the minimum accepted length for an array or blocks list.
func (d View) MinRows() int { return d.minRows }

// MaxRows is the maximum accepted length for an array or blocks list, or zero when unbounded.
func (d View) MaxRows() int { return d.maxRows }

// RowLabel is the child property used to label array rows in the admin.
func (d View) RowLabel() string { return d.admin.RowLabelPath }

// RowLabelComponent returns the optional statically registered admin plugin
// component selected for array or blocks row headings. The returned
// configuration is detached from the immutable definition.
func (d View) RowLabelComponent() (pluginKey, component string, config json.RawMessage, ok bool) {
	c := d.admin.RowLabel
	if c.PluginKey == "" {
		return "", "", nil, false
	}
	return c.PluginKey, c.Key, componentConfig(c), true
}

// RowLabels returns optional singular and plural author-facing array row names.
func (d View) RowLabels() RowLabels { return cloneRowLabels(d.admin.RowLabels) }

// InitiallyCollapsed reports whether a collapsible presentation group starts closed.
func (d View) InitiallyCollapsed() bool { return d.admin.InitiallyCollapsed }

// JoinCollection is the collection queried by an inverse join.
func (d View) JoinCollection() string { return d.joinCollection }

// JoinOn is the target relationship path matched against the source document ID.
func (d View) JoinOn() string { return d.joinOn }

// JoinLimit is the maximum related documents returned for an inverse join.
func (d View) JoinLimit() int { return d.joinLimit }

// JoinDefaultColumns returns the target fields shown by the inverse-join table.
func (d View) JoinDefaultColumns() []string {
	return append([]string(nil), d.joinDefaultColumns...)
}

// JoinDefaultSort returns the target sort expression, including an optional descending prefix.
func (d View) JoinDefaultSort() string { return d.joinDefaultSort }

// JoinAllowCreate reports whether the admin may offer inline target creation.
func (d View) JoinAllowCreate() bool { return d.joinAllowCreate }

// ValueType is the declared output contract for a virtual field.
func (d View) ValueType() ValueType { return d.valueType }

// RelationshipFilters returns configured multi-rule and polymorphic option filters.
func (d View) RelationshipFilters() []RelationshipFilterRule {
	return cloneRelationshipFilterRules(d.relationshipFilters)
}

// ReferenceDeleteAction returns the explicitly authored hard-delete policy.
// An empty action means config resolution must apply the deterministic default
// for the reference shape.
func (d View) ReferenceDeleteAction() ReferenceDeleteAction {
	return d.referenceDeleteAction
}

// PluginConfig returns a copy of the plugin-owned serialized configuration.
func (d View) PluginConfig() json.RawMessage {
	return append(json.RawMessage(nil), d.pluginConfig...)
}

// PluginReferenceKeys returns JSON property names whose string values contain
// public collection slugs and therefore participate in content renames and
// persisted reference-shape migration safety.
func (d View) PluginReferenceKeys() []string {
	return append([]string(nil), d.pluginReferenceKeys...)
}

// Issues validates the current declaration without executing behavior.
func (d View) Issues() []Issue {
	issues := append([]Issue(nil), d.issues...)
	issues = append(issues, d.shapeIssues()...)
	issues = append(issues, d.metadataIssues()...)
	if behavior := d.graphPolicy().behavior; behavior != nil {
		issues = append(issues, behavior.issues()...)
	}
	return issues
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

func cloneBlocks(blocks []Block) []Block {
	cloned := make([]Block, len(blocks))
	for index, block := range blocks {
		cloned[index] = block.Snapshot()
	}
	return cloned
}

func cloneBlockLabels(labels BlockLabels) BlockLabels {
	labels.SingularTranslations = cloneTranslations(labels.SingularTranslations)
	labels.PluralTranslations = cloneTranslations(labels.PluralTranslations)
	return labels
}

func optionsFromValues(values []string) []Option {
	options := make([]Option, len(values))
	for index, value := range values {
		options[index] = Option{Value: value}
	}
	return options
}

func cloneOptions(options []Option) []Option {
	cloned := make([]Option, len(options))
	for index, option := range options {
		cloned[index] = option
		cloned[index].LabelTranslations = cloneTranslations(option.LabelTranslations)
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
