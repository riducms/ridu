package schema_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
)

func TestParseHistoricalPreservesBlockSchemas(t *testing.T) {
	for _, carrier := range []string{"inline", "registered", "embedded"} {
		for _, previousName := range []string{"missing name", "number child", "retired metadata"} {
			t.Run(carrier+"/"+previousName, func(t *testing.T) {
				block := field.Block{Slug: "card", Fields: field.Fields{field.Text("title")}}
				if previousName == "retired metadata" {
					block.Admin.RowLabelPath = "title"
				}
				config := ridu.Config{Name: "Historical blocks", Collections: []ridu.Collection{{Slug: "pages"}}}
				switch carrier {
				case "inline":
					config.Collections[0].Fields = field.Fields{field.Blocks("layout", block)}
				case "registered":
					config.Blocks = []field.Block{block}
					config.Collections[0].Fields = field.Fields{field.Blocks("layout").References("card")}
				case "embedded":
					config.Plugins = []ridu.Plugin{richtext.New()}
					config.Collections[0].Fields = field.Fields{richtext.Field("body", richtext.Config{Blocks: []field.Block{block}})}
				}
				current, err := ridu.Resolve(config)
				if err != nil {
					t.Fatal(err)
				}
				snapshot := current.Snapshot()
				var historical *schema.BlockType
				switch carrier {
				case "inline":
					historical = &snapshot.Collections[0].Fields[0].Blocks.Types[0]
				case "registered":
					historical = &snapshot.Blocks[0]
				case "embedded":
					historical = &snapshot.Collections[0].Fields[0].Plugin.EmbeddedTrees[0].Cases[0].Types[0]
				}
				if previousName == "number child" {
					name := &historical.Fields[1]
					name.Type, name.Text, name.Default = schema.FieldTypeNumber, nil, nil
					name.Number = &schema.NumberField{}
				} else {
					historical.Fields = historical.Fields[:1]
				}
				encoded, err := schema.NewManifest(snapshot).Bytes()
				if err != nil {
					t.Fatal(err)
				}
				errorPath := ".fields.blockName"
				if previousName == "retired metadata" {
					encoded = bytes.ReplaceAll(encoded, []byte(`"rowLabel": "title"`), []byte(`"nameField": "title", "rowLabel": "title"`))
					// Reproduce the old canonical encoder's indentation and ordering.
					var canonical bytes.Buffer
					if err := json.Indent(&canonical, encoded, "", "  "); err != nil {
						t.Fatal(err)
					}
					encoded = canonical.Bytes()
					errorPath = ".admin.nameField"
				}
				if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), errorPath) {
					t.Fatalf("current parser accepted historical block metadata: %v", err)
				}
				parsed, err := schema.ParseHistorical(encoded)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := parsed.Bytes()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(encoded, actual) {
					t.Fatal("historical parsing changed canonical schema bytes")
				}
			})
		}
	}
}

func TestParseHistoricalRetainsFormatAndMetadataChecks(t *testing.T) {
	for _, encoded := range []string{
		`{"version":999,"application":{"name":"History"}}`,
		`{"version":1,"application":{"name":"History"},"unknown":true}`,
		`{"version":1,"application":{"name":"History"},"blocks":[{"slug":"card","nameField":"title"}]}`,
		`{"version":1,"application":{"name":"History"},"blocks":[{"slug":"card","admin":{"unknown":true}}]}`,
		`{"version":1,"application":{"name":"History"},"blocks":[{"slug":"card","admin":{"nameField":"missing"}}]}`,
		`{"version":1,"application":{"name":"History"},"collections":[{"slug":"pages","fields":[{"name":"rating","type":"number","category":"scalar","number":{"min":10,"max":1}}]}]}`,
	} {
		if _, err := schema.ParseHistorical([]byte(encoded)); err == nil {
			t.Fatalf("historical parsing accepted invalid metadata: %s", encoded)
		}
	}
	valid := schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "History"}})
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.ParseHistorical(append(encoded, encoded...)); err == nil {
		t.Fatal("historical parser accepted trailing JSON")
	}
}

func TestParseRejectsEmptyRecordedNamingProperty(t *testing.T) {
	for _, value := range []string{`""`, `null`} {
		encoded := []byte(`{"version":1,"application":{"name":"History"},"blocks":[{"slug":"card","admin":{"nameField":` + value + `}}]}`)
		if _, err := schema.Parse(encoded); err == nil || !strings.Contains(err.Error(), ".admin.nameField") {
			t.Fatalf("current parser accepted retired naming property %s: %v", value, err)
		}
		if _, err := schema.ParseHistorical(encoded); err == nil || !strings.Contains(err.Error(), ".admin.nameField") {
			t.Fatalf("historical parser accepted non-canonical naming property %s: %v", value, err)
		}
	}
}
