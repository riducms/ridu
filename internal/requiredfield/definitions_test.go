package requiredfield

import (
	"errors"
	"reflect"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

func resolveConfig(t testing.TB, config core.Config) schema.Snapshot {
	t.Helper()
	manifest, err := core.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	return manifest.Snapshot()
}

// sharedConfig places hero in two collections, nested in a section, and
// beneath a localized container.
func sharedConfig(heading field.Node, drafts bool) core.Config {
	hero := field.Block{Slug: "hero", Fields: field.Fields{heading, field.Text("subtitle")}}
	section := field.Block{Slug: "section", Fields: field.Fields{field.Text("anchor"), field.Blocks("children").References("hero")}}
	pages := core.Collection{Slug: "pages", Fields: field.Fields{field.Blocks("layout").References("section", "hero"), field.Blocks("translated").References("hero").Localized()}}
	if drafts {
		pages.Versions, pages.VersionConfig = true, core.VersionConfig{Drafts: true}
	}
	return core.Config{
		Name:         "Shared requirements",
		Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
		Blocks:       []field.Block{hero, section},
		Collections:  []core.Collection{pages, {Slug: "posts", Fields: field.Fields{field.Blocks("body").References("hero")}}},
	}
}

func heroRow(values store.Values) store.Value {
	values["blockType"] = store.String("hero")
	return store.Object(values)
}

// A field a definition newly requires is one requirement, audited in every
// stored block of the definition: at any depth, in every resource, and in
// each translation of a localized container.
func TestDefinitionRequirementAuditsEveryStoredBlock(t *testing.T) {
	before := resolveConfig(t, sharedConfig(field.Text("heading"), false))
	after := resolveConfig(t, sharedConfig(field.Text("heading").Required(), false))
	requirements := Detect(before, after, Renames{})
	if got := addresses(requirements); !reflect.DeepEqual(got, []string{"block hero.heading"}) {
		t.Fatalf("requirements = %v", got)
	}
	groups := Groups(requirements)
	if len(groups) != 2 || groups[0].Resource.Slug != "pages" || len(groups[0].Roots) != 2 || groups[1].Resource.Slug != "posts" {
		t.Fatalf("groups = %#v", groups)
	}
	audit := NewAudit(requirements)
	audit.Inspect("pages", "nested", store.Values{"layout": store.List(store.Object(store.Values{"blockType": store.String("section"), "children": store.List(heroRow(store.Values{"heading": store.String("")}))}))})
	audit.Inspect("pages", "translated", store.Values{"translated": store.Object(store.Values{
		"en": store.List(heroRow(store.Values{"heading": store.String("Present")})),
		"fr": store.List(heroRow(store.Values{})),
	})})
	audit.Inspect("posts", "complete", store.Values{"body": store.List(heroRow(store.Values{"heading": store.String("Present")}))})
	audit.Inspect("posts", "missing", store.Values{"body": store.List(heroRow(store.Values{"subtitle": store.String("No heading")}))})
	var failure *MissingValuesError
	if err := audit.Err("", false); !errors.As(err, &failure) {
		t.Fatalf("audit error = %v", err)
	}
	type finding struct {
		locale    schema.LocaleCode
		documents int
	}
	var got []finding
	for _, found := range failure.Findings {
		got = append(got, finding{found.Locale, found.Documents})
	}
	if want := []finding{{"", 2}, {"fr", 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %+v", got)
	}
}

// A definition's field whose localization changes is newly required only
// outside localized containers: beneath one, every view clears it.
func TestDefinitionRequirementFollowsItsLocalizationScope(t *testing.T) {
	before := resolveConfig(t, sharedConfig(field.Text("heading").Required(), false))
	after := resolveConfig(t, sharedConfig(field.Text("heading").Required().Localized(), false))
	requirements := Detect(before, after, Renames{})
	if len(requirements) != 1 || requirements[0].Scope != ScopeUnlocalized || requirements[0].Address() != "block hero.heading (outside localized fields)" {
		t.Fatalf("requirements = %#v", requirements)
	}
	resolved, err := Resolve(after, Addresses(requirements))
	if err != nil || resolved[0].Scope != ScopeUnlocalized {
		t.Fatalf("resolved = %#v, %v", resolved, err)
	}
	audit := NewAudit(resolved)
	// The localized container's blocks keep their stored values unchanged.
	audit.Inspect("pages", "localized", store.Values{"translated": store.Object(store.Values{"en": store.List(heroRow(store.Values{}))})})
	if err := audit.Err("", false); err != nil {
		t.Fatalf("a localized placement was audited: %v", err)
	}
	audit.Inspect("posts", "plain", store.Values{"body": store.List(heroRow(store.Values{"heading": store.Object(store.Values{})}))})
	if err := audit.Err("", false); err == nil {
		t.Fatal("an unlocalized placement without a translation passed")
	}
}

// A resource that stops deferring drafts requires every required field
// again, including those of each definition it places, but only there.
func TestDraftDeferralRequiresPlacedDefinitionsOfThatResource(t *testing.T) {
	before := resolveConfig(t, sharedConfig(field.Text("heading").Required(), true))
	after := resolveConfig(t, sharedConfig(field.Text("heading").Required(), false))
	requirements := Detect(before, after, Renames{})
	var got []string
	for _, requirement := range requirements {
		got = append(got, string(requirement.Resource.Slug)+"."+requirement.Path())
	}
	if !reflect.DeepEqual(got, []string{"pages.layout.**.hero.heading", "pages.translated.**.hero.heading"}) {
		t.Fatalf("requirements = %v", got)
	}
	audit := NewAudit(requirements)
	audit.Inspect("posts", "elsewhere", store.Values{"body": store.List(heroRow(store.Values{}))})
	audit.Inspect("pages", "nested", store.Values{"layout": store.List(store.Object(store.Values{"blockType": store.String("section"), "children": store.List(heroRow(store.Values{}))}))})
	var failure *MissingValuesError
	if err := audit.Err("", false); !errors.As(err, &failure) || len(failure.Findings) != 1 || failure.Findings[0].Samples[0] != "nested" {
		t.Fatalf("audit error = %v", err)
	}
}

// Detection follows definitions: a graph with thousands of times more
// placements costs about as much as one with fewer definitions.
func TestDetectFollowsDefinitions(t *testing.T) {
	cost := func(layers int) float64 {
		beforeConfig := blockreferences.LayeredConfig(layers, 3)
		afterConfig := blockreferences.LayeredConfig(layers, 3)
		afterConfig.Blocks = append([]field.Block(nil), afterConfig.Blocks...)
		for index := range afterConfig.Blocks {
			afterConfig.Blocks[index].Fields = append(append(field.Fields(nil), afterConfig.Blocks[index].Fields...), field.Text("note").Required())
		}
		before, after := resolveConfig(t, beforeConfig), resolveConfig(t, afterConfig)
		var requirements []Requirement
		allocations := testing.AllocsPerRun(3, func() { requirements = Detect(before, after, Renames{}) })
		if len(requirements) != 3*(layers+1) {
			t.Fatalf("%d layers: %d requirements", layers, len(requirements))
		}
		return allocations
	}
	if shallow, deep := cost(3), cost(7); deep > 4*shallow {
		t.Fatalf("detection allocations grew from %.0f to %.0f with placements", shallow, deep)
	}
}
