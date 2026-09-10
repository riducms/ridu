package field_test

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
	"testing"
)

func TestLocalRowLabelSnapshotIsolation(t *testing.T) {
	config := store.Object(store.Values{"title": store.String("Heading")})
	for _, node := range (field.Fields{
		field.Array("rows", field.Fields{field.Text("title")}).Admin(field.Admin{RowLabel: field.Component("app:summary", config)}),
		field.Blocks("blocks", field.Block{Slug: "copy", Fields: field.Fields{field.Text("title")}}).Admin(field.Admin{RowLabel: field.Component("app:summary", config)}),
	}) {
		definition := field.Snapshot(node)
		reference, raw := definition.LocalRowLabel()
		if reference != "app:summary" || string(raw) != `{"title":"Heading"}` {
			t.Fatal(reference, string(raw))
		}
		raw[2] = 'X'
		_, fresh := definition.LocalRowLabel()
		if string(fresh) != `{"title":"Heading"}` {
			t.Fatal("row label configuration leaked")
		}
	}
	for _, component := range []field.ComponentRef{field.Component("invalid"), field.Component("app:summary", store.Boolean(true)), field.Component("app:summary", store.Null())} {
		if len(field.Snapshot(field.Array("rows", nil).Admin(field.Admin{RowLabel: component})).Issues()) == 0 {
			t.Fatal("malformed selection accepted")
		}
	}
}
