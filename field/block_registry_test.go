package field_test

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
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
