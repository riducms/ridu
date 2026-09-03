package schemadiff

import (
	"fmt"
	"os"

	"github.com/riducms/ridu/schema"
)

// ReadManifest reads one optional canonical manifest file.
func ReadManifest(path string) (schema.Manifest, bool, error) {
	encoded, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return schema.Manifest{}, false, nil
	}
	if err != nil {
		return schema.Manifest{}, false, err
	}
	manifest, err := schema.Parse(encoded)
	if err != nil {
		return schema.Manifest{}, false, fmt.Errorf("parse schema manifest %s: %w", path, err)
	}
	return manifest, true, nil
}
