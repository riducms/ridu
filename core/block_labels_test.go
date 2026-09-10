package core_test

import (
	"bytes"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"reflect"
	"strings"
	"testing"
)

func TestBlockLabelsAcrossInlineAndReferencedPlacements(t *testing.T) {
	for _, labels := range []field.BlockLabels{
		{}, {Singular: "Banner"}, {Plural: "Banners"},
		{Singular: "Banner", Plural: "Banners", SingularTranslations: map[string]string{"fr": "Bannière"}, PluralTranslations: map[string]string{"fr": "Bannières"}},
	} {
		t.Run(labels.Singular+"/"+labels.Plural, func(t *testing.T) {
			block := field.Block{Slug: "heroes", Labels: labels, Fields: field.Fields{field.Text("heading")}}
			config := ridu.Config{Name: "Block labels", Blocks: []field.Block{block}, Plugins: []ridu.Plugin{richtext.New()},
				Admin:       ridu.AdminConfig{Localization: ridu.AdminLocalizationConfig{DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}, {Code: "fr", Label: "Français"}}}},
				Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("inline", block), field.Blocks("shared").References("heroes"), richtext.Field("body", richtext.Config{BlockReferences: []string{"heroes"}}), richtext.Field("inlineBody", richtext.Config{Blocks: []field.Block{block}})}}},
				Globals:     []ridu.Global{{Slug: "settings", Fields: field.Fields{field.Blocks("layout").References("heroes")}}},
			}
			manifest, err := ridu.Resolve(config)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := manifest.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := schema.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := parsed.Snapshot()
			fields := snapshot.Collections[0].Fields
			want := schema.BlockLabels{Singular: "Hero", Plural: "Heroes", SingularTranslations: labels.SingularTranslations, PluralTranslations: labels.PluralTranslations}
			if labels.Singular != "" {
				want.Singular = labels.Singular
			}
			if labels.Plural != "" {
				want.Plural = labels.Plural
			}
			for _, b := range []schema.BlockType{snapshot.Blocks[0], fields[0].Blocks.ResolvedTypes()[0], fields[1].Blocks.ResolvedTypes()[0], fields[2].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0], fields[3].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0], snapshot.Globals[0].Fields[0].Blocks.ResolvedTypes()[0]} {
				if !reflect.DeepEqual(b.Labels, want) {
					t.Fatalf("labels = %#v, want %#v", b.Labels, want)
				}
				if b.Slug != "heroes" {
					t.Fatal("labels changed stored discriminator")
				}
			}
			if want.PluralTranslations != nil {
				snapshot.Blocks[0].Labels.PluralTranslations["fr"] = "Changed"
				fields[1].Blocks.ResolvedTypes()[0].Labels.SingularTranslations["fr"] = "Changed"
				again, _ := parsed.Bytes()
				if !bytes.Equal(raw, again) {
					t.Fatal("label mutation changed immutable manifest")
				}
			}
		})
	}
}

func TestBlockLabelsValidateBothTranslationForms(t *testing.T) {
	for _, member := range []string{"singularTranslations", "pluralTranslations"} {
		t.Run(member, func(t *testing.T) {
			labels := field.BlockLabels{}
			if member == "singularTranslations" {
				labels.SingularTranslations = map[string]string{"en": " "}
			} else {
				labels.PluralTranslations = map[string]string{"en": " "}
			}
			_, err := ridu.Resolve(ridu.Config{Name: "Invalid labels", Blocks: []field.Block{{Slug: "hero", Labels: labels, Fields: field.Fields{field.Text("heading")}}}})
			if err == nil || !strings.Contains(err.Error(), ".labels."+member) {
				t.Fatalf("expected precise translation path, got %v", err)
			}
		})
	}
}
