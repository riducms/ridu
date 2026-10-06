package protocol_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
)

func TestAdminPreparedRuntimeEncodedManifestKeepsTheWireForm(t *testing.T) {
	runtime := protocol.AdminPreparedRuntimeV1{
		AdminPreparedNavigationV1: protocol.AdminPreparedNavigationV1{
			CollectionOperations: map[string]protocol.OperationCapabilities{"posts": {Read: true}},
			GlobalOperations:     map[string]protocol.OperationCapabilities{},
			ContentLocale:        "en",
		},
		Manifest:    schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "Encoded"}, Plugins: []schema.Plugin{}},
		Theme:       "system",
		Preferences: map[string]json.RawMessage{},
	}
	plain, err := json.Marshal(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.EncodedManifest, err = json.Marshal(runtime.Manifest); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(&runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, encoded) {
		t.Fatalf("encoded manifest changed the wire form:\n%s\n%s", plain, encoded)
	}
	var decoded protocol.AdminPreparedRuntimeV1
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Manifest.Application.Name != "Encoded" || decoded.EncodedManifest != nil || !decoded.CollectionOperations["posts"].Read {
		t.Fatalf("decoded runtime = %#v", decoded)
	}
}
