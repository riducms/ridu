package blockreferences

import (
	"context"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// MigrationHarness drives one adapter's immutable migrations for
// RunDefinitionMigrations. Each test starts from a fresh database.
type MigrationHarness struct {
	// Begin applies the initial migration of before and returns the store.
	Begin func(t *testing.T, before schema.Manifest) store.Store
	// Migrate creates and applies a migration from the current head to
	// after, confirming candidates. A refused migration leaves no artifact.
	Migrate func(t *testing.T, after schema.Manifest, candidates []schemadiff.RenameCandidate) error
	// Rollback undoes the last applied migration; nil when the adapter
	// recovers forward instead.
	Rollback func(t *testing.T) error
}

// PlacementsConfig places one hero definition along eight paths: at the top
// of a layout, inside sections and cards at several depths, beneath a
// localized container, in a second collection and in a global, each with
// versions. heading and label become hero's heading and cta.label fields.
func PlacementsConfig(heading, label field.Node) ridu.Config {
	hero := field.Block{Slug: "hero", Fields: field.Fields{heading, field.Group("cta", field.Fields{label})}}
	card := field.Block{Slug: "card", Fields: field.Fields{field.Text("body"), field.Array("items", field.Fields{field.Text("caption")}), field.Blocks("nested").References("hero")}}
	section := field.Block{Slug: "section", Fields: field.Fields{field.Text("title"), field.Blocks("children").References("hero", "card")}}
	return ridu.Config{
		Name:         "Definition placements",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
		Blocks:       []field.Block{hero, card, section},
		Collections: []ridu.Collection{
			{Slug: "pages", Versions: true, Fields: field.Fields{
				field.Text("title"), field.Blocks("layout").References("section", "hero", "card"), field.Blocks("translated").References("hero").Localized(),
			}},
			{Slug: "posts", Fields: field.Fields{field.Blocks("body").References("card")}},
		},
		Globals: []ridu.Global{{Slug: "site", Versions: true, Fields: field.Fields{field.Blocks("footer").References("section")}}},
	}
}

func placementHero(key, text string, label bool) store.Value {
	cta := store.Values{}
	if label {
		cta["label"] = store.String("Go")
	}
	return store.Object(store.Values{"blockType": store.String("hero"), key: store.String(text), "cta": store.Object(cta)})
}

func placementCard(heroes ...store.Value) store.Value {
	return store.Object(store.Values{"blockType": store.String("card"), "body": store.String("Card"), "nested": store.List(heroes...)})
}

func placementSection(children ...store.Value) store.Value {
	return store.Object(store.Values{"blockType": store.String("section"), "title": store.String("Section"), "children": store.List(children...)})
}

// CountBlocks counts the stored hero blocks beneath a value that hold key.
func CountBlocks(value store.Value, key string) int {
	count := 0
	switch value.Kind() {
	case store.ValueList:
		for item := range value.Elements() {
			count += CountBlocks(item, key)
		}
	case store.ValueObject:
		if slug, _ := value.Get("blockType").StringValue(); slug == "hero" {
			if _, exists := value.Lookup(key); exists {
				count++
			}
		}
		for _, child := range value.Entries() {
			count += CountBlocks(child, key)
		}
	}
	return count
}

func countValues(values store.Values, key string) int {
	return CountBlocks(store.Object(values), key)
}

// RunDefinitionMigrations renames a shared definition's field and makes one
// of its nested fields required, each as one definition-level change, and
// proves the migration reaches every stored block at every placement: in
// current documents, versions, a second collection and a global.
func RunDefinitionMigrations(t *testing.T, harness MigrationHarness) {
	ctx := context.Background()
	resolve := func(config ridu.Config) schema.Manifest {
		t.Helper()
		manifest, err := ridu.Resolve(config)
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	beforeConfig := PlacementsConfig(field.Text("heading"), field.Text("label"))
	before := resolve(beforeConfig)
	backend := harness.Begin(t, before)
	application, err := ridu.New(beforeConfig, backend)
	if err != nil {
		t.Fatal(err)
	}
	layout := func(key string) store.Values {
		return store.Values{
			"title": store.String("Home"),
			"layout": store.List(
				placementSection(placementHero(key, "In a section", true), placementCard(placementHero(key, "In a section's card", true))),
				placementHero(key, "At the top", true),
				placementCard(placementHero(key, "In a card", true)),
			),
			"translated": store.List(placementHero(key, "English", true)),
		}
	}
	page, err := application.Local().Create(ctx, "pages", layout("heading"), ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	french := store.Values{"title": store.String("Accueil"), "translated": store.List(placementHero("heading", "Français", true))}
	if _, err := application.Local().PublishChanges(ctx, "pages", page.ID, french, ridu.MutationOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}
	// This post's hero has no cta label.
	post, err := application.Local().Create(ctx, "posts", store.Values{"body": store.List(placementCard(placementHero("heading", "In a post", false)))}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().UpdateGlobal(ctx, "site", store.Values{"footer": store.List(placementSection(placementHero("heading", "In the footer", true), placementCard(placementHero("heading", "In a footer card", true))))}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}

	// One confirmed rename moves the field in every stored hero block.
	type counts struct{ pages, versions, posts, site int }
	count := func(config ridu.Config, key string) counts {
		t.Helper()
		application, err := ridu.New(config, backend)
		if err != nil {
			t.Fatal(err)
		}
		var result counts
		found, err := application.Local().Find(ctx, "pages", page.ID, ridu.FindOptions{AllLocales: true})
		if err != nil {
			t.Fatal(err)
		}
		result.pages = countValues(found.Values, key)
		history, err := application.Local().Versions(ctx, "pages", page.ID, ridu.FindOptions{AllLocales: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, version := range history {
			result.versions += countValues(version.Snapshot.Values, key)
		}
		stored, err := application.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{})
		if err != nil {
			t.Fatal(err)
		}
		result.posts = countValues(stored.Values, key)
		global, err := application.Local().Global(ctx, "site", ridu.FindOptions{})
		if err != nil {
			t.Fatal(err)
		}
		result.site = countValues(global.Values, key)
		return result
	}
	seeded := count(beforeConfig, "heading")
	if seeded.pages != 6 || seeded.versions < 6 || seeded.posts != 1 || seeded.site != 2 {
		t.Fatalf("seeded hero blocks = %+v", seeded)
	}
	afterConfig := PlacementsConfig(field.Text("headline"), field.Text("label"))
	after := resolve(afterConfig)
	candidates := schemadiff.RenameCandidates(before, after)
	if len(candidates) != 1 || candidates[0].Kind != schemadiff.RenameBlockField || candidates[0].Block != "hero" {
		t.Fatalf("rename candidates = %#v", candidates)
	}
	if err := harness.Migrate(t, after, candidates); err != nil {
		t.Fatalf("block field rename: %v", err)
	}
	expect := func(config ridu.Config, key string) {
		t.Helper()
		if got := count(config, key); got != seeded {
			t.Errorf("hero blocks with %q = %+v, want %+v", key, got, seeded)
		}
	}
	expect(afterConfig, "headline")

	// Requiring a nested field of the definition audits every stored block:
	// the post's nested hero lacks the label, so nothing changes.
	required := resolve(PlacementsConfig(field.Text("headline"), field.Text("label").Required()))
	err = harness.Migrate(t, required, nil)
	if err == nil || !strings.Contains(err.Error(), "RIDU_REQUIRED_VALUES_MISSING") || !strings.Contains(err.Error(), "block hero.cta.label in 1 document, for example "+post.ID) {
		t.Fatalf("required nested block field = %v", err)
	}
	expect(afterConfig, "headline")

	if harness.Rollback == nil {
		return
	}
	if err := harness.Rollback(t); err != nil {
		t.Fatalf("roll back the block field rename: %v", err)
	}
	expect(beforeConfig, "heading")
}
