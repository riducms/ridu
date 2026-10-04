package blocktypes_test

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/blocktypes"
	"github.com/riducms/ridu/schema"
)

func resolveBlocks(blocks ...field.Block) (schema.Manifest, error) {
	fields := make(field.Fields, len(blocks))
	for i, block := range blocks {
		fields[i] = field.Blocks([]string{"layout", "footer", "sidebar"}[i], block)
	}
	return core.Resolve(core.Config{Name: "Block naming", Admin: core.AdminConfig{Localization: core.AdminLocalizationConfig{DefaultLanguage: "en", Languages: []core.AdminLanguage{{Code: "en", Label: "English"}}}}, Collections: []core.Collection{{Slug: "pages", Fields: fields}}})
}

func TestReusableBlockNames(t *testing.T) {
	hero := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Text("heading").Required(), field.Blocks("children", field.Block{Slug: "text", Fields: field.Fields{field.Text("body")}})}}
	relabeled := hero.Snapshot()
	relabeled.Admin = field.BlockAdmin{RowLabelPath: "heading"}
	relabeled.Labels = field.BlockLabels{Singular: "Banner", Plural: "Banners", SingularTranslations: map[string]string{"en": "Display banner"}, PluralTranslations: map[string]string{"en": "Display banners"}}
	relabeled.Fields[0] = field.Text("heading").Required().Label("Headline")
	manifest, err := resolveBlocks(hero, relabeled)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := blocktypes.Build(manifest.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Variants) != 2 {
		t.Fatalf("variants: %#v", catalog.Variants)
	}
	snapshot := manifest.Snapshot()
	for _, root := range snapshot.Collections[0].Fields {
		nested := root.Blocks.ResolvedTypes()[0].ResolvedFields()[1]
		if catalog.Fields[nested.ID].Name != "HeroChildren" {
			t.Fatalf("nested reuse lost: %#v", catalog.Fields[nested.ID])
		}
	}
	again, err := resolveBlocks(hero, relabeled)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := manifest.Bytes()
	b, _ := again.Bytes()
	if string(a) != string(b) {
		t.Fatal("manifest generation unstable")
	}
	encoded, _ := manifest.Bytes()
	parsed, err := schema.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Snapshot().Collections[0].Fields[0].Blocks.ResolvedTypes()[0].TypeName != "Hero" {
		t.Fatal("named metadata lost")
	}
}

func TestBlockNameConflicts(t *testing.T) {
	hero := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Text("heading").Required()}}
	for _, test := range []struct {
		name  string
		other field.Block
	}{
		{"different discriminator", field.Block{TypeName: "Hero", Slug: "cta", Labels: field.BlockLabels{Singular: "CTA"}, Fields: field.Fields{field.Text("heading").Required()}}},
		{"different child", field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Number("heading").Required()}}},
		{"different requiredness", field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Text("heading")}}},
		{"generated suffix", field.Block{TypeName: "HeroInput", Slug: "hero", Fields: field.Fields{field.Text("heading")}}},
		{"resource symbol", field.Block{TypeName: "Pages", Slug: "hero", Fields: field.Fields{}}},
		{"framework symbol", field.Block{TypeName: "ID", Slug: "hero", Fields: field.Fields{}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolveBlocks(hero, test.other)
			if err == nil || !strings.Contains(err.Error(), "footer.blocks.hero.typeName") && !strings.Contains(err.Error(), "footer.blocks.cta.typeName") {
				t.Fatalf("missing precise conflict: %v", err)
			}
		})
	}
	for _, name := range []string{"hero", "Hero-Type", "Hero Type", "Hero_", "1Hero"} {
		named := hero
		named.TypeName = name
		_, err := resolveBlocks(named)
		if err == nil || !strings.Contains(err.Error(), "invalid_block_type_name") {
			t.Fatalf("invalid name %q: %v", name, err)
		}
	}
}

func TestUnnamedBlocksHavePathDerivedNames(t *testing.T) {
	hero := field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}}
	manifest, err := resolveBlocks(hero, hero)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := blocktypes.Build(manifest.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	names := []string{catalog.Variants[0].Name, catalog.Variants[1].Name}
	if names[0] != "PagesFooterHero" || names[1] != "PagesLayoutHero" {
		t.Fatalf("names=%v", names)
	}
	renamed := hero
	renamed.Labels.Singular = "Different"
	other, err := resolveBlocks(renamed, hero)
	if err != nil {
		t.Fatal(err)
	}
	otherCatalog, _ := blocktypes.Build(other.Snapshot())
	if otherCatalog.Variants[1].Name != names[1] {
		t.Fatal("label changed identity")
	}
}

func TestReusableBlockLocalizationContext(t *testing.T) {
	hero := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Text("heading").Localized()}}
	for _, localizedFirst := range []bool{false, true} {
		fields := field.Fields{field.Blocks("plain", hero), field.Blocks("translated", hero).Localized()}
		if localizedFirst {
			fields[0], fields[1] = fields[1], fields[0]
		}
		manifest, err := core.Resolve(core.Config{Name: "Localization contexts", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}}}, Collections: []core.Collection{{Slug: "pages", Fields: fields}}})
		if err != nil {
			t.Fatal(err)
		}
		catalog, err := blocktypes.Build(manifest.Snapshot())
		if err != nil {
			t.Fatal(err)
		}
		if !catalog.Variants[0].Block.ResolvedFields()[0].Localized {
			t.Fatal("canonical type lost configured child localization")
		}
	}
	different := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Text("heading")}}
	_, err := core.Resolve(core.Config{Name: "Conflicting declarations", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}}}, Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("one", hero).Localized(), field.Blocks("two", different).Localized()}}}})
	if err == nil || !strings.Contains(err.Error(), "block_type_name_conflict") {
		t.Fatalf("authored locality conflict hidden by ancestor: %v", err)
	}
}

func TestGeneratedNestedNameConflict(t *testing.T) {
	hero := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Group("style", field.Fields{field.Text("tone")})}}
	collision := field.Block{TypeName: "HeroStyle", Slug: "other", Fields: field.Fields{}}
	if _, err := resolveBlocks(hero, collision); err == nil || !strings.Contains(err.Error(), "block_type_name_conflict") {
		t.Fatalf("nested symbol collision: %v", err)
	}
}

func TestGeneratedFrameworkAndGlobalNameConflicts(t *testing.T) {
	for _, name := range []string{"ContractError", "Array", "RiduLocalizedValues", "CollectionSlug", "GlobalSlug", "PagesCollection", "PagesAllLocalesPopulateOutput"} {
		_, err := resolveBlocks(field.Block{TypeName: name, Slug: "hero", Fields: field.Fields{}})
		if err == nil || !strings.Contains(err.Error(), "block_type_name_conflict") {
			t.Fatalf("generated helper %s collision: %v", name, err)
		}
	}
	_, err := core.Resolve(core.Config{Name: "Global symbols", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout", field.Block{TypeName: "Footer", Slug: "hero", Fields: field.Fields{}})}}}, Globals: []core.Global{{Slug: "footer", Label: "Banner", Fields: field.Fields{field.Text("text")}}}})
	if err == nil || !strings.Contains(err.Error(), "block_type_name_conflict") {
		t.Fatalf("global generated symbol collision: %v", err)
	}
}
