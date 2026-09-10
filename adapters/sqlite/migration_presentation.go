package sqlite

import (
	"reflect"
	"sort"

	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// sqliteWithoutPresentation projects a private copy onto the schema properties
// that may affect stored data. Keep the allowlist explicit: validation, content
// locale identities and fallbacks, capabilities and plugin configuration still
// compare normally. The complete original manifests remain in immutable history.
func sqliteWithoutPresentation(snapshot schema.Snapshot) schema.Snapshot {
	snapshot = schema.NewManifest(snapshot).Snapshot()
	snapshot.Application.Name = ""
	snapshot.Application.NameTranslations = nil
	sqliteClearEndpointPresentation(snapshot.Application.Endpoints)
	if settings := snapshot.Application.AdminLocalization; settings != nil {
		settings.DefaultLanguage = ""
		settings.DefaultTimeZone = ""
		for i := range settings.Languages {
			settings.Languages[i].Label = ""
			settings.Languages[i].LabelTranslations = nil
		}
		for i := range settings.TimeZones {
			settings.TimeZones[i].Label = ""
			settings.TimeZones[i].LabelTranslations = nil
		}
		// Picker order changes presentation, while membership remains significant.
		sort.Slice(settings.Languages, func(i, j int) bool { return settings.Languages[i].Code < settings.Languages[j].Code })
		sort.Slice(settings.TimeZones, func(i, j int) bool { return settings.TimeZones[i].ID < settings.TimeZones[j].ID })
	}
	if settings := snapshot.Application.Localization; settings != nil {
		for i := range settings.Locales {
			settings.Locales[i].Label = ""
			settings.Locales[i].RTL = false
		}
	}
	sqliteClearBlockPresentation(snapshot.Blocks)
	for _, resources := range [][]schema.Collection{snapshot.Collections, snapshot.Globals} {
		for i := range resources {
			resources[i].Labels = schema.CollectionLabels{}
			resources[i].Admin = schema.CollectionAdmin{}
			sqliteClearEndpointPresentation(resources[i].Endpoints)
			sqliteClearFieldPresentation(resources[i].Fields)
		}
	}
	// Rebind references to the normalized registry instead of retaining bindings
	// that captured the original display metadata.
	return schema.NewManifest(snapshot).Snapshot()
}

func sqliteClearFieldPresentation(fields []schema.Field) {
	for i := range fields {
		candidate := &fields[i]
		candidate.Admin = schema.FieldAdmin{}
		if candidate.Join != nil {
			candidate.Join.DefaultColumns = nil
			candidate.Join.AllowCreate = nil
		}
		if candidate.Number != nil {
			candidate.Number.Step = nil
		}
		if candidate.Code != nil {
			candidate.Code.Language = ""
		}
		if candidate.Select != nil {
			for j := range candidate.Select.Options {
				candidate.Select.Options[j].Label = ""
				candidate.Select.Options[j].LabelTranslations = nil
			}
			// Allowed values form a set; selected/default values remain ordered data.
			sort.Slice(candidate.Select.Options, func(i, j int) bool {
				return candidate.Select.Options[i].Value < candidate.Select.Options[j].Value
			})
		}
		if candidate.Nested != nil {
			candidate.Nested.RowLabel = ""
			candidate.Nested.RowLabelComponent = nil
			candidate.Nested.RowLabels = nil
			sqliteClearFieldPresentation(candidate.Nested.ResolvedFields())
		}
		if candidate.Blocks != nil {
			sqliteClearBlockPresentation(candidate.Blocks.ResolvedTypes())
		}
		if candidate.Plugin != nil {
			for j := range candidate.Plugin.EmbeddedTrees {
				for k := range candidate.Plugin.EmbeddedTrees[j].Cases {
					sqliteClearBlockPresentation(candidate.Plugin.EmbeddedTrees[j].Cases[k].ResolvedTypes())
				}
			}
		}
	}
}

func sqliteClearEndpointPresentation(endpoints []schema.Endpoint) {
	for i := range endpoints {
		endpoints[i].Summary = ""
	}
}

func sqliteClearBlockPresentation(blocks []schema.BlockType) {
	for i := range blocks {
		blocks[i].Labels = schema.BlockLabels{}
		blocks[i].Admin = nil
		blocks[i].TypeName = ""
		sqliteClearFieldPresentation(blocks[i].ResolvedFields())
	}
}

func sqlitePresentationOnlyArtifact(artifact migration.Artifact, before *schema.Manifest, after schema.Manifest) bool {
	if before == nil {
		return false
	}
	// An unchanged storage schema does not imply that compiled transforms or
	// plugin/auth upgrade steps leave derived data unchanged.
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != migration.StepAssertSchema {
				return false
			}
		}
	}
	return reflect.DeepEqual(sqliteWithoutPresentation(before.Snapshot()), sqliteWithoutPresentation(after.Snapshot()))
}
