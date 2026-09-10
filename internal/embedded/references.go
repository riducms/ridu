package embedded

import (
	"strconv"
	"strings"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// RewriteCollectionReferences composes declared envelope references with the
// caller's ordinary payload traversal. The interpreter establishes boundaries
// first; the second bounded pass visits envelope JSON only, skipping every
// schema-owned payload (including JSON that resembles an envelope reference).
func RewriteCollectionReferences(field schema.Field, value store.Value, before, after string, transform func(Occurrence) (store.Values, error)) (store.Value, bool, error) {
	if field.Plugin == nil || len(field.Plugin.ReferenceKeys) == 0 {
		updated, err := Transform(field, value, field.Name, nil, transform)
		return updated, false, err
	}
	boundaries := &referenceBoundary{}
	budget := NewBudget()
	updated, err := Transform(field, value, "value", budget, func(o Occurrence) (store.Values, error) {
		boundary := boundaries
		for _, segment := range strings.Split(o.RuntimePath, ".")[1:] {
			if boundary.children == nil {
				boundary.children = map[string]*referenceBoundary{}
			}
			if boundary.children[segment] == nil {
				boundary.children[segment] = &referenceBoundary{}
			}
			boundary = boundary.children[segment]
		}
		boundary.payload = true
		return transform(o)
	})
	if err != nil {
		return value, false, err
	}
	keys := map[string]bool{}
	for _, key := range field.Plugin.ReferenceKeys {
		keys[key] = true
	}
	changed := false
	var walk func(store.Value, *referenceBoundary, string) (store.Value, error)
	walk = func(value store.Value, boundary *referenceBoundary, path string) (store.Value, error) {
		if boundary != nil && boundary.payload {
			return value, nil
		}
		if err := budget.Enter(path); err != nil {
			return value, err
		}
		defer budget.Leave()
		if object, ok := value.CopyObject(); ok {
			for key, child := range object {
				var next *referenceBoundary
				if boundary != nil {
					next = boundary.children[key]
				}
				if text, ok := child.StringValue(); ok && keys[key] && text == before {
					object[key], changed = store.String(after), true
					continue
				}
				updated, err := walk(child, next, join(path, key))
				if err != nil {
					return value, err
				}
				object[key] = updated
			}
			return store.Object(object), nil
		}
		if items, ok := value.CopyList(); ok {
			for i, child := range items {
				var next *referenceBoundary
				if boundary != nil {
					next = boundary.children[strconv.Itoa(i)]
				}
				updated, err := walk(child, next, join(path, strconv.Itoa(i)))
				if err != nil {
					return value, err
				}
				items[i] = updated
			}
			return store.List(items...), nil
		}
		return value, nil
	}
	updated, err = walk(updated, boundaries, field.Name)
	if err != nil {
		return value, false, err
	}
	return updated, changed, nil
}

type referenceBoundary struct {
	payload  bool
	children map[string]*referenceBoundary
}
