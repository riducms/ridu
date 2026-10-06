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
	// roots[i] caches projections of fields[i]. A root is allocated when its
	// field is first projected and grows to its bound only for a second selection.
	roots []*projectionRoot
	// selections are owned copies of recent selections. Cache entries share
	// them because neither a copy nor its slices are ever mutated.
	selections []*Selection
	// scratch collects provenance while one root is projected.
	scratch localeSources
}

type projectionRoot struct {
	// source is the generation every entry projects.
	source store.Value
	// entries are projections of source, most recent first, bounded by
	// projectionEntriesPerField.
	entries []projectionEntry
}

type projectionEntry struct {
	selection *Selection
	value     store.Value
	visible   bool
	// sources is nil when no localized value supplied the projection.
	sources *localeSources
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
	projector.selections = nil
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
		projector.roots = make([]*projectionRoot, len(projector.fields))
	}
	var sources map[string]schema.LocaleCode
	var owned *Selection
	for index, field := range projector.fields {
		value, exists := values[field.Name]
		if !exists {
			// This can be a partial validation patch. Absence must not add a
			// cached field to the result or evict an unchanged document root.
			continue
		}
		root := projector.roots[index]
		if root == nil {
			root = &projectionRoot{}
			projector.roots[index] = root
		}
		entry, hit := root.cached(value, selection)
		if !hit {
			if owned == nil {
				owned = projector.ownedSelection(selection)
			}
			projector.scratch = localeSources{}
			projected, visible, _ := projectValueAt(field, value, *owned, field.Name, &projector.scratch)
			entry = projectionEntry{selection: owned, value: projected, visible: visible}
			if projector.scratch.len() != 0 {
				recorded := projector.scratch
				entry.sources = &recorded
			}
			projector.scratch = localeSources{}
			root.store(value, entry)
		}
		if entry.visible {
			values[field.Name] = entry.value
		} else {
			delete(values, field.Name)
		}
		if includeSources && entry.sources != nil {
			if sources == nil {
				sources = make(map[string]schema.LocaleCode, entry.sources.len())
			}
			entry.sources.copyInto(sources)
		}
	}
	return sources
}

// cached returns a retained projection of value for selection. Locale variants
// are kept only for the current source: earlier callback views retain their
// own immutable Values without the cache pinning old roots.
func (root *projectionRoot) cached(value store.Value, selection Selection) (projectionEntry, bool) {
	if len(root.entries) == 0 {
		return projectionEntry{}, false
	}
	if !root.source.SameBacking(value) {
		clear(root.entries)
		root.entries = root.entries[:0]
		root.source = store.Value{}
		return projectionEntry{}, false
	}
	for index, entry := range root.entries {
		if sameProjectionSelection(*entry.selection, selection) {
			copy(root.entries[1:index+1], root.entries[:index])
			root.entries[0] = entry
			return entry, true
		}
	}
	return projectionEntry{}, false
}

// store records entry for source as the most recent projection, evicting the
// least recently used selection when the bound is reached.
func (root *projectionRoot) store(source store.Value, entry projectionEntry) {
	root.source = source
	switch {
	case len(root.entries) < cap(root.entries):
		root.entries = root.entries[:len(root.entries)+1]
	case len(root.entries) == 0:
		// Most roots see one selection; grow to the bound only for a second.
		root.entries = make([]projectionEntry, 1)
	case len(root.entries) < projectionEntriesPerField:
		grown := make([]projectionEntry, len(root.entries)+1, projectionEntriesPerField)
		copy(grown, root.entries)
		clear(root.entries)
		root.entries = grown
	}
	copy(root.entries[1:], root.entries[:len(root.entries)-1])
	root.entries[0] = entry
}

// ownedSelection returns a retained copy of selection whose slices no caller
// can mutate, reusing an equal copy so cache entries share one allocation.
func (projector *Projector) ownedSelection(selection Selection) *Selection {
	for _, owned := range projector.selections {
		if sameProjectionSelection(*owned, selection) {
			return owned
		}
	}
	copied := selection
	copied.Chain = slices.Clone(selection.Chain)
	copied.Configured = slices.Clone(selection.Configured)
	owned := &copied
	if len(projector.selections) == projectionEntriesPerField {
		// Entries holding an evicted copy keep it alive; only sharing ends.
		copy(projector.selections, projector.selections[1:])
		projector.selections[len(projector.selections)-1] = owned
	} else {
		projector.selections = append(projector.selections, owned)
	}
	return owned
}

func sameProjectionSelection(left, right Selection) bool {
	return left.Locale == right.Locale && left.All == right.All && left.PreserveNull == right.PreserveNull &&
		slices.Equal(left.Chain, right.Chain) && slices.Equal(left.Configured, right.Configured)
}
