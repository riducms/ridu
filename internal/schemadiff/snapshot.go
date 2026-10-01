package schemadiff

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/riducms/ridu/schema"
)

// ReadManifest reads one optional canonical manifest file. The file is
// indented, and a parsed manifest keeps that indentation inside raw plugin
// field configuration, where a structural comparison reads it as a changed
// field. The manifest is therefore parsed again from its compact encoding,
// which is the form migrations store and a resolved config produces.
func ReadManifest(path string) (schema.Manifest, bool, error) {
	encoded, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return schema.Manifest{}, false, nil
	}
	if err != nil {
		return schema.Manifest{}, false, err
	}
	manifest, err := schema.Parse(encoded)
	if err == nil {
		manifest, err = CompactManifest(manifest)
	}
	if err != nil {
		return schema.Manifest{}, false, fmt.Errorf("parse schema manifest %s: %w", path, err)
	}
	return manifest, true, nil
}

// CompactManifest normalizes raw plugin configuration before structural
// comparison, regardless of whether a snapshot came from a file or a database.
func CompactManifest(manifest schema.Manifest) (schema.Manifest, error) {
	compact, err := json.Marshal(manifest)
	if err != nil {
		return schema.Manifest{}, err
	}
	return schema.Parse(compact)
}
