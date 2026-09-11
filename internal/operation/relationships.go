package operation

import (
	"errors"
	"fmt"
	"iter"
	"sort"
	"strconv"
	"strings"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type referenceKind string

const (
	relationshipReferenceKind referenceKind = "relationship"
	uploadReferenceKind       referenceKind = "upload"

	// MaxDocumentReferences bounds the relationship and upload work admitted by
	// one document mutation. The limit lives in the operation engine so local
	// calls, jobs, REST, GraphQL, and future transports cannot bypass it.
	MaxDocumentReferences = 512
)

type documentReference struct {
	target schema.RelationshipTarget
	id     string
	path   string
	locale schema.LocaleCode
	kind   referenceKind

	fieldIdentity string
	optionFilters []schema.RelationshipFilter
}

func (engine *Engine) validateDocumentReferences(ctx Context, transaction store.Transaction, values store.Values, selection localization.Selection) ([]schema.Issue, error) {
	references, limitIssue, err := engine.collectReferenceValidationCandidates(ctx.Collection.Fields, values, selection, ctx.projections)
	if err != nil {
		return nil, &Error{Code: "validation", Status: 422, Message: "localized reference validation failed", Cause: err}
	}
	if limitIssue != nil {
		return []schema.Issue{*limitIssue}, nil
	}
	type referenceCheck struct {
		reference documentReference
		selection localization.Selection
		filter    *query.Node
	}
	checks := make(map[string]referenceCheck, len(references))
	referenceKeys := make([][]string, len(references))
	for referenceIndex, reference := range references {
		selections, resolveError := engine.referenceValidationSelections(reference, selection)
		if resolveError != nil {
			return nil, &Error{Code: "validation", Status: 422, Message: "localized reference validation failed", Cause: resolveError}
		}
		for _, referenceSelection := range selections {
			sourceValues := values
			if selection.All && referenceSelection.Locale != "" {
				sourceValues = projectValues(ctx.projections, ctx.Collection.Fields, values, referenceSelection)
			}
			filter, filterIdentity, filterError := referenceOptionPredicate(reference, sourceValues)
			if filterError != nil {
				return nil, &Error{Code: "store_failed", Status: 500, Message: "reference option filter configuration is invalid", Cause: filterError}
			}
			key := documentReferenceKey(reference, referenceSelection, filterIdentity)
			checks[key] = referenceCheck{reference: reference, selection: referenceSelection, filter: filter}
			referenceKeys[referenceIndex] = append(referenceKeys[referenceIndex], key)
		}
	}
	keys := make([]string, 0, len(checks))
	for key := range checks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	checked := make(map[string]bool, len(keys))
	for _, key := range keys {
		check := checks[key]
		reference := check.reference
		referenceSelection := check.selection
		target, exists := engine.collections[string(reference.target.CollectionID)]
		if !exists || target.Schema.ID != reference.target.CollectionID || target.Schema.Slug != reference.target.CollectionSlug {
			return nil, &Error{Code: "store_failed", Status: 500, Message: "reference target collection is unavailable"}
		}
		targetContext := Context{
			Context: ctx.Context, Operation: operation.Read, Collection: target.Schema, ID: reference.id,
			Actor: cloneDocumentPointer(ctx.Actor), ActorCollection: ctx.ActorCollection, Data: store.Values{}, Locale: referenceSelection.Locale, AllLocales: referenceSelection.All,
			Locales: append([]schema.LocaleCode(nil), referenceSelection.Configured...),
		}
		decision, err := authorize(target, targetContext)
		if err != nil {
			return nil, &Error{Code: "access_failed", Status: 500, Message: "reference target access rule failed", Cause: err}
		}
		access := decision.Access
		if decision.Kind == Deny {
			access = denyAllAccessPredicate()
		}
		_, err = transaction.Find(ctx.Context, store.Request{
			Collection: target.Schema, Collections: engine.schemas, ID: reference.id,
			Filter: check.filter, Access: access, PublishedOnly: ctx.Actor == nil && target.Schema.Versions != nil,
			Lock: store.LockReference, Locales: referenceSelection.Configured,
			LocaleChain: referenceSelection.Chain, AllLocales: referenceSelection.All,
		})
		available := err == nil && decision.Kind != Deny
		switch {
		case err == nil:
		case errors.Is(err, store.ErrNotFound):
		default:
			return nil, translateStoreError(err)
		}
		checked[key] = available
	}
	var issues []schema.Issue
	for referenceIndex, reference := range references {
		available := true
		for _, key := range referenceKeys[referenceIndex] {
			if !checked[key] {
				available = false
				break
			}
		}
		if !available || len(referenceKeys[referenceIndex]) == 0 {
			issues = append(issues, unavailableReferenceIssue(reference))
		}
	}
	return issues, nil
}

func (engine *Engine) collectReferenceValidationCandidates(fields []schema.Field, values store.Values, selection localization.Selection, projections *localization.Projector) ([]documentReference, *schema.Issue, error) {
	if !selection.All || len(selection.Configured) == 0 {
		collector := newDocumentReferenceCollector()
		collectDocumentReferences(fields, values, "", referenceCollectionMode{}, collector)
		return collector.result()
	}

	// An all-locales admission decision is about every effective document view,
	// not only locale keys physically present in canonical storage. Project each
	// configured locale through its fallback chain so, for example, an `fr`
	// relationship inherited from `en` is still checked with the `fr` target
	// access rule and `fr` source predicates. The collector keeps the requested
	// locale in the issue path even when another locale supplied the value.
	var references []documentReference
	seen := make(map[string]struct{})
	for _, locale := range selection.Configured {
		localeSelection, err := localization.Resolve(engine.localization, string(locale), nil, false, false)
		if err != nil {
			return nil, nil, err
		}
		projected := projectValues(projections, fields, values, localeSelection)
		collector := newDocumentReferenceCollector()
		collectDocumentReferences(fields, projected, "", referenceCollectionMode{projectedLocale: locale}, collector)
		localeReferences, limitIssue, _ := collector.result()
		if limitIssue != nil {
			return nil, limitIssue, nil
		}
		for _, reference := range localeReferences {
			key := referenceOccurrenceKey(reference)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			if len(references) == MaxDocumentReferences {
				return nil, maxDocumentReferencesIssue(reference.path), nil
			}
			references = append(references, reference)
		}
	}
	return references, nil, nil
}

func referenceOccurrenceKey(reference documentReference) string {
	return string(reference.target.CollectionID) + "\x00" + reference.id + "\x00" + reference.path + "\x00" +
		string(reference.locale) + "\x00" + string(reference.kind) + "\x00" + reference.fieldIdentity
}

func (engine *Engine) referenceValidationSelections(reference documentReference, selection localization.Selection) ([]localization.Selection, error) {
	if reference.locale != "" {
		resolved, err := localization.Resolve(engine.localization, string(reference.locale), nil, false, false)
		if err != nil {
			return nil, err
		}
		return []localization.Selection{resolved}, nil
	}
	if !selection.All || len(selection.Configured) == 0 {
		return []localization.Selection{selection}, nil
	}
	resolved := make([]localization.Selection, 0, len(selection.Configured))
	for _, locale := range selection.Configured {
		candidate, err := localization.Resolve(engine.localization, string(locale), nil, false, false)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, candidate)
	}
	return resolved, nil
}

func unavailableReferenceIssue(reference documentReference) schema.Issue {
	if reference.kind == uploadReferenceKind {
		return schema.Issue{Code: "invalid_upload", Path: reference.path, Message: "upload target is unavailable"}
	}
	return schema.Issue{Code: "invalid_relationship", Path: reference.path, Message: "relationship target is unavailable"}
}

func documentReferenceKey(reference documentReference, selection localization.Selection, filterIdentity string) string {
	return string(reference.target.CollectionID) + "\x00" + reference.id + "\x00" + string(selection.Locale) + "\x00" +
		string(reference.kind) + "\x00" + reference.fieldIdentity + "\x00" + filterIdentity
}

func referenceOptionPredicate(reference documentReference, source store.Values) (*query.Node, string, error) {
	type configuredFilter struct {
		identity string
		filter   schema.RelationshipFilter
	}
	configured := make([]configuredFilter, 0, len(reference.optionFilters))
	for index, filter := range reference.optionFilters {
		configured = append(configured, configuredFilter{identity: "optionFilters:" + strconv.Itoa(index), filter: filter})
	}
	if len(configured) != 0 && reference.fieldIdentity == "" {
		return nil, "", fmt.Errorf("filtered reference field requires a canonical identity")
	}

	expressions := make([]query.Expression, 0, len(configured))
	var identity strings.Builder
	for _, candidate := range configured {
		filter := candidate.filter
		operator := filter.Operator
		hasSource := filter.SourcePath != nil
		hasValue := filter.Value != nil
		if filter.TargetPath.String() == "" || hasSource == hasValue {
			return nil, "", fmt.Errorf("%s on field %q requires a target path and exactly one source path or literal value", candidate.identity, reference.fieldIdentity)
		}
		queryOperator, valid := referenceQueryOperator(operator)
		if !valid {
			return nil, "", fmt.Errorf("%s on field %q uses unsupported operator %q", candidate.identity, reference.fieldIdentity, operator)
		}
		if filter.CollectionSlug != "" && filter.CollectionSlug != reference.target.CollectionSlug {
			continue
		}
		identity.WriteString(candidate.identity)
		identity.WriteByte(':')
		identity.WriteString(string(filter.CollectionSlug))
		identity.WriteByte(':')
		identity.WriteString(filter.TargetPath.String())
		identity.WriteByte(':')
		identity.WriteString(operator)
		identity.WriteByte(':')
		var queryValue query.Value
		var signature string
		var scalar bool
		if filter.SourcePath != nil {
			identity.WriteString(filter.SourcePath.String())
			identity.WriteByte('=')
			sourceValue, exists := referenceFilterSource(source, *filter.SourcePath)
			if !exists || sourceValue.Kind() == store.ValueNull {
				identity.WriteString("<missing>;")
				continue
			}
			queryValue, signature, scalar = referenceQueryValue(sourceValue)
		} else {
			identity.WriteString("literal=")
			queryValue, signature, scalar = referenceLiteralQueryValue(filter.Value)
		}
		identity.WriteString(signature)
		identity.WriteByte(';')
		if !scalar {
			continue
		}
		expression, err := query.Compare(filter.TargetPath, queryOperator, queryValue)
		if err != nil {
			return nil, "", fmt.Errorf("%s on field %q: %w", candidate.identity, reference.fieldIdentity, err)
		}
		expressions = append(expressions, expression)
	}
	if len(expressions) == 0 {
		return nil, identity.String(), nil
	}
	if len(expressions) == 1 {
		node := expressions[0].Node()
		return &node, identity.String(), nil
	}
	combined, err := query.And(expressions...)
	if err != nil {
		return nil, "", err
	}
	node := combined.Node()
	return &node, identity.String(), nil
}

func referenceLiteralQueryValue(value *schema.ScalarLiteral) (query.Value, string, bool) {
	if value == nil {
		return query.Value{}, "missing", false
	}
	decoded, err := value.Decode()
	if err != nil {
		return query.Value{}, "invalid-" + string(value.Type) + ":" + strconv.Quote(value.Value), false
	}
	switch value.Type {
	case schema.ValueTypeString:
		return decoded, "string:" + strconv.Quote(value.Value), true
	case schema.ValueTypeNumber:
		number, _ := decoded.NumberValue()
		return decoded, "number:" + strconv.FormatFloat(number, 'g', -1, 64), true
	case schema.ValueTypeBoolean:
		return decoded, "boolean:" + value.Value, true
	default:
		return query.Value{}, "invalid-type:" + string(value.Type), false
	}
}

func referenceFilterSource(values store.Values, path query.Path) (store.Value, bool) {
	segments := path.Segments()
	if len(segments) == 0 {
		return store.Value{}, false
	}
	value, exists := values[segments[0]]
	for _, segment := range segments[1:] {
		if !exists {
			return store.Value{}, false
		}
		value, exists = value.Lookup(segment)
	}
	return value, exists
}

func referenceQueryValue(value store.Value) (query.Value, string, bool) {
	switch value.Kind() {
	case store.ValueString:
		text, _ := value.StringValue()
		return query.String(text), "string:" + strconv.Quote(text), true
	case store.ValueNumber:
		number, _ := value.NumberValue()
		return query.Number(number), "number:" + strconv.FormatFloat(number, 'g', -1, 64), true
	case store.ValueBoolean:
		boolean, _ := value.BooleanValue()
		return query.Boolean(boolean), "boolean:" + strconv.FormatBool(boolean), true
	default:
		return query.Value{}, "non-scalar:" + string(value.Kind()), false
	}
}

func referenceQueryOperator(operator string) (query.Operator, bool) {
	switch operator {
	case "equals":
		return query.OperatorEqual, true
	case "notEquals":
		return query.OperatorNotEqual, true
	case "like":
		return query.OperatorLike, true
	case "contains":
		return query.OperatorContains, true
	case "greaterThan":
		return query.OperatorGreaterThan, true
	case "greaterThanEqual":
		return query.OperatorGreaterThanEqual, true
	case "lessThan":
		return query.OperatorLessThan, true
	case "lessThanEqual":
		return query.OperatorLessThanEqual, true
	default:
		return "", false
	}
}

type referenceCollectionMode struct {
	allLocales      bool
	inheritedLocale schema.LocaleCode
	projectedLocale schema.LocaleCode
}

type documentReferenceCollector struct {
	references     []documentReference
	exceededPath   string
	traversalError error
}

func newDocumentReferenceCollector() *documentReferenceCollector {
	return &documentReferenceCollector{references: make([]documentReference, 0, MaxDocumentReferences)}
}

func (collector *documentReferenceCollector) append(reference documentReference) bool {
	if len(collector.references) == MaxDocumentReferences {
		collector.exceededPath = reference.path
		return false
	}
	collector.references = append(collector.references, reference)
	return true
}

func (collector *documentReferenceCollector) full() bool {
	return collector.exceededPath != "" || collector.traversalError != nil
}

func (collector *documentReferenceCollector) result() ([]documentReference, *schema.Issue, error) {
	if collector.traversalError != nil {
		return nil, nil, collector.traversalError
	}
	if collector.full() {
		return nil, maxDocumentReferencesIssue(collector.exceededPath), nil
	}
	return collector.references, nil, nil
}

func maxDocumentReferencesIssue(path string) *schema.Issue {
	return &schema.Issue{
		Code: "max_references", Path: path,
		Message: fmt.Sprintf("a document may contain at most %d relationship and upload references", MaxDocumentReferences),
	}
}

func collectDocumentReferences(fields []schema.Field, values store.Values, prefix string, mode referenceCollectionMode, collector *documentReferenceCollector) {
	collectDocumentReferenceObject(fields, store.Object(values), prefix, mode, collector)
}

func collectDocumentReferenceObject(fields []schema.Field, values store.Value, prefix string, mode referenceCollectionMode, collector *documentReferenceCollector) {
	for _, field := range fields {
		if collector.full() {
			return
		}
		value, exists := values.Lookup(field.Name)
		if !exists || value.Kind() == store.ValueNull {
			continue
		}
		path := joinFieldPath(prefix, field.Name)
		if mode.projectedLocale != "" && field.Localized {
			unlocalized := field
			unlocalized.Localized = false
			localizedMode := mode
			localizedMode.inheritedLocale = mode.projectedLocale
			collectDocumentReferenceFieldValue(
				unlocalized, value, joinFieldPath(path, string(mode.projectedLocale)), localizedMode, collector,
			)
			continue
		}
		if mode.allLocales && field.Localized {
			if value.Kind() != store.ValueObject {
				continue
			}
			locales := make([]string, 0, value.Len())
			for locale := range value.Entries() {
				locales = append(locales, locale)
			}
			sort.Strings(locales)
			unlocalized := field
			unlocalized.Localized = false
			for _, locale := range locales {
				if collector.full() {
					return
				}
				localizedMode := mode
				localizedMode.inheritedLocale = schema.LocaleCode(locale)
				collectDocumentReferenceFieldValue(
					unlocalized, value.Get(locale), joinFieldPath(path, locale), localizedMode, collector,
				)
			}
			continue
		}
		collectDocumentReferenceFieldValue(field, value, path, mode, collector)
	}
}

func collectDocumentReferenceFieldValue(field schema.Field, value store.Value, path string, mode referenceCollectionMode, collector *documentReferenceCollector) {
	switch field.Type {
	case schema.FieldTypePlugin:
		err := embedded.Visit(field, value, path, nil, func(occurrence embedded.ReadOccurrence) error {
			collectDocumentReferenceObject(occurrence.Fields, occurrence.Payload, occurrence.RuntimePath, mode, collector)
			return nil
		})
		if err != nil {
			collector.traversalError = embeddedOperationError(err, false)
		}
	case schema.FieldTypeRelationship:
		collectRelationshipFieldReferences(field, value, path, mode.inheritedLocale, collector)
	case schema.FieldTypeUpload:
		collectUploadFieldReferences(field, value, path, mode.inheritedLocale, collector)
	case schema.FieldTypeGroup:
		if value.Kind() == store.ValueObject && field.Nested != nil {
			collectDocumentReferenceObject(field.Nested.ResolvedFields(), value, path, mode, collector)
		}
	case schema.FieldTypeArray:
		if value.Kind() != store.ValueList || field.Nested == nil {
			return
		}
		index := -1
		for item := range value.Elements() {
			index++
			if collector.full() {
				return
			}
			if item.Kind() == store.ValueObject {
				collectDocumentReferenceObject(field.Nested.ResolvedFields(), item, fmt.Sprintf("%s.%d", path, index), mode, collector)
			}
		}
	case schema.FieldTypeBlocks:
		if value.Kind() != store.ValueList || field.Blocks == nil {
			return
		}
		index := -1
		for item := range value.Elements() {
			index++
			if collector.full() {
				return
			}
			if item.Kind() != store.ValueObject {
				continue
			}
			blockKey, _ := item.Get("blockType").StringValue()
			if block := findBlock(field.Blocks.ResolvedTypes(), blockKey); block != nil {
				collectDocumentReferenceObject(block.ResolvedFields(), item, fmt.Sprintf("%s.%d", path, index), mode, collector)
			}
		}
	}
}

func collectRelationshipFieldReferences(field schema.Field, value store.Value, path string, locale schema.LocaleCode, collector *documentReferenceCollector) {
	if field.Relationship == nil {
		return
	}
	index := -1
	for item := range referenceElements(value, field.Relationship.HasMany) {
		index++
		if collector.full() {
			return
		}
		itemPath := path
		if field.Relationship.HasMany {
			itemPath = fmt.Sprintf("%s.%d", path, index)
		}
		if !field.Relationship.Polymorphic {
			id, valid := item.StringValue()
			if valid && id != "" {
				collector.append(documentReference{
					target: schema.RelationshipTarget{CollectionID: field.Relationship.CollectionID, CollectionSlug: field.Relationship.CollectionSlug},
					id:     id, path: itemPath, locale: locale, kind: relationshipReferenceKind,
					fieldIdentity: field.Path.String(),
					optionFilters: append([]schema.RelationshipFilter(nil), field.Relationship.OptionFilters...),
				})
			}
			continue
		}
		if item.Kind() != store.ValueObject {
			continue
		}
		slug, slugValid := item.Get("relationTo").StringValue()
		id, idValid := item.Get("id").StringValue()
		if !slugValid || !idValid {
			continue
		}
		for _, target := range field.Relationship.Targets {
			if string(target.CollectionSlug) == slug {
				collector.append(documentReference{
					target: target, id: id, path: itemPath, locale: locale, kind: relationshipReferenceKind,
					fieldIdentity: field.Path.String(),
					optionFilters: append([]schema.RelationshipFilter(nil), field.Relationship.OptionFilters...),
				})
				break
			}
		}
	}
}

func collectUploadFieldReferences(field schema.Field, value store.Value, path string, locale schema.LocaleCode, collector *documentReferenceCollector) {
	if field.Upload == nil {
		return
	}
	index := -1
	for item := range referenceElements(value, field.Upload.HasMany) {
		index++
		if collector.full() {
			return
		}
		id, valid := item.StringValue()
		if !valid || id == "" {
			continue
		}
		itemPath := path
		if field.Upload.HasMany {
			itemPath = fmt.Sprintf("%s.%d", path, index)
		}
		collector.append(documentReference{
			target: schema.RelationshipTarget{CollectionID: field.Upload.CollectionID, CollectionSlug: field.Upload.CollectionSlug},
			id:     id, path: itemPath, locale: locale, kind: uploadReferenceKind,
			fieldIdentity: field.Path.String(),
			optionFilters: append([]schema.RelationshipFilter(nil), field.Upload.OptionFilters...),
		})
	}
}

// referenceElements applies the singular/has-many field contract without
// materializing an intermediate slice for a read-only reference pass.
func referenceElements(value store.Value, many bool) iter.Seq[store.Value] {
	if many {
		return value.Elements()
	}
	return func(yield func(store.Value) bool) { yield(value) }
}
