package typescript_test

import (
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/schema"
	"testing"
)

func TestAdminLoaderGeneratorRejectsMalformedManifest(t *testing.T) {
	for name, output := range map[string]schema.AdminDataType{
		"unknown kind":     {Kind: "script"},
		"missing element":  {Kind: "array"},
		"source injection": {Kind: "object", Fields: map[string]schema.AdminDataType{"x\"; alert(1); //": {Kind: "string"}}},
	} {
		t.Run(name, func(t *testing.T) {
			manifest := schema.NewManifest(schema.Snapshot{Application: schema.Application{AdminLoaders: []schema.AdminLoader{{Key: "dashboard", Input: schema.AdminDataType{Kind: "object"}, Output: output}}}})
			if code, err := typescript.Client(manifest); err == nil || code != nil {
				t.Fatalf("generated malformed loader: %s %v", code, err)
			}
		})
	}
}
