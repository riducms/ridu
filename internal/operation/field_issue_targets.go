package operation

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// fieldIssueTargets projects only schema-owned values, using the same portable
// schema/key chain as the form controller. Display indices and callback binding
// IDs never identify an issue. An exact-locale form supplies its own locale scope;
// all-locales callers receive explicitly qualified tokens for translated values.
type fieldIssueLocation struct {
	target  string
	fieldID schema.StableID
	locale  schema.LocaleCode
	// list reports a primitive list field.
	list bool
}

func fieldIssueTargets(collection Collection, values store.Values, allLocales bool) map[string]string {
	result := make(map[string]string)
	for path, location := range fieldIssueLocations(collection, values, allLocales, "") {
		result[path] = location.target
	}
	return result
}

// Track locale while traversing schema structure. User keys are arbitrary strings
// and must never be interpreted as the markers used in the opaque wire token.
// Tokens name fields by placement: a shared block definition's fields take the
// stable IDs of the placement the walk reached them through.
func fieldIssueLocations(collection Collection, values store.Values, allLocales bool, locale schema.LocaleCode) map[string]fieldIssueLocation {
	result := make(map[string]fieldIssueLocation)
	add := func(path string, token []string, field schema.Field, fieldID schema.StableID, locale schema.LocaleCode) {
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(token)
		result[path] = fieldIssueLocation{target: strings.TrimSuffix(buffer.String(), "\n"), fieldID: fieldID, locale: locale, list: primitivefield.IsList(field)}
	}
	id := func(f schema.Field, placement fieldPlacement) schema.StableID {
		if placement.shared {
			return schema.PlacementFieldID(collection.Schema.ID, placement.canonical)
		}
		return f.ID
	}
	var record func([]schema.Field, store.Value, string, []string, fieldPlacement, schema.LocaleCode)
	var visit func(schema.Field, store.Value, string, []string, fieldPlacement, bool, schema.LocaleCode)
	record = func(fields []schema.Field, values store.Value, path string, token []string, parent fieldPlacement, locale schema.LocaleCode) {
		for _, f := range fields {
			placement := parent.child(f.Name)
			visit(f, values.Get(f.Name), joinFieldPath(path, f.Name), appendIssueToken(token, string(id(f, placement))), placement, false, locale)
		}
	}
	visit = func(f schema.Field, value store.Value, path string, token []string, placement fieldPlacement, translated bool, locale schema.LocaleCode) {
		fieldID := id(f, placement)
		add(path, token, f, fieldID, locale)
		if allLocales && f.Localized && !translated {
			for code, item := range value.Entries() {
				visit(f, item, joinFieldPath(path, code), appendIssueToken(token, "@locale", code), placement, true, schema.LocaleCode(code))
			}
			return
		}
		if f.Type == schema.FieldTypeGroup && f.Nested != nil {
			if value.Kind() == store.ValueObject {
				record(f.Nested.ResolvedFields(), value, path, token, placement, locale)
			}
		}
		if f.Type == schema.FieldTypeArray || f.Type == schema.FieldTypeBlocks {
			counts := make(map[string]int)
			for row := range value.Elements() {
				key, _ := row.Get("_key").StringValue()
				counts[key]++
			}
			i := -1
			for row := range value.Elements() {
				i++
				key, _ := row.Get("_key").StringValue()
				if key == "" || counts[key] != 1 {
					continue
				}
				var children []schema.Field
				caseKey := ""
				childPlacement := placement
				if f.Nested != nil {
					children = f.Nested.ResolvedFields()
				}
				if f.Blocks != nil {
					tag, _ := row.Get("blockType").StringValue()
					if block, found := f.Blocks.Definition(tag); found {
						caseKey, children = block.Slug, block.ResolvedFields()
						childPlacement = placement.enter(block.Slug)
					}
				}
				next := appendIssueToken(token, key, caseKey)
				rowPath := joinFieldPath(path, strconv.Itoa(i))
				add(rowPath, next, f, fieldID, locale)
				record(children, row, rowPath, next, childPlacement, locale)
			}
		}
		if f.Plugin != nil && len(f.Plugin.EmbeddedTrees) != 0 {
			occurrences, err := embedded.Occurrences(f, value, path, nil)
			if err != nil {
				return
			}
			for _, occurrence := range occurrences {
				next := appendIssueToken(token, occurrence.Tree.Key, occurrence.Case.TagValue, occurrence.Type.Slug, occurrence.Key)
				add(occurrence.RuntimePath, next, f, fieldID, locale)
				payload := placement.enter(occurrence.Tree.Key, occurrence.Case.TagValue, occurrence.Type.Slug)
				record(occurrence.Fields, store.Object(occurrence.Payload), occurrence.RuntimePath, next, payload, locale)
			}
		}
	}
	record(collection.Schema.Fields, store.Object(values), "", nil, collection.placement, locale)
	return result
}

func appendIssueToken(prefix []string, segments ...string) []string {
	return append(append([]string(nil), prefix...), segments...)
}

// correlatePrimitiveListIssues attaches the whole-list occurrence to built-in
// item diagnostics. The message may mention an item position; the target never
// does, so a client can invalidate the entire list's feedback after any edit.
func correlatePrimitiveListIssues(collection Collection, values store.Values, ctx Context, issues []schema.Issue) {
	if len(issues) == 0 {
		return
	}
	if plan := collection.runtimePlan().readOutput; plan == nil || !plan.lists {
		return
	}
	locations := fieldIssueLocations(collection, values, ctx.AllLocales, ctx.Locale)
	for i := range issues {
		location, found := locations[issues[i].Path]
		if !found || !location.list {
			continue
		}
		issues[i].Target = location.target
		issues[i].FieldID = location.fieldID
		issues[i].Locale = location.locale
		if collection.Schema.Capabilities.Global {
			issues[i].GlobalID = collection.Schema.ID
		} else {
			issues[i].CollectionID = collection.Schema.ID
		}
	}
}
