package operation

import (
	"errors"
	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/localization"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"strconv"
	"strings"
)

func embeddedOperationError(err error, recovery bool) error {
	if err == nil {
		return nil
	}
	var failure *embedded.Error
	if errors.As(err, &failure) {
		code, status, message := "validation", 422, "embedded document validation failed"
		if recovery {
			code, status, message = "block_recovery_required", 409, "This document requires embedded schema recovery. Restore the schema or migrate the stored document before using it."
		}
		return &Error{Code: code, Status: status, Message: message, Issues: []schema.Issue{failure.Issue}, Cause: err}
	}
	return err
}

// A patch trie applies all detached sibling changes with one traversal of each
// changed container, rather than cloning its whole envelope for every child.
type runtimePatch struct {
	children    map[string]*runtimePatch
	value       store.Value
	set, remove bool
}

func (p *runtimePatch) add(path string, value store.Value, remove bool) {
	current := p
	for _, segment := range strings.Split(path, ".") {
		if current.children == nil {
			current.children = map[string]*runtimePatch{}
		}
		if current.children[segment] == nil {
			current.children[segment] = &runtimePatch{}
		}
		current = current.children[segment]
	}
	current.value, current.set, current.remove = value, true, remove
}
func (p *runtimePatch) apply(values store.Values) {
	for name, patch := range p.children {
		if patch.set {
			if patch.remove {
				delete(values, name)
			} else {
				values[name] = patch.value
			}
			continue
		}
		value, exists := values[name]
		if !exists {
			continue
		}
		values[name] = patch.applyValue(value)
	}
}
func (p *runtimePatch) applyValue(value store.Value) store.Value {
	if object, ok := value.CopyObject(); ok {
		p.apply(object)
		return store.Object(object)
	}
	if value.Kind() == store.ValueList {
		if len(p.children) > 1 {
			// Dynamic defaults collect a batch before applying it. Rebuilding once
			// keeps that batch linear, without copying a leaf for every changed item.
			items, _ := value.CopyList()
			for name, child := range p.children {
				index, err := strconv.Atoi(name)
				if err != nil || index < 0 || index >= len(items) {
					continue
				}
				if child.set {
					if !child.remove {
						items[index] = child.value
					}
				} else {
					items[index] = child.applyValue(items[index])
				}
			}
			return store.List(items...)
		}
		// Copy only each changed item's persistent path. Materializing a detached
		// slice here would copy the entire width for every field hook, even though
		// callback snapshots can safely share all the unchanged list branches.
		for name, child := range p.children {
			index, err := strconv.Atoi(name)
			if err != nil {
				continue
			}
			item, ok := value.ListItem(index)
			if !ok {
				continue
			}
			if child.set {
				if !child.remove {
					value, _ = value.WithListItem(index, child.value)
				}
			} else {
				value, _ = value.WithListItem(index, child.applyValue(item))
			}
		}
		return value
	}
	return value
}

func originalFieldValues(ctx Context) store.Values {
	if ctx.originalCanonical != nil {
		selection := localization.Selection{Locale: ctx.Locale, All: ctx.AllLocales, Configured: append([]schema.LocaleCode(nil), ctx.Locales...), PreserveNull: true}
		if ctx.Locale != "" {
			selection.Chain = []schema.LocaleCode{ctx.Locale}
		}
		return projectValues(ctx.projections, ctx.Collection.Fields, ctx.originalCanonical.Values, selection)
	}
	if ctx.Original != nil {
		return store.CloneValues(ctx.Original.Values)
	}
	return nil
}
func originalFieldLocations(ctx Context, path string, values store.Values) map[string]fieldLocation {
	result := map[string]fieldLocation{}
	if values == nil {
		return result
	}
	for _, location := range fieldLocationsAtPath(ctx.Collection.Fields, values, path, ctx.AllLocales, true) {
		result[location.identity] = location
	}
	return result
}
