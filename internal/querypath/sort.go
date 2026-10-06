package querypath

import (
	"errors"

	"github.com/riducms/ridu/internal/membership"
	"github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

// ValidateSort accepts a path to one stored scalar of collection, reached
// directly or through unlocalized groups. The operation engine rejects every
// other caller sort with it, and configuration rejects a join's default sort,
// so both agree on what can be ordered. The error explains why a path cannot
// be sorted.
func ValidateSort(collection schema.Collection, path query.Path) error {
	switch path.String() {
	case "id", "createdAt", "updatedAt":
		return nil
	case "_status":
		if collection.Versions != nil {
			return nil
		}
		return errors.New("only a versioned collection has a status to order by")
	case "_revision":
		if collection.Versions != nil || collection.Upload != nil {
			return nil
		}
		return errors.New("only a versioned or upload collection has a revision to order by")
	}
	segments := path.Segments()
	for length := 1; length <= len(segments); length++ {
		prefix, err := query.NewPath(segments[:length]...)
		if err != nil {
			continue
		}
		field, found := population.FieldAtPath(collection.Fields, prefix)
		if !found {
			continue // Block discriminators are not fields; Blocks fail below.
		}
		last := length == len(segments)
		switch {
		case field.Type == schema.FieldTypeArray || field.Type == schema.FieldTypeBlocks:
			return errors.New("sort paths cannot traverse or end at an array or blocks field: a document has no single value there to order by")
		case field.Type == schema.FieldTypeJSON || field.Type == schema.FieldTypePlugin:
			return errors.New("JSON and plugin values cannot be sorted: they have no single scalar to order by; sort by an ordinary field")
		case field.Type == schema.FieldTypeGroup && field.Localized && !last:
			return errors.New("sort paths cannot pass through a localized group: each locale holds its own group; sort by a localized field instead")
		case field.Type == schema.FieldTypeGroup && last:
			return errors.New("a group has no single value to order by; sort by a field inside it")
		case last && membership.KindOf(field) != membership.None:
			return errors.New("lists, has-many fields and polymorphic relationships hold several items or a reference, not a single value to order by")
		case last && !sortableLeaf(field):
			return errors.New("this field has no stored scalar value to order by")
		}
	}
	return nil
}

func sortableLeaf(field schema.Field) bool {
	if field.Category == schema.FieldCategoryPresentation {
		return false
	}
	switch field.Type {
	case schema.FieldTypeText, schema.FieldTypeTextarea, schema.FieldTypeEmail, schema.FieldTypeCode,
		schema.FieldTypeDate, schema.FieldTypeRadio, schema.FieldTypeSelect, schema.FieldTypeNumber,
		schema.FieldTypeCheckbox, schema.FieldTypeRelationship, schema.FieldTypeUpload:
		return true
	default:
		return false
	}
}
