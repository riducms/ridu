// Package plugintest provides the public backend conformance suite for Ridu plugins.
package plugintest

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Fixture describes the smallest real application surface needed to exercise
// a compiled plugin's manifest and generation contracts.
type Fixture struct {
	Plugin ridu.Plugin
	Fields field.Fields
	// ValidData and InvalidData opt into runtime storage, local API, REST, and
	// validator conformance. InvalidData must be rejected as validation.
	ValidData   store.Values
	InvalidData store.Values
	// Compatibility records the Ridu releases the plugin author promises to
	// test. Published plugin packages should retain old rows as they release.
	Compatibility []CompatibilityCase
}

// CompatibilityCase is one expected descriptor result in a plugin's
// cross-release test matrix.
type CompatibilityCase struct {
	RiduVersion string
	Compatible  bool
}

// Run executes the stable plugin-author conformance suite. Plugin packages
// should call it from an external-package test so import cycles and accidental
// reliance on private framework code are caught.
func Run(t *testing.T, fixture Fixture) {
	t.Helper()
	if fixture.Plugin == nil {
		t.Fatal("plugin conformance fixture requires a plugin")
	}
	t.Run("deterministic-manifest", func(t *testing.T) {
		config := ridu.Config{Name: "Plugin conformance", Plugins: []ridu.Plugin{fixture.Plugin}, Collections: []ridu.Collection{{Slug: "fixtures", Fields: fixture.Fields}}}
		first, err := ridu.Resolve(config)
		if err != nil {
			t.Fatal(err)
		}
		second, err := ridu.Resolve(config)
		if err != nil {
			t.Fatal(err)
		}
		firstBytes, _ := first.Bytes()
		secondBytes, _ := second.Bytes()
		if !bytes.Equal(firstBytes, secondBytes) {
			t.Fatal("plugin resolution is not deterministic")
		}
		if _, err := schema.Parse(firstBytes); err != nil {
			t.Fatalf("plugin manifest does not round-trip: %v", err)
		}
		assertFieldMappings(t, first.Snapshot(), fixture.Plugin.Key())
	})
	if len(fixture.Compatibility) != 0 {
		t.Run("compatibility-matrix", func(t *testing.T) {
			provider, ok := fixture.Plugin.(ridu.DescriptorProvider)
			if !ok {
				t.Fatal("compatibility matrix requires DescriptorProvider")
			}
			rangeDeclaration := provider.Descriptor().Ridu
			for _, test := range fixture.Compatibility {
				actual := schema.SemanticVersionInRange(test.RiduVersion, rangeDeclaration.Minimum, rangeDeclaration.MaximumExclusive)
				if actual != test.Compatible {
					t.Errorf("Ridu %s compatibility = %t, want %t", test.RiduVersion, actual, test.Compatible)
				}
			}
		})
	}
	if fixture.ValidData != nil {
		t.Run("runtime-and-transport", func(t *testing.T) {
			config := ridu.Config{Name: "Plugin conformance", Plugins: []ridu.Plugin{fixture.Plugin}, Collections: []ridu.Collection{{Slug: "fixtures", Fields: fixture.Fields}}}
			application, err := ridu.New(config, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := application.Local().Create(t.Context(), "fixtures", fixture.ValidData, nil); err != nil {
				t.Fatalf("local API rejected valid plugin data: %v", err)
			}
			body, err := json.Marshal(fixture.ValidData)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/collections/fixtures", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			application.Handler(ridu.HandlerOptions{}).ServeHTTP(response, request)
			if response.Code != http.StatusCreated {
				t.Fatalf("REST API rejected valid plugin data with %d: %s", response.Code, response.Body.String())
			}
			if fixture.InvalidData != nil {
				_, err := application.Local().Create(t.Context(), "fixtures", fixture.InvalidData, nil)
				var operationError *ridu.OperationError
				if !errors.As(err, &operationError) || operationError.Code != "validation" {
					t.Fatalf("local API invalid plugin data error = %#v, want validation", err)
				}
			}
		})
	}
}

func assertFieldMappings(t *testing.T, snapshot schema.Snapshot, key string) {
	t.Helper()
	declared := make(map[string]bool)
	for _, plugin := range snapshot.Plugins {
		for _, fieldType := range plugin.FieldTypes {
			declared[fieldType.Key] = true
		}
	}
	var inspect func([]schema.Field)
	inspect = func(fields []schema.Field) {
		for _, candidate := range fields {
			if candidate.Plugin != nil && candidate.Plugin.Key == key && !declared[candidate.Plugin.Key] {
				t.Errorf("plugin field %q has no generated type mapping", candidate.Plugin.Key)
			}
			if candidate.Nested != nil {
				inspect(candidate.Nested.ResolvedFields())
			}
			if candidate.Blocks != nil {
				for _, block := range candidate.Blocks.ResolvedTypes() {
					inspect(block.ResolvedFields())
				}
			}
		}
	}
	for _, collection := range snapshot.Collections {
		inspect(collection.Fields)
	}
}
