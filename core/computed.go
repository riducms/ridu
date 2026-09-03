package core

import (
	"context"
	"fmt"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// ComputedContext contains the trusted runtime inputs available to a virtual field resolver.
type ComputedContext struct {
	Context      context.Context
	Operation    Operation
	CollectionID schema.StableID
	GlobalID     schema.StableID
	Actor        *store.Document
	// ActorCollection identifies the exact auth collection that owns Actor.
	ActorCollection schema.CollectionSlug
	Document        store.Document
	Local           *LocalAPI
	Locale          schema.LocaleCode
	AllLocales      bool
}

// Computed resolves one virtual field. Its result is validated against the field's declared type.
type Computed func(ComputedContext) (store.Value, error)

func validateComputedRuntime(config Config, manifest schema.Manifest) error {
	snapshot := manifest.Snapshot()
	var issues []schema.Issue
	validate := func(prefix string, fields []schema.Field, resolvers map[string]Computed) {
		virtuals := make(map[string]struct{})
		for _, candidate := range fields {
			if candidate.Type == schema.FieldTypeVirtual {
				virtuals[candidate.Path.String()] = struct{}{}
				if resolvers[candidate.Path.String()] == nil {
					issues = append(issues, schema.Issue{Code: "missing_computed_resolver", Path: prefix + ".computed." + candidate.Path.String(), Message: "virtual field requires a computed resolver"})
				}
			}
		}
		for path, resolver := range resolvers {
			if _, exists := virtuals[path]; !exists {
				issues = append(issues, schema.Issue{Code: "unknown_computed_field", Path: prefix + ".computed." + path, Message: fmt.Sprintf("computed resolver %q does not target a virtual field", path)})
			} else if resolver == nil {
				issues = append(issues, schema.Issue{Code: "missing_computed_resolver", Path: prefix + ".computed." + path, Message: "computed resolver must not be nil"})
			}
		}
	}
	for index, collection := range config.Collections {
		validate(fmt.Sprintf("collections[%d]", index), snapshot.Collections[index].Fields, collection.Computed)
	}
	for index, global := range config.Globals {
		validate(fmt.Sprintf("globals[%d]", index), snapshot.Globals[index].Fields, global.Computed)
	}
	if len(issues) != 0 {
		return schema.NewValidationError(issues)
	}
	return nil
}
