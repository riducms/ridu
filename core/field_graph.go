package core

import (
	"fmt"

	"github.com/riducms/ridu/field"
	configresolver "github.com/riducms/ridu/internal/config"
	"github.com/riducms/ridu/schema"
)

// FieldGraphContext identifies the resource a graph plugin is editing. Resource
// transforms complete first; graph transforms then run once in Plugins order.
// Resource hooks, endpoints and domain capabilities retain their own contracts.
type FieldGraphContext struct {
	ResourceKind string
	Slug         schema.CollectionSlug
}

// FieldGraphTransformer edits immutable fields through public graph operations.
// The input and result are snapshotted. Plugins should return deterministic
// edits; closure captures and other plugin-owned external state remain their
// responsibility. Duplicate plugin keys are rejected before any transform runs.
type FieldGraphTransformer interface {
	Plugin
	TransformFields(FieldGraphContext, field.Fields) (field.Fields, error)
}

// FieldGraphValidator checks a resource's final immutable field configuration
// after every graph transformer has completed. It cannot publish graph edits.
// Use this for plugin-owned schema and host settings, not document validation;
// runtime value validators belong to the field or plugin value contract.
type FieldGraphValidator interface {
	Plugin
	ValidateFields(FieldGraphContext, field.Fields) error
}

func validateGraphPluginKeys(plugins []Plugin) error {
	transforms := false
	for _, p := range plugins {
		if isNilPlugin(p) {
			continue
		}
		_, resource := p.(ConfigTransformer)
		_, graph := p.(FieldGraphTransformer)
		_, validator := p.(FieldGraphValidator)
		transforms = transforms || resource || graph || validator
	}
	// Preserve aggregate static validation for metadata-only plugins. A config
	// with executable transforms must reject duplicate keys before invoking one.
	if !transforms {
		return nil
	}
	seen := make(map[string]int, len(plugins))
	for i, p := range plugins {
		if isNilPlugin(p) {
			continue
		}
		if prior, ok := seen[p.Key()]; ok {
			return schema.NewValidationError([]schema.Issue{{Code: "duplicate_plugin_key", Path: fmt.Sprintf("plugins[%d]", i), Message: fmt.Sprintf("plugin %q already declared at plugins[%d]; transforms were not run", p.Key(), prior)}})
		}
		seen[p.Key()] = i
	}
	return nil
}

// composeFieldGraphs applies copy-safe graph edits before one resolution.
func composeFieldGraphs(working *Config, plugins []Plugin) error {
	for i := range working.Collections {
		if working.Collections[i].Upload {
			fields, err := configresolver.UploadFields(working.Collections[i].Fields, fmt.Sprintf("collections[%d].fields", i))
			if err != nil {
				return err
			}
			working.Collections[i].Fields = fields
		}
	}
	for pi, plugin := range plugins {
		transformer, ok := plugin.(FieldGraphTransformer)
		if !ok {
			continue
		}
		apply := func(context FieldGraphContext, graph field.Fields) (field.Fields, error) {
			result, err := transformer.TransformFields(context, graph.Snapshot())
			if err != nil {
				return nil, schema.NewValidationError([]schema.Issue{{Code: "plugin_field_graph_transform_failed", Path: fmt.Sprintf("plugins[%d]", pi), Message: fmt.Sprintf("plugin %q field graph transform failed for %s %q: %v", plugin.Key(), context.ResourceKind, context.Slug, err)}})
			}
			return result.WithProvenance("plugin:" + plugin.Key()), nil
		}
		for i := range working.Collections {
			c := &working.Collections[i]
			f, err := apply(FieldGraphContext{ResourceKind: "collection", Slug: c.Slug}, c.Fields)
			if err != nil {
				return err
			}
			c.Fields = f
		}
		for i := range working.Globals {
			g := &working.Globals[i]
			f, err := apply(FieldGraphContext{ResourceKind: "global", Slug: g.Slug}, g.Fields)
			if err != nil {
				return err
			}
			g.Fields = f
		}
	}
	for pi, plugin := range plugins {
		validator, ok := plugin.(FieldGraphValidator)
		if !ok {
			continue
		}
		validate := func(context FieldGraphContext, graph field.Fields) error {
			if err := validator.ValidateFields(context, graph.Snapshot()); err != nil {
				return schema.NewValidationError([]schema.Issue{{Code: "plugin_field_graph_validation_failed", Path: fmt.Sprintf("plugins[%d]", pi), Message: fmt.Sprintf("plugin %q field graph validation failed for %s %q: %v", plugin.Key(), context.ResourceKind, context.Slug, err)}})
			}
			return nil
		}
		for _, collection := range working.Collections {
			if err := validate(FieldGraphContext{ResourceKind: "collection", Slug: collection.Slug}, collection.Fields); err != nil {
				return err
			}
		}
		for _, global := range working.Globals {
			if err := validate(FieldGraphContext{ResourceKind: "global", Slug: global.Slug}, global.Fields); err != nil {
				return err
			}
		}
	}
	return nil
}
