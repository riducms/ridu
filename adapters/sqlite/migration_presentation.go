package sqlite

import "github.com/riducms/ridu/schema"

// sqliteStorageSchema is the private copy SQLite's planners compare: the
// storage schema, without settings that never shape stored data. The complete
// original manifests remain in immutable history.
func sqliteStorageSchema(snapshot schema.Snapshot) schema.Snapshot {
	return schema.NewManifest(snapshot).StorageSchema().Snapshot()
}
