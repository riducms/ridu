package field_test

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

func TestBlockRegistryTraversalAndAtomicMutation(t *testing.T) {
	block := field.Block{Slug: "card", Fields: field.Fields{field.Text("secret").Admin(field.Admin{VisibleWhen: field.Equal(field.Root("tenant"), "open")})}}
	registry, err := field.NewBlockRegistry(block)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := registry.Bind(field.Fields{field.Text("tenant"), field.Blocks("layout").References("card")})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	if err := fs.Walk(func(v field.Visit) error {
		if v.Node.Name() == "secret" {
			found = true
			if strings.Join(v.Path, ".") != "layout.card.secret" || v.DefinitionSlug != "card" {
				t.Fatalf("placement: %+v", v)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("referenced child not visited")
	}
	branch := field.Snapshot(fs[1]).Branches()[0]
	if !branch.Referenced || branch.Selector.Slug != "card" || branch.Fields[0].Name() != "secret" {
		t.Fatalf("read traversal: %+v", branch)
	}
	for _, edit := range []func(*field.ChildrenDraft) error{
		func(d *field.ChildrenDraft) error {
			return d.EditBlock("layout", "card", func(c *field.ChildrenDraft) error { return c.Rename("secret", "public") })
		},
		func(d *field.ChildrenDraft) error { return d.RenameRoot("tenant", "account") },
	} {
		if _, err := fs.Edit(edit); err == nil || !strings.Contains(err.Error(), "referenced_block_immutable") {
			t.Fatalf("mutation not rejected: %v", err)
		}
	}
	if field.Snapshot(fs[1]).Branches()[0].Fields[0].Name() != "secret" || fs[0].Name() != "tenant" {
		t.Fatal("failed edit changed graph")
	}
	if _, err := fs.Edit(func(d *field.ChildrenDraft) error { return d.RenameRoot("layout", "body") }); err != nil {
		t.Fatal("unrelated rename:", err)
	}
	block.Fields[0] = field.Text("changed")
	detached := registry.Blocks()
	detached[0].Fields[0] = field.Text("changed")
	if registry.Blocks()[0].Fields[0].Name() != "secret" {
		t.Fatal("registry aliases authored or returned slice")
	}
}

// Binding interns inline declarations by slug: one definition per slug, the
// same as a registration, and containers select it by reference.
func TestBlockRegistryInternsInlineDeclarations(t *testing.T) {
	note := field.Block{Slug: "note", Fields: field.Fields{field.Text("text")}}
	card := field.Block{Slug: "hero-card", Fields: field.Fields{field.Text("title"), field.Blocks("children", note)}}
	registry, err := field.NewBlockRegistry()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := registry.BindAt(field.Fields{field.Blocks("layout", card, note)}, "collections[0].fields")
	if err != nil {
		t.Fatal(err)
	}
	articles, err := registry.BindAt(field.Fields{field.Blocks("body", card)}, "collections[1].fields")
	if err != nil {
		t.Fatal(err)
	}
	if got := field.Snapshot(pages[0]).BlockReferences(); strings.Join(got, ",") != "hero-card,note" {
		t.Fatalf("inline container selection = %v", got)
	}
	if got := field.Snapshot(articles[0]).BlockReferences(); strings.Join(got, ",") != "hero-card" {
		t.Fatalf("second container selection = %v", got)
	}
	definitions := registry.Blocks()
	if len(definitions) != 2 {
		t.Fatalf("definitions = %d, want note and hero-card once each", len(definitions))
	}
	names := map[string]string{}
	for _, definition := range definitions {
		names[definition.Slug] = definition.TypeName
	}
	if names["hero-card"] != "HeroCard" || names["note"] != "Note" {
		t.Fatalf("derived type names = %v", names)
	}
	branch := field.Snapshot(pages[0]).Branches()[0]
	if !branch.Referenced || branch.Selector.Slug != "hero-card" {
		t.Fatalf("interned branch = %+v", branch)
	}
}

func TestBlockRegistryRejectsDifferentDeclarationsOfOneSlug(t *testing.T) {
	validated := func(operation.Context, operation.Value[string]) ([]operation.Issue, error) { return nil, nil }
	shared := field.Block{Slug: "card", Fields: field.Fields{field.Text("title").Validate(validated)}}
	build := func() field.Block {
		return field.Block{Slug: "card", Fields: field.Fields{field.Text("title").Validate(validated)}}
	}
	for _, test := range []struct {
		name, code string
		first      field.Block
		second     field.Block
	}{
		{"different fields", "block_definition_conflict", field.Block{Slug: "card", Fields: field.Fields{field.Text("title")}}, field.Block{Slug: "card", Fields: field.Fields{field.Text("heading")}}},
		{"different labels", "block_definition_conflict", field.Block{Slug: "card", Fields: field.Fields{field.Text("title")}}, field.Block{Slug: "card", Labels: field.BlockLabels{Singular: "Tile"}, Fields: field.Fields{field.Text("title")}}},
		{"different type name", "block_definition_conflict", field.Block{Slug: "card", Fields: field.Fields{field.Text("title")}}, field.Block{Slug: "card", TypeName: "Tile", Fields: field.Fields{field.Text("title")}}},
		{"callbacks built twice", "block_definition_conflict", build(), build()},
		{"type name of another block", "block_type_name_conflict", field.Block{Slug: "card", Fields: field.Fields{field.Text("title")}}, field.Block{Slug: "tile", TypeName: "Card", Fields: field.Fields{field.Text("title")}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, err := field.NewBlockRegistry()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := registry.BindAt(field.Fields{field.Blocks("layout", test.first)}, "collections[0].fields"); err != nil {
				t.Fatal(err)
			}
			_, err = registry.BindAt(field.Fields{field.Blocks("body", test.second)}, "collections[1].fields")
			if err == nil || !strings.Contains(err.Error(), test.code) || !strings.Contains(err.Error(), "collections[0].fields[0].blocks[0]") {
				t.Fatalf("BindAt error = %v, want %s naming the first declaration", err, test.code)
			}
		})
	}
	// Equal declarations, including one value with callbacks reused anywhere, are one block.
	registry, err := field.NewBlockRegistry(shared)
	if err != nil {
		t.Fatal(err)
	}
	for index, fields := range []field.Fields{
		{field.Blocks("layout", shared)},
		{field.Group("group", field.Fields{field.Blocks("nested", shared)})},
		{field.Blocks("plain", field.Block{Slug: "plain", Fields: field.Fields{field.Text("title")}})},
		{field.Blocks("again", field.Block{Slug: "plain", Fields: field.Fields{field.Text("title")}})},
	} {
		if _, err := registry.BindAt(fields, "fields"); err != nil {
			t.Fatalf("declaration %d: %v", index, err)
		}
	}
	// References select only central registrations, independent of order.
	if _, err := registry.BindAt(field.Fields{field.Blocks("ref").References("plain")}, "fields"); err == nil || !strings.Contains(err.Error(), "declared inline") {
		t.Fatalf("reference to an inline declaration = %v", err)
	}
}
