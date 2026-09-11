package blocks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/examples/blocks/content"
	"github.com/riducms/ridu/examples/blocks/generated"
	"github.com/riducms/ridu/examples/blocks/render"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/query"
)

// This same test is copied, without generated-file edits, into a fresh ridu new
// project by the CLI acceptance test. That run owns real migration artifacts.
func TestReferenceWorkflow(t *testing.T) {
	ctx := context.Background()
	draft := true
	database := os.Getenv("RIDU_BLOCKS_REFERENCE_DB")
	migrated := database != ""
	if !migrated {
		database = filepath.Join(t.TempDir(), "reference.sqlite")
	}
	backend, err := sqlite.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	config := content.Config()
	if !migrated {
		manifest, err := ridu.Resolve(config)
		if err != nil {
			t.Fatal(err)
		}
		if err := backend.Migrate(ctx, manifest); err != nil {
			t.Fatal(err)
		}
	}
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	pages := generated.PagesCollection.With(app.Local())
	assets := generated.AssetsCollection.With(app.Local())
	asset, err := assets.Create(ctx, generated.AssetCreate{Title: "Mountains", URL: "/mountains.svg"}, ridu.TypedMutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var body richtext.Document[generated.ContentBodyBlocksBlockInputPayload]
	if err := json.Unmarshal([]byte(`{"version":1,"root":{"type":"root","version":1,"format":"","indent":0,"direction":null,"children":[{"type":"paragraph","version":1,"format":"","indent":0,"direction":null,"textFormat":0,"textStyle":"","children":[{"type":"text","version":1,"text":"A portable page.","detail":0,"format":0,"mode":"normal","style":""}]}]}}`), &body); err != nil {
		t.Fatal(err)
	}
	input := generated.PageCreate{Title: "Reference", Layout: core.NonNull(generated.PagesLayoutInput{
		&generated.HeroInput{Heading: "Hello <reader>", Appearance: core.Set(generated.HeroAppearanceInput{Tone: core.Set(generated.HeroAppearanceToneDark)})},
		&generated.ContentInput{Title: core.Set("Story"), Body: core.Set(body), Links: core.Set([]generated.ContentLinksRowInput{{Key: "read-more", Label: "Read more", Href: "/about"}, {Key: "contact", Label: "Contact", Href: "/contact"}})},
		&generated.MediaInput{Asset: asset.ID, Caption: core.Set("Mountains")},
		&generated.CTAInput{Label: "Read more"},
	})}
	page, err := pages.Create(ctx, input, ridu.TypedMutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Layout == nil || len(*page.Layout) != 4 {
		t.Fatalf("created layout = %#v", page.Layout)
	}
	keys := make(map[string]bool)
	for _, block := range *page.Layout {
		key := block.BlockKey()
		if key == "" || keys[key] {
			t.Fatalf("invalid generated identity %q", key)
		}
		keys[key] = true
	}
	for _, value := range *page.Layout {
		if block, ok := value.(*generated.Content); ok {
			if block.Body == nil {
				t.Fatal("rich-text child was omitted")
			}
			encoded, err := json.Marshal(block.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(encoded, []byte("A portable page.")) {
				t.Fatal("rich-text child did not survive creation")
			}
		}
	}
	var rendered bytes.Buffer
	if err := render.HTML(&rendered, *page.Layout); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rendered.Bytes(), []byte("Hello &lt;reader&gt;")) {
		t.Fatal(rendered.String())
	}
	// Reused block types use the same renderer across distinct generated layouts.
	campaign, err := generated.CampaignsCollection.With(app.Local()).Create(ctx, generated.CampaignCreate{
		Title: core.Set("Campaign"),
		Layout: core.Set(generated.CampaignsLayoutInput{
			&generated.HeroInput{Heading: "Campaign <reader>"},
			&generated.CTAInput{Label: "Join & share"},
		}),
	}, ridu.TypedMutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if campaign.Layout == nil {
		t.Fatal("campaign layout was omitted")
	}
	rendered.Reset()
	if err := render.HTML(&rendered, *campaign.Layout); err != nil {
		t.Fatal(err)
	}
	if rendered.String() != "<header><h1>Campaign &lt;reader&gt;</h1></header><aside>Join &amp; share</aside>" {
		t.Fatal(rendered.String())
	}
	assetPath, err := query.ParsePath("layout.media.asset")
	if err != nil {
		t.Fatal(err)
	}
	found, err := generated.PagesCollection.With(app.Local()).Find(ctx, page.ID, core.TypedReadOptions{Draft: &draft, Populate: []query.Population{{Path: assetPath, Depth: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range *found.Layout {
		if media, ok := value.(*generated.Media); ok {
			if media.Asset == nil || media.Asset.Document == nil || media.Asset.ID != asset.ID {
				t.Fatalf("population lost typed asset: %#v", media.Asset)
			}
		}
	}
	listed, err := generated.PagesCollection.With(app.Local()).List(ctx, core.TypedListOptions{Draft: &draft, Populate: []query.Population{{Path: assetPath, Depth: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if listed.Total < 1 || len(listed.Documents) == 0 {
		t.Fatal("populated list is empty")
	}
	for _, document := range listed.Documents {
		if document.Layout == nil {
			t.Fatal("populated list omitted layout")
		}
		for _, value := range *document.Layout {
			if media, ok := value.(*generated.Media); ok && (media.Asset == nil || media.Asset.Document == nil || media.Asset.Document.ID != media.Asset.ID) {
				t.Fatal("populated list lost typed asset")
			}
		}
	}

	heading, label := "Bonjour", "En savoir plus"
	translated, err := page.Layout.Retain()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range translated {
		switch block := value.(type) {
		case *generated.HeroUpdate:
			block.Heading = &heading
		case *generated.ContentUpdate:
			block.Title = core.Set("Histoire")
		case *generated.CTAUpdate:
			block.Label = &label
		}
	}
	// Media is deliberately untouched and still retained, with all its fields.

	_, err = pages.Update(ctx, page.ID, generated.PageUpdate{Layout: core.SetNonNull(translated)}, ridu.TypedMutationOptions{ExpectedRevision: page.Revision, Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	all, err := generated.PagesCollectionAllLocales.With(app.Local()).Find(ctx, page.ID, core.TypedReadOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range *all.Layout {
		if hero, ok := value.(*generated.HeroAllLocales); ok {
			if hero.Heading == nil || ((*hero.Heading)["en"] != "Hello <reader>" || (*hero.Heading)["fr"] != "Bonjour") {
				t.Fatalf("locale output = %#v", hero.Heading)
			}
		}
	}
	allPages, err := generated.PagesCollectionAllLocales.With(app.Local()).List(ctx, core.TypedListOptions{Draft: &draft, Populate: []query.Population{{Path: assetPath, Depth: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, document := range allPages.Documents {
		if document.ID != page.ID {
			continue
		}
		matched = true
		for _, value := range *document.Layout {
			switch block := value.(type) {
			case *generated.HeroAllLocales:
				if block.Heading == nil || (*block.Heading)["en"] != "Hello <reader>" || (*block.Heading)["fr"] != "Bonjour" {
					t.Fatal("all-locales list lost translations")
				}
			case *generated.MediaAllLocales:
				if block.Asset == nil || block.Asset.Document == nil {
					t.Fatal("all-locales list lost populated asset")
				}
			}
		}
	}
	if !matched {
		t.Fatal("all-locales list omitted created page")
	}

	// Keyed patches leave unrelated and potentially redacted children untouched.
	retained, err := generated.PagesCollection.With(app.Local()).Find(ctx, page.ID, core.TypedReadOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range *retained.Layout {
		switch block := value.(type) {
		case *generated.Hero:
			appearance, ok := block.Appearance.Get()
			if !ok {
				t.Fatal("patch lost Hero appearance")
			}
			tone, ok := appearance.Tone.Get()
			if !ok || tone != "dark" {
				t.Fatal("patch changed Hero appearance")
			}
		case *generated.Content:
			links, ok := block.Links.Get()
			if !ok || len(links) != 2 || links[0].Key != "read-more" {
				t.Fatal("patch lost nested links or their identity")
			}
			encoded, err := json.Marshal(block.Body)
			if err != nil || !bytes.Contains(encoded, []byte("A portable page.")) {
				t.Fatal("patch lost ordinary rich-text child")
			}
		case *generated.Media:
			if block.Asset == nil || block.Asset.ID != asset.ID {
				t.Fatal("patch lost required asset")
			}
		}
	}

	// Edit a nested link through a read which redacts required labels. Retention
	// must preserve both rows, every omitted label, and unrelated block content.
	restrictedConfig := content.Config()
	restrictedConfig.Collections[1].Fields, err = restrictedConfig.Collections[1].Fields.Edit(func(root *field.ChildrenDraft) error {
		return root.EditBlock("layout", "content", func(block *field.ChildrenDraft) error {
			return block.EditChildren("links", func(links *field.ChildrenDraft) error {
				return links.EditText("label", func(label field.TextField) field.TextField {
					return label.Access(field.Access{Read: func(operation.Context) (bool, error) { return false, nil }})
				})
			})
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	restrictedApp, err := ridu.New(restrictedConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	restrictedPages := generated.PagesCollection.With(restrictedApp.Local())
	before, err := restrictedPages.Find(ctx, page.ID, core.TypedReadOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	patch, err := before.Layout.Retain()
	if err != nil {
		t.Fatal(err)
	}
	edited := false
	for i, value := range *before.Layout {
		block, ok := value.(*generated.Content)
		if !ok {
			continue
		}
		links, present := block.Links.Get()
		if !present || len(links) != 2 || links[0].Label != nil || links[1].Label != nil {
			t.Fatal("nested edit did not exercise redacted rows")
		}
		changes, err := links.Retain()
		if err != nil {
			t.Fatal(err)
		}
		href := "/about-us"
		for _, row := range changes {
			if row.RowKey() == "read-more" {
				row.(*generated.ContentLinksRowUpdate).Href = &href
				edited = true
			}
		}
		patch[i].(*generated.ContentUpdate).Links = core.Set(changes)
	}
	if !edited {
		t.Fatal("nested link was not edited")
	}
	if _, err := restrictedPages.Update(ctx, page.ID, generated.PageUpdate{Layout: core.SetNonNull(patch)}, ridu.TypedMutationOptions{ExpectedRevision: before.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := restrictedPages.Update(ctx, page.ID, generated.PageUpdate{Layout: core.SetNonNull(patch)}, ridu.TypedMutationOptions{ExpectedRevision: before.Revision}); err == nil {
		t.Fatal("stale nested update accepted")
	}
	after, err := pages.Find(ctx, page.ID, core.TypedReadOptions{Draft: &draft, Populate: []query.Population{{Path: assetPath, Depth: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range *after.Layout {
		switch block := value.(type) {
		case *generated.Content:
			links, ok := block.Links.Get()
			if !ok || len(links) != 2 || links[0].Key != "read-more" || links[1].Key != "contact" || links[0].Href == nil || *links[0].Href != "/about-us" || links[0].Label == nil || *links[0].Label != "Read more" || links[1].Label == nil || *links[1].Label != "Contact" || links[1].Href == nil || *links[1].Href != "/contact" {
				t.Fatal("nested edit lost sibling fields, identity or order")
			}
			encoded, err := json.Marshal(block.Body)
			if err != nil || !bytes.Contains(encoded, []byte("A portable page.")) {
				t.Fatal("nested edit lost rich text")
			}
		case *generated.Media:
			if block.Asset == nil || block.Asset.ID != asset.ID || block.Asset.Document == nil {
				t.Fatal("nested edit lost populated media")
			}
		}
	}
	// A second invocation after migration must retain the first invocation's page.
	if migrated {
		previous, err := pages.List(ctx, core.TypedListOptions{Draft: &draft})
		if err != nil {
			t.Fatal(err)
		}
		if os.Getenv("RIDU_BLOCKS_REFERENCE_EVOLVED") == "1" && previous.Total < 2 {
			t.Fatal("migration lost existing content")
		}
	}
	var unknown generated.PagesLayout
	if err := json.Unmarshal([]byte(`[{"blockType":"retired","_key":"old"}]`), &unknown); err == nil {
		t.Fatal("unknown discriminator accepted")
	}
}
