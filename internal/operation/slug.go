package operation

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func normalizeSlugFields(fields []schema.Field, data, submitted store.Values, original *store.Document, operation Kind) {
	for _, candidate := range fields {
		if candidate.Text == nil || candidate.Text.Slug == nil {
			continue
		}
		sourcePath := candidate.Text.Slug.SourcePath.Segments()
		source := mergedStringAtPath(data, originalValues(original), candidate.Text.Slug.SourcePath.Segments())
		generated := field.NormalizeSlug(source)
		baselineSource := mergedStringAtPath(submitted, originalValues(original), sourcePath)
		baselineGenerated := field.NormalizeSlug(baselineSource)
		submittedValue, explicitlySubmitted := submitted[candidate.Name]
		submittedSlug, submittedString := submittedValue.StringValue()
		currentValue, currentExists := data[candidate.Name]
		currentSlug, currentString := currentValue.StringValue()

		if operation == Create || operation == Duplicate || original == nil {
			if currentExists && !currentString {
				continue
			}
			normalizedCurrent := field.NormalizeSlug(currentSlug)
			switch {
			case explicitlySubmitted && !submittedString:
				continue
			case explicitlySubmitted && submittedSlug != "":
				// User-authored values stay manual even when a hook refines them.
				data[candidate.Name] = store.String(normalizedCurrent)
			case currentSlug != "" && normalizedCurrent != baselineGenerated:
				// A hook supplied a manual slug after the request entered the engine.
				data[candidate.Name] = store.String(normalizedCurrent)
			default:
				data[candidate.Name] = store.String(generated)
			}
			continue
		}

		originalSlug := stringAtPath(original.Values, []string{candidate.Name})
		originalSource := stringAtPath(original.Values, sourcePath)
		originalGenerated := originalSlug == field.NormalizeSlug(originalSource)
		if explicitlySubmitted {
			if !submittedString {
				continue
			}
			normalizedSubmission := field.NormalizeSlug(submittedSlug)
			if currentExists && !currentString {
				continue
			}
			normalizedCurrent := field.NormalizeSlug(currentSlug)
			if normalizedCurrent != normalizedSubmission {
				// A later hook deliberately replaced the submitted slug.
				data[candidate.Name] = store.String(normalizedCurrent)
				continue
			}
			if normalizedSubmission == baselineGenerated {
				// Submitting the current source value is the stateless regenerate signal.
				data[candidate.Name] = store.String(generated)
				continue
			}
			if normalizedSubmission != originalSlug {
				data[candidate.Name] = store.String(normalizedSubmission)
				continue
			}
		} else if currentExists {
			if !currentString {
				continue
			}
			normalizedCurrent := field.NormalizeSlug(currentSlug)
			if normalizedCurrent != originalSlug && normalizedCurrent != baselineGenerated {
				// A hook changed an omitted slug into an explicit manual value.
				data[candidate.Name] = store.String(normalizedCurrent)
				continue
			}
		}
		if originalSlug == "" || originalGenerated {
			if generated != originalSlug {
				data[candidate.Name] = store.String(generated)
			}
			continue
		}
		if current, exists := data[candidate.Name]; exists {
			if currentText, valid := current.StringValue(); valid {
				data[candidate.Name] = store.String(field.NormalizeSlug(currentText))
			}
		}
	}
}

func validateSlugUniqueness(
	ctx context.Context,
	transaction store.Transaction,
	collection schema.Collection,
	collections map[schema.StableID]schema.Collection,
	values store.Values,
	documentID string,
	locales []schema.LocaleCode,
) ([]schema.Issue, error) {
	var issues []schema.Issue
	for _, candidate := range collection.Fields {
		if candidate.Text == nil || candidate.Text.Slug == nil {
			continue
		}
		value, exists := values[candidate.Name]
		if !exists || value.Kind() == store.ValueNull {
			continue
		}
		slug, valid := value.StringValue()
		if !valid || slug == "" {
			continue
		}
		expression := query.Equal(candidate.Path, query.String(slug))
		node := expression.Node()
		page, err := transaction.List(ctx, store.Request{
			Collection: collection, Collections: collections, Filter: &node,
			Page: 1, Limit: 2, Deletion: store.DeletionActive, Locales: locales,
		})
		if err != nil {
			return nil, fmt.Errorf("check slug uniqueness for %q: %w", candidate.Path.String(), err)
		}
		for _, document := range page.Documents {
			if document.ID == documentID {
				continue
			}
			issues = append(issues, schema.Issue{
				Code: "unique", Path: candidate.Path.String(),
				Message: fmt.Sprintf("%s must be unique", candidate.Admin.Label),
			})
			break
		}
	}
	return issues, nil
}

func originalValues(document *store.Document) store.Values {
	if document == nil {
		return nil
	}
	return document.Values
}

func mergedStringAtPath(current, original store.Values, segments []string) string {
	if len(segments) == 0 {
		return ""
	}
	currentValue, currentExists := current[segments[0]]
	originalValue, originalExists := original[segments[0]]
	if len(segments) == 1 {
		if currentExists {
			value, _ := currentValue.StringValue()
			return value
		}
		if originalExists {
			value, _ := originalValue.StringValue()
			return value
		}
		return ""
	}
	if currentExists && currentValue.Kind() == store.ValueNull {
		return ""
	}
	var currentObject, originalObject store.Values
	if currentExists {
		currentObject, _ = currentValue.ObjectValue()
	}
	if originalExists {
		originalObject, _ = originalValue.ObjectValue()
	}
	return mergedStringAtPath(currentObject, originalObject, segments[1:])
}

func stringAtPath(values store.Values, segments []string) string {
	return mergedStringAtPath(values, nil, segments)
}
