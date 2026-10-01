package schema

// WithoutAdminPresentation returns the manifest with every admin presentation
// setting cleared: the Admin settings of collections, globals, fields and
// block types. Those settings shape only the admin interface, never stored
// data, so two manifests that differ only in them describe the same
// database. Migration history is compared through this projection, which lets
// a project hide, regroup or relabel resources and fields in the admin
// without writing a migration.
func (manifest Manifest) WithoutAdminPresentation() Manifest {
	snapshot := manifest.Snapshot()
	clearAdminBlocks(snapshot.Blocks)
	for _, resources := range [][]Collection{snapshot.Collections, snapshot.Globals} {
		for index := range resources {
			resources[index].Admin = CollectionAdmin{}
			clearAdminFields(resources[index].Fields)
		}
	}
	// Rebind the detached graph to the cleared registry without materializing
	// placement views or copying the snapshot again.
	_ = BindBlockReferences(&snapshot)
	return Manifest{snapshot: snapshot}
}

// SameStorage reports whether two manifests differ at most in admin
// presentation.
func (manifest Manifest) SameStorage(other Manifest) bool {
	return manifest.WithoutAdminPresentation().Equal(other.WithoutAdminPresentation())
}

func clearAdminFields(fields []Field) {
	for index := range fields {
		field := &fields[index]
		field.Admin = FieldAdmin{}
		if field.Nested != nil {
			clearAdminFields(field.Nested.Fields)
		}
		if field.Blocks != nil {
			clearAdminBlocks(field.Blocks.Types)
		}
		if field.Plugin != nil {
			for tree := range field.Plugin.EmbeddedTrees {
				for variant := range field.Plugin.EmbeddedTrees[tree].Cases {
					clearAdminBlocks(field.Plugin.EmbeddedTrees[tree].Cases[variant].Types)
				}
			}
		}
	}
}

func clearAdminBlocks(blocks []BlockType) {
	for index := range blocks {
		blocks[index].Admin = nil
		clearAdminFields(blocks[index].Fields)
	}
}
