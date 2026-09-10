package localization

import (
	"slices"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const projectionEntriesPerField = 4

// Projector reuses recent root-field projections within one operation. Its
// resolved schema must remain immutable. A changed root is projected normally;
// only the latest source generation and its recent locale selections are kept.
// This cache does not index every nested item or retain an operation's history.
// Independent operations own independent projectors; a Projector is not intended
// for concurrent use. Call Clear when the operation finishes to release its values.
type Projector struct {
	fields []schema.Field
	roots  []projectionRoot
}

type projectionRoot struct {
	entries [projectionEntriesPerField]projectionEntry
}

type projectionEntry struct {
	valid     bool
	selection Selection
	source    store.Value
	value     store.Value
	visible   bool
	sources   map[string]schema.LocaleCode
}

// NewProjector borrows one resolved schema, which must remain immutable for the
// projector's lifetime. Cache storage is allocated only for active projections.
func NewProjector(fields []schema.Field) *Projector {
	return &Projector{fields: fields}
}

// Clear releases all cached source values, projections and provenance. The
// fixed schema remains available if the projector is used again.
func (projector *Projector) Clear() {
	projector.roots = nil
}

// Values returns a detached root map containing immutable projected values.
// Provenance stays private in the cache instead of being copied into a discarded
// Document on values-only paths.
func (projector *Projector) Values(values store.Values, selection Selection) store.Values {
	result := store.CloneValues(values)
	projector.project(result, selection, false)
	return result
}

// Document uses the current document's metadata and returns detached mutable
// root and provenance maps, including when all field projections are cache hits.
func (projector *Projector) Document(document store.Document, selection Selection) store.Document {
	if selection.Locale == "" && !selection.All {
		return store.CloneDocument(document)
	}
	// Selected projections replace provenance; do not copy stale input metadata
	// only to discard it. CloneDocument still owns the other mutable metadata.
	document.LocalizationSources = nil
	result := store.CloneDocument(document)
	result.LocalizationSources = projector.project(result.Values, selection, true)
	return result
}

// ValuesChecked retains embedded admission on every call, independently of any
// cached projection. All canonical locale branches are validated as usual.
func (projector *Projector) ValuesChecked(values store.Values, selection Selection) (store.Values, error) {
	if err := embedded.ValidateValues(projector.fields, values, "", true, nil); err != nil {
		return nil, err
	}
	return projector.Values(values, selection), nil
}

// DocumentChecked retains embedded admission on every call, independently of
// cached projections.
func (projector *Projector) DocumentChecked(document store.Document, selection Selection) (store.Document, error) {
	if err := embedded.ValidateValues(projector.fields, document.Values, "", true, nil); err != nil {
		return store.Document{}, err
	}
	return projector.Document(document, selection), nil
}

func (projector *Projector) project(values store.Values, selection Selection, includeSources bool) map[string]schema.LocaleCode {
	if selection.Locale == "" && !selection.All {
		return nil
	}
	if projector.roots == nil {
		projector.roots = make([]projectionRoot, len(projector.fields))
	}
	var sources map[string]schema.LocaleCode
	for index, field := range projector.fields {
		value, exists := values[field.Name]
		if !exists {
			// This can be a partial validation patch. Absence must not add a
			// cached field to the result or evict an unchanged document root.
			continue
		}
		entry := projector.roots[index].project(field, value, selection)
		if entry.visible {
			values[field.Name] = entry.value
		} else {
			delete(values, field.Name)
		}
		if includeSources && len(entry.sources) != 0 {
			if sources == nil {
				sources = make(map[string]schema.LocaleCode, len(entry.sources))
			}
			for path, locale := range entry.sources {
				sources[path] = locale
			}
		}
	}
	return sources
}

func (root *projectionRoot) project(field schema.Field, value store.Value, selection Selection) projectionEntry {
	// Keep locale variants only for the current source. Earlier callback views
	// retain their own immutable Values without the cache pinning old roots.
	if root.entries[0].valid && !root.entries[0].source.SameBacking(value) {
		*root = projectionRoot{}
	}
	for index, entry := range root.entries {
		if entry.valid && sameProjectionSelection(entry.selection, selection) && entry.source.SameBacking(value) {
			copy(root.entries[1:index+1], root.entries[:index])
			root.entries[0] = entry
			return entry
		}
	}
	sources := make(map[string]schema.LocaleCode)
	projected, visible, _ := projectValueAt(field, value, selection, field.Name, sources)
	if len(sources) == 0 {
		sources = nil
	}
	entry := projectionEntry{
		valid: true, selection: cloneProjectionSelection(selection),
		source: value, value: projected, visible: visible, sources: sources,
	}
	copy(root.entries[1:], root.entries[:projectionEntriesPerField-1])
	root.entries[0] = entry
	return entry
}

func sameProjectionSelection(left, right Selection) bool {
	return left.Locale == right.Locale && left.All == right.All && left.PreserveNull == right.PreserveNull &&
		slices.Equal(left.Chain, right.Chain) && slices.Equal(left.Configured, right.Configured)
}

func cloneProjectionSelection(selection Selection) Selection {
	selection.Chain = slices.Clone(selection.Chain)
	selection.Configured = slices.Clone(selection.Configured)
	return selection
}
