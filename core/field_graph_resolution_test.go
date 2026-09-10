package core

import (
	"encoding/json"

	configresolver "github.com/riducms/ridu/internal/config"
	"github.com/riducms/ridu/schema"
)

// testFieldGraphResolution keeps metadata assertions at the private resolution
// boundary. Applications receive only the portable schema through Resolve.
type testFieldGraphResolution struct {
	manifest schema.Manifest
	graph    configresolver.Graph
}

func resolveTestFieldGraph(config Config) (testFieldGraphResolution, error) {
	working, manifest, err := resolveConfig(config)
	if err != nil {
		return testFieldGraphResolution{}, err
	}
	return testFieldGraphResolution{manifest: manifest, graph: working.fieldGraph}, nil
}

func (r testFieldGraphResolution) Manifest() schema.Manifest { return r.manifest }
func (r testFieldGraphResolution) Occurrences() []configresolver.Occurrence {
	return r.graph.Occurrences()
}
func (r testFieldGraphResolution) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Manifest    schema.Manifest             `json:"manifest"`
		Occurrences []configresolver.Occurrence `json:"occurrences"`
	}{r.manifest, r.graph.Occurrences()})
}
