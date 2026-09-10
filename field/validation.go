package field

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

type declarationIssues struct{ issues []Issue }

func (d *declarationIssues) issue(code, path, message string) {
	d.issues = append(d.issues, Issue{Code: code, Path: path, Message: message})
}

func setDefault[T ~string | ~bool | ~float64](d View, value T) View {
	d = clearDefaultSettings(d)
	d = withPolicies[T, T](d, func(p *typedPolicies[T, T]) { p.defaultFrom = nil })
	normalized, err := normalizeDefault(value)
	if err != nil {
		d.issues = append(d.issues, Issue{Code: "invalid_default", Path: "default", Message: err.Error()})
	} else {
		d.defaultValue = &normalized
	}
	return d
}

func clearDefaultSettings(d View) View {
	d.defaultValue = nil
	d.selectDefaults = nil
	previous := d.issues
	d.issues = nil
	for _, issue := range previous {
		if issue.Path != "default" && issue.Path != "defaultFrom" {
			d.issues = append(d.issues, issue)
		}
	}
	return d
}

// shapeIssues validates the final graph state, so refining a value also clears
// any constraint inconsistency that the refinement resolves.
func (d View) shapeIssues() []Issue {
	issues := &declarationIssues{}
	add := issues.issue
	for _, bound := range []struct {
		path  string
		value *int
	}{{"minLength", d.minLength}, {"maxLength", d.maxLength}} {
		if bound.value != nil && *bound.value < 0 {
			add("invalid_length", bound.path, "field length must not be negative")
		}
	}
	if d.minLength != nil && d.maxLength != nil && *d.minLength > *d.maxLength {
		add("invalid_length_bounds", "minLength", "minimum length must not exceed maximum length")
	}
	for _, bound := range []struct {
		path  string
		value *float64
	}{{"min", d.minimum}, {"max", d.maximum}, {"step", d.step}} {
		if bound.value != nil && (math.IsNaN(*bound.value) || math.IsInf(*bound.value, 0)) {
			add("invalid_number", bound.path, "number constraint must be finite")
		}
	}
	if d.step != nil && *d.step <= 0 {
		add("invalid_step", "step", "number step must be positive")
	}
	if d.minimum != nil && d.maximum != nil && *d.minimum > *d.maximum {
		add("invalid_number_bounds", "min", "number minimum must not exceed maximum")
	}
	if d.minRows < 0 {
		add("invalid_min_rows", "minRows", "minimum rows must not be negative")
	}
	if d.maxRows < 0 {
		add("invalid_max_rows", "maxRows", "maximum rows must not be negative")
	}
	if d.maxRows > 0 && d.minRows > d.maxRows {
		add("invalid_row_bounds", "minRows", "minimum rows must not exceed maximum rows")
	}
	if value := d.defaultValue; value != nil {
		switch value.Kind() {
		case DefaultString:
			length := utf8.RuneCountInString(value.String())
			if d.minLength != nil && length < *d.minLength {
				add("invalid_default", "default", "field default is shorter than the configured minimum length")
			}
			if d.maxLength != nil && length > *d.maxLength {
				add("invalid_default", "default", "field default is longer than the configured maximum length")
			}
		case DefaultList:
			d.listDefaultIssues(issues)
		case DefaultNumber:
			number, err := strconv.ParseFloat(value.String(), 64)
			if err == nil && d.minimum != nil && number < *d.minimum {
				add("invalid_default", "default", "number default is below the configured minimum")
			}
			if err == nil && d.maximum != nil && number > *d.maximum {
				add("invalid_default", "default", "number default is above the configured maximum")
			}
		}
	}
	if d.admin.Placeholder != "" || len(d.admin.PlaceholderTranslations) > 0 {
		switch d.kind {
		case KindText, KindTextList, KindNumberList, KindTextarea, KindEmail, KindNumber, KindSelect, KindRelationship, KindUpload:
		default:
			add("incompatible_presentation", "admin.placeholder", "placeholder requires a compatible text, numeric, choice, or reference control")
		}
		if strings.TrimSpace(d.admin.Placeholder) == "" {
			add("invalid_placeholder", "admin.placeholder", "placeholder must provide non-blank canonical text")
		}
	}
	if d.admin.Columns != 0 && (d.admin.Columns < 1 || d.admin.Columns > 12) {
		add("invalid_columns", "admin.columns", "field columns must be between 1 and 12")
	}
	if d.admin.CodeLanguage != "" && d.kind != KindCode {
		add("incompatible_presentation", "admin.codeLanguage", "language requires a code field")
	}
	if d.dateFormat != "" {
		switch d.dateFormat {
		case DateOnly, DateTime, TimeOnly:
		default:
			add("invalid_date_format", "format", "date format must be date, date-time, or time")
		}
	}
	if d.referenceDeleteAction != "" && d.referenceDeleteAction != ReferenceDeleteNullify && d.referenceDeleteAction != ReferenceDeleteRestrict {
		add("invalid_reference_delete_action", "onDelete", "reference delete action must be nullify or restrict")
	}
	seen := map[string]bool{}
	for i, rule := range d.relationshipFilters {
		path := fmt.Sprintf("filterOptionRules[%d]", i)
		if rule.TargetPath == "" || (rule.SourcePath != "") == (rule.Literal != nil) {
			add("invalid_relationship_filter", path, "relationship filter requires a target path and exactly one source path or literal value")
		}
		if !validRelationshipFilterOperator(rule.Operator) {
			add("invalid_relationship_filter_operator", path+".operator", fmt.Sprintf("unsupported relationship filter operator %q", rule.Operator))
		}
		literal := ""
		if rule.Literal != nil {
			literal = string(rule.Literal.Kind()) + ":" + rule.Literal.String()
		}
		key := rule.Collection + "\x00" + rule.TargetPath + "\x00" + string(rule.Operator) + "\x00" + rule.SourcePath + "\x00" + literal
		if seen[key] {
			add("duplicate_relationship_filter", path, "relationship filter rule must be unique")
		}
		seen[key] = true
	}
	if d.kind == KindJoin {
		if d.joinLimit < 1 || d.joinLimit > 100 {
			add("invalid_join_limit", "limit", "join limit must be between 1 and 100")
		}
		seen := map[string]bool{}
		for i, path := range d.joinDefaultColumns {
			if strings.TrimSpace(path) == "" {
				add("invalid_join_column", fmt.Sprintf("defaultColumns[%d]", i), "join column must not be empty")
			}
			if seen[path] {
				add("duplicate_join_column", fmt.Sprintf("defaultColumns[%d]", i), "join column must be unique")
			}
			seen[path] = true
		}
		if d.joinDefaultSort != "" && strings.TrimPrefix(d.joinDefaultSort, "-") == "" {
			add("invalid_join_sort", "defaultSort", "join default sort must name a field path")
		}
	}
	if d.kind == KindPlugin && len(d.pluginConfig) > 0 && !json.Valid(d.pluginConfig) {
		add("invalid_plugin_config", "config", "plugin configuration must be valid JSON")
	}
	if d.kind == KindVirtual && !d.BehaviorSummary().Resolver {
		add("missing_computed_resolver", "resolver", "computed output requires an attached resolver")
	}
	if d.slugConfigured {
		if d.slugSource == "" {
			add("missing_slug_source", "sourcePath", "slug source path must not be empty")
		}
		if d.localized {
			add("unsupported_slug_localization", "localized", "slug fields cannot be localized; use separate explicit slug fields when locale-specific URLs are required")
		}
		if d.defaultValue != nil || d.BehaviorSummary().DynamicDefault {
			add("unsupported_slug_default", "default", "slug fields derive their initial value from the configured source and cannot declare a default")
		}
	}
	if !d.admin.VisibleWhen.IsZero() {
		nodes := 0
		validateCondition(issues, d.admin.VisibleWhen, "admin.visibleWhen", 0, &nodes)
	}
	for _, entry := range []struct {
		component ComponentRef
		path      string
	}{{d.admin.Editor, "admin.editor"}, {d.admin.RowLabel, "admin.rowLabel"}} {
		if err := entry.component.Err(); err != nil {
			add("invalid_admin_component", entry.path, err.Error())
		}
	}
	return issues.issues
}
