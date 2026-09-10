package config

import (
	"encoding/json"
	"os"
	"testing"
)

func TestBlockLabelsMatchPayloadDefaults(t *testing.T) {
	// Captured from Payload formatLabels at fea6f8a47a50ff1330d8a5071b43e7dcffb97b22.
	// This fixture is portable; tests do not require Payload or JavaScript at runtime.
	encoded, err := os.ReadFile("testdata/block-labels.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Slug, Singular, Plural string }
	if err := json.Unmarshal(encoded, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Slug, func(t *testing.T) {
			actual := defaultBlockLabels(c.Slug)
			if actual.Singular != c.Singular || actual.Plural != c.Plural {
				t.Fatalf("got %#v; Payload labels: %q / %q", actual, c.Singular, c.Plural)
			}
		})
	}
}
