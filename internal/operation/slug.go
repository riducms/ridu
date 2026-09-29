package operation

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func normalizeSlugFields(fields []schema.Field, data, submitted store.Values, original *store.Document, operationKind operation.Kind) {
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

		if operationKind == operation.Create || operationKind == operation.Duplicate || original == nil {
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
		expression := query.Equal(candidate.Path, slug)
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
		currentObject, _ = currentValue.CopyObject()
	}
	if originalExists {
		originalObject, _ = originalValue.CopyObject()
	}
	return mergedStringAtPath(currentObject, originalObject, segments[1:])
}

func stringAtPath(values store.Values, segments []string) string {
	return mergedStringAtPath(values, nil, segments)
}

// maxDuplicateSlugAttempts bounds the "-copy-N" search for one slug field.
const maxDuplicateSlugAttempts = 100

// uniqueDuplicateSlugs gives a duplicate a free slug instead of the original's.
// Slugs are unique, so a copy that keeps its source's slug can never be saved.
// A slug that hooks or the caller already changed is left alone. The lookup
// uses the write transaction, including trashed documents, and the database's
// unique constraint remains the final authority.
func uniqueDuplicateSlugs(ctx context.Context, transaction store.Transaction, collection schema.Collection, data store.Values, original *store.Document, locales []schema.LocaleCode) error {
	if original == nil {
		return nil
	}
	for _, candidate := range collection.Fields {
		if candidate.Text == nil || candidate.Text.Slug == nil {
			continue
		}
		current, _ := data[candidate.Name].StringValue()
		source, _ := original.Values[candidate.Name].StringValue()
		if current == "" || current != source {
			continue
		}
		stem := copySlugStem(current)
		for attempt := 1; attempt <= maxDuplicateSlugAttempts; attempt++ {
			next := stem + "-copy"
			if attempt > 1 {
				next = fmt.Sprintf("%s-copy-%d", stem, attempt)
			}
			taken, err := slugTaken(ctx, transaction, collection, candidate.Name, next, locales)
			if err != nil {
				return err
			}
			if !taken {
				data[candidate.Name] = store.String(next)
				break
			}
		}
	}
	return nil
}

// copySlugStem removes an earlier copy suffix, so copying "about-copy" gives
// "about-copy-2" rather than "about-copy-copy".
func copySlugStem(slug string) string {
	if stem, found := strings.CutSuffix(slug, "-copy"); found && stem != "" {
		return stem
	}
	if index := strings.LastIndex(slug, "-copy-"); index > 0 {
		if _, err := strconv.Atoi(slug[index+len("-copy-"):]); err == nil {
			return slug[:index]
		}
	}
	return slug
}

func slugTaken(ctx context.Context, transaction store.Transaction, collection schema.Collection, name, slug string, locales []schema.LocaleCode) (bool, error) {
	path, err := query.NewPath(name)
	if err != nil {
		return false, err
	}
	filter := query.Equal(path, slug).Node()
	page, err := transaction.List(ctx, store.Request{Collection: collection, Filter: &filter, Limit: 1, Deletion: store.DeletionAll, Locales: locales})
	if err != nil {
		return false, err
	}
	return len(page.Documents) != 0, nil
}
