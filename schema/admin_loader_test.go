package schema

import (
	"encoding/json"
	"testing"
)

func TestAdminLoaderManifestValidation(t *testing.T) {
	valid := AdminLoader{Key: "counts", Input: AdminDataType{Kind: "object"}, Output: AdminDataType{Kind: "number"}}
	for _, output := range []AdminDataType{{Kind: "array"}, {Kind: "injected();"}, {Kind: "number", Fields: map[string]AdminDataType{"field": {Kind: "string"}}}, {Kind: "object", Fields: map[string]AdminDataType{"__proto__": {Kind: "string"}}}} {
		loader := valid
		loader.Output = output
		encoded, err := json.Marshal(Snapshot{Version: CurrentVersion, Application: Application{Name: "Loaders", AdminLoaders: []AdminLoader{loader}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(encoded); err == nil {
			t.Fatalf("accepted malformed descriptor %#v", output)
		}
	}
	if err := ValidateAdminLoaders([]AdminLoader{valid, valid}); err == nil {
		t.Fatal("accepted duplicate loader keys")
	}
	if err := ValidateAdminLoaders([]AdminLoader{valid}); err != nil {
		t.Fatal(err)
	}
}
