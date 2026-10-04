package schemadiff

import (
	"fmt"

	"github.com/riducms/ridu/schema"
)

// RejectVersionsEnable refuses to reinterpret an existing current document as
// a versioned working and live head. Renames contains confirmed collection ID
// continuity; an unmatched resource is new and may start versioned.
func RejectVersionsEnable(before, after schema.Snapshot, renames map[schema.StableID]schema.StableID) error {
	check := func(kind string, previous []schema.Collection, current []schema.Collection) error {
		byID := make(map[schema.StableID]schema.Collection, len(current))
		for _, resource := range current {
			byID[resource.ID] = resource
		}
		for _, resource := range previous {
			if resource.Versions != nil || resource.Capabilities.Versions {
				continue
			}
			id := resource.ID
			if mapped := renames[id]; mapped != "" {
				id = mapped
			}
			next, exists := byID[id]
			if exists && (next.Versions != nil || next.Capabilities.Versions) {
				return fmt.Errorf("RIDU_VERSIONS_ENABLE_UNSUPPORTED: cannot enable versions on existing %s %q; create a new versioned resource and explicitly import reviewed content instead", kind, resource.ID)
			}
		}
		return nil
	}
	if err := check("collection", before.Collections, after.Collections); err != nil {
		return err
	}
	return check("global", before.Globals, after.Globals)
}
