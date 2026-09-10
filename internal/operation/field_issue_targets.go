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
}

func fieldIssueTargets(fields []schema.Field, values store.Values, allLocales bool) map[string]string {
	result := make(map[string]string)
	for path, location := range fieldIssueLocations(fields, values, allLocales, "") {
		result[path] = location.target
	}
	return result
}

// Track locale while traversing schema structure. User keys are arbitrary strings
// and must never be interpreted as the markers used in the opaque wire token.
func fieldIssueLocations(fields []schema.Field, values store.Values, allLocales bool, locale schema.LocaleCode) map[string]fieldIssueLocation {
	result := make(map[string]fieldIssueLocation)
	add := func(path string, token []string, fieldID schema.StableID, locale schema.LocaleCode) {
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(token)
		result[path] = fieldIssueLocation{target: strings.TrimSuffix(buffer.String(), "\n"), fieldID: fieldID, locale: locale}
	}
	var record func([]schema.Field, store.Values, string, []string, schema.LocaleCode)
	var visit func(schema.Field, store.Value, string, []string, bool, schema.LocaleCode)
	record = func(fields []schema.Field, values store.Values, path string, token []string, locale schema.LocaleCode) {
		for _, f := range fields {
			visit(f, values[f.Name], joinFieldPath(path, f.Name), appendIssueToken(token, string(f.ID)), false, locale)
		}
	}
	visit = func(f schema.Field, value store.Value, path string, token []string, translated bool, locale schema.LocaleCode) {
		add(path, token, f.ID, locale)
		if allLocales && f.Localized && !translated {
			for code, item := range value.Entries() {
				visit(f, item, joinFieldPath(path, code), appendIssueToken(token, "@locale", code), true, schema.LocaleCode(code))
			}
			return
		}
		if f.Type == schema.FieldTypeGroup && f.Nested != nil {
			if value.Kind() == store.ValueObject {
				for _, child := range f.Nested.ResolvedFields() {
					visit(child, value.Get(child.Name), joinFieldPath(path, child.Name), appendIssueToken(token, string(child.ID)), false, locale)
				}
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
				if f.Nested != nil {
					children = f.Nested.ResolvedFields()
				}
				if f.Blocks != nil {
					tag, _ := row.Get("blockType").StringValue()
					for _, block := range f.Blocks.ResolvedTypes() {
						if block.Slug == tag {
							caseKey, children = block.Slug, block.ResolvedFields()
							break
						}
					}
				}
				next := appendIssueToken(token, key, caseKey)
				rowPath := joinFieldPath(path, strconv.Itoa(i))
				add(rowPath, next, f.ID, locale)
				for _, child := range children {
					visit(child, row.Get(child.Name), joinFieldPath(rowPath, child.Name), appendIssueToken(next, string(child.ID)), false, locale)
				}
			}
		}
		if f.Plugin != nil && len(f.Plugin.EmbeddedTrees) != 0 {
			occurrences, err := embedded.Occurrences(f, value, path, nil)
			if err != nil {
				return
			}
			for _, occurrence := range occurrences {
				next := appendIssueToken(token, occurrence.Tree.Key, occurrence.Case.TagValue, occurrence.Type.Slug, occurrence.Key)
				add(occurrence.RuntimePath, next, f.ID, locale)
				record(occurrence.Fields, occurrence.Payload, occurrence.RuntimePath, next, locale)
			}
		}
	}
	record(fields, values, "", nil, locale)
	return result
}

func appendIssueToken(prefix []string, segments ...string) []string {
	return append(append([]string(nil), prefix...), segments...)
}

// correlatePrimitiveListIssues attaches the whole-list occurrence to built-in
// item diagnostics. The message may mention an item position; the target never
// does, so a client can invalidate the entire list's feedback after any edit.
func correlatePrimitiveListIssues(collection schema.Collection, values store.Values, ctx Context, issues []schema.Issue) {
	if len(issues) == 0 {
		return
	}
	ids := map[string]bool{}
	var index func([]schema.Field)
	index = func(fields []schema.Field) {
		for _, field := range fields {
			if primitivefield.IsList(field) {
				ids[string(field.ID)] = true
			}
			index(schema.ChildFields(field))
		}
	}
	index(collection.Fields)
	if len(ids) == 0 {
		return
	}
	locations := fieldIssueLocations(collection.Fields, values, ctx.AllLocales, ctx.Locale)
	for i := range issues {
		location, found := locations[issues[i].Path]
		if !found || !ids[string(location.fieldID)] {
			continue
		}
		issues[i].Target = location.target
		issues[i].FieldID = location.fieldID
		issues[i].Locale = location.locale
		if collection.Capabilities.Global {
			issues[i].GlobalID = collection.ID
		} else {
			issues[i].CollectionID = collection.ID
		}
	}
}
