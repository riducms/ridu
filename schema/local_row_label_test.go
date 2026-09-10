package schema

import (
	"encoding/json"
	"testing"
)

func TestLocalRowLabelMetadata(t *testing.T) {
	label := &FieldAdminComponent{Reference: "app:summary", Config: json.RawMessage(`{"title":"Heading"}`)}
	if err := validateRowLabelComponent(label, "rows", nil); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(label)
	if err != nil || string(encoded) != `{"reference":"app:summary","config":{"title":"Heading"}}` {
		t.Fatal(string(encoded), err)
	}
	var restored FieldAdminComponent
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if err := validateRowLabelComponent(&restored, "rows", nil); err != nil {
		t.Fatal(err)
	}
	if err := validatePairedAdminComponent(label, "field", nil, "field renderer"); err == nil {
		t.Fatal("local advanced field renderer accepted")
	}
	for _, invalid := range []FieldAdminComponent{{Reference: "app:summary", Plugin: "paired"}, {Reference: "app:summary", Component: "label"}, {Reference: "bad"}, {Reference: "app:summary", Config: json.RawMessage(`[]`)}} {
		if err := validateRowLabelComponent(&invalid, "rows", nil); err == nil {
			t.Fatal("invalid local label accepted", invalid)
		}
	}
}
