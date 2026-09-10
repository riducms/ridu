package primitivelists_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/tests/contracts/primitivelists/generated"
)

func TestGeneratedEmbeddedPrimitiveListOutputPreservesEmptyValues(t *testing.T) {
	body := `{"version":1,"root":{"type":"root","children":[{"type":"block","version":1,"fields":{"_key":"card-a","blockType":"card","points":[],"sizes":[0,10,10]}}]}}`
	for _, raw := range []string{`{"body":` + body + `}`, `{"localizedBody":{"fr":` + body + `}}`} {
		var document generated.PrimitiveProductAllLocales
		if err := json.Unmarshal([]byte(raw), &document); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		var before, after map[string]any
		if err := json.Unmarshal([]byte(raw), &before); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &after); err != nil {
			t.Fatal(err)
		}
		for name, value := range before {
			if !reflect.DeepEqual(after[name], value) {
				t.Fatalf("embedded %s lost list value: %s", name, encoded)
			}
		}
		invalid := strings.Replace(raw, `"points":[]`, `"points":[null]`, 1)
		if err := json.Unmarshal([]byte(invalid), &document); err == nil {
			t.Fatalf("accepted null embedded item: %s", invalid)
		}
	}
}
