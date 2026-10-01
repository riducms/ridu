package schema

import "sort"

// StorageSchema returns the manifest without the settings that never shape
// stored data. Two manifests with the same storage schema describe the same
// database, so migration history is compared through this projection and
// presentation changes need no migration. The projection clears:
//
//   - the application name, admin interface languages and timezones, admin
//     loaders, and custom endpoint metadata;
//   - labels and admin settings of collections, globals, block types and
//     fields, generated block type names, content-locale labels and direction,
//     select option labels and order, and other editor-only field settings;
//   - plugin build metadata: admin packages, endpoints and the generated Go and
//     TypeScript types of plugin fields.
//
// Everything an adapter or validation reads stays, including field types,
// validation rules, select option values, content locales and fallbacks,
// capabilities, auth settings and plugin configuration.
func (manifest Manifest) StorageSchema() Manifest {
	snapshot := manifest.Snapshot()
	application := &snapshot.Application
	application.Name, application.NameTranslations = "", nil
	application.AdminLoaders, application.AdminLocalization, application.Endpoints = nil, nil, nil
	if localization := application.Localization; localization != nil {
		for index := range localization.Locales {
			localization.Locales[index].Label, localization.Locales[index].RTL = "", false
		}
	}
	clearPresentationBlocks(snapshot.Blocks)
	for _, resources := range [][]Collection{snapshot.Collections, snapshot.Globals} {
		for index := range resources {
			resources[index].Labels = CollectionLabels{}
			resources[index].Admin = CollectionAdmin{}
			resources[index].Endpoints = nil
			clearPresentationFields(resources[index].Fields)
		}
	}
	for index := range snapshot.Plugins {
		plugin := &snapshot.Plugins[index]
		plugin.GoPackage, plugin.Admin, plugin.Endpoints = "", nil, nil
		for fieldIndex := range plugin.FieldTypes {
			fieldType := &plugin.FieldTypes[fieldIndex]
			fieldType.TypeScriptPackage, fieldType.TypeScriptOutput, fieldType.TypeScriptInput, fieldType.TypeScriptWhere = "", "", "", ""
			fieldType.GoPackage, fieldType.GoType = "", ""
		}
	}
	// Rebind the detached graph to the cleared registry without materializing
	// placement views or copying the snapshot again.
	_ = BindBlockReferences(&snapshot)
	return Manifest{snapshot: snapshot}
}

// SameStorage reports whether two manifests have the same storage schema.
func (manifest Manifest) SameStorage(other Manifest) bool {
	return manifest.StorageSchema().Equal(other.StorageSchema())
}

func clearPresentationFields(fields []Field) {
	for index := range fields {
		field := &fields[index]
		field.Admin = FieldAdmin{}
		if field.Join != nil {
			field.Join.DefaultColumns, field.Join.AllowCreate = nil, nil
		}
		if field.Number != nil {
			field.Number.Step = nil
		}
		if field.Code != nil {
			field.Code.Language = ""
		}
		if field.Select != nil {
			for option := range field.Select.Options {
				field.Select.Options[option].Label, field.Select.Options[option].LabelTranslations = "", nil
			}
			// Allowed values form a set; selected and default values stay ordered data.
			sort.Slice(field.Select.Options, func(i, j int) bool {
				return field.Select.Options[i].Value < field.Select.Options[j].Value
			})
		}
		if field.Nested != nil {
			field.Nested.RowLabel, field.Nested.RowLabelComponent, field.Nested.RowLabels = "", nil, nil
			clearPresentationFields(field.Nested.Fields)
		}
		if field.Blocks != nil {
			clearPresentationBlocks(field.Blocks.Types)
		}
		if field.Plugin != nil {
			for tree := range field.Plugin.EmbeddedTrees {
				for variant := range field.Plugin.EmbeddedTrees[tree].Cases {
					clearPresentationBlocks(field.Plugin.EmbeddedTrees[tree].Cases[variant].Types)
				}
			}
		}
	}
}

func clearPresentationBlocks(blocks []BlockType) {
	for index := range blocks {
		blocks[index].Labels = BlockLabels{}
		blocks[index].Admin = nil
		blocks[index].TypeName = ""
		clearPresentationFields(blocks[index].Fields)
	}
}
