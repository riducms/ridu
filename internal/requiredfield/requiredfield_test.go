package requiredfield

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func resolve(t *testing.T, collections ...core.Collection) schema.Snapshot {
	t.Helper()
	manifest, err := core.Resolve(core.Config{
		Name: "Required fields",
		Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: collections,
	})
	if err != nil {
		t.Fatal(err)
	}
	return manifest.Snapshot()
}

func addresses(requirements []Requirement) []string {
	result := make([]string, len(requirements))
	for index, requirement := range requirements {
		result[index] = requirement.Address()
	}
	return result
}

// Detect reports exactly the stored places that become required. Fields that
// were already required, new resources, new containers and new block types
// hold no stored values that could lack the field.
func TestDetectListsNewlyRequiredStoredFields(t *testing.T) {
	before := resolve(t,
		core.Collection{Slug: "posts", Fields: field.Fields{
			field.Text("title").Required(),
			field.Text("summary"),
			field.Group("seo", field.Fields{field.Text("title")}),
			field.Array("items", field.Fields{field.Text("caption")}),
			field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}}),
			field.Text("subtitle").Localized(),
			field.Text("kind"),
		}},
	)
	after := resolve(t,
		core.Collection{Slug: "posts", Fields: field.Fields{
			field.Text("title").Required(),
			field.Text("summary").Required(),
			field.Group("seo", field.Fields{field.Text("title").Required()}),
			field.Array("items", field.Fields{field.Text("caption").Required(), field.Number("rank").Required()}),
			field.Blocks("layout",
				field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading").Required()}},
				field.Block{Slug: "quote", Fields: field.Fields{field.Text("body").Required()}},
			),
			field.Text("subtitle").Required(),
			field.Number("kind").Required(),
			field.Group("meta", field.Fields{field.Text("note").Required()}),
			field.Text("added").Required(),
		}},
		core.Collection{Slug: "pages", Fields: field.Fields{field.Text("title").Required()}},
	)
	got := addresses(Detect(before, after, Renames{}))
	// A block definition's field is one requirement for every stored block.
	want := []string{
		"posts.summary", "posts.seo.title", "posts.items.caption", "posts.items.rank",
		"posts.subtitle", "posts.kind", "posts.added", "block hero.heading",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("requirements = %v, want %v", got, want)
	}
}

// A confirmed rename keeps the requiredness its field already had, and a
// resource that stops deferring drafts requires every required field of its
// working documents again.
func TestDetectFollowsRenamesAndDraftDeferral(t *testing.T) {
	before := resolve(t, core.Collection{Slug: "posts", Versions: true, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()}})
	renamed := resolve(t, core.Collection{Slug: "posts", Versions: true, VersionConfig: core.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("headline").Required()}})
	if got := addresses(Detect(before, renamed, Renames{})); !reflect.DeepEqual(got, []string{"posts.headline"}) {
		t.Fatalf("unconfirmed rename requirements = %v", got)
	}
	bound := Renames{Fields: map[schema.StableID]schema.Field{renamed.Collections[0].Fields[0].ID: before.Collections[0].Fields[0]}}
	if got := Detect(before, renamed, bound); len(got) != 0 {
		t.Fatalf("confirmed rename requirements = %v", addresses(got))
	}
	published := resolve(t, core.Collection{Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title").Required()}})
	if got := addresses(Detect(before, published, Renames{})); !reflect.DeepEqual(got, []string{"posts.title"}) {
		t.Fatalf("draft deferral requirements = %v", got)
	}
}

// Inspect applies write validation's notion of a missing value, including
// translations: an untranslated locale is optional, an empty translation is
// not, and a value with no translation at all is missing everywhere.
func TestInspectReportsMissingValuesByLocale(t *testing.T) {
	before := resolve(t, core.Collection{Slug: "posts", Fields: field.Fields{
		field.Text("title").Localized(), field.Relationships("tags", "posts"),
		field.Group("seo", field.Fields{field.Text("title")}).Localized(),
		field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading")}}),
	}})
	after := resolve(t, core.Collection{Slug: "posts", Fields: field.Fields{
		field.Text("title").Localized().Required(), field.Relationships("tags", "posts").Required(),
		field.Group("seo", field.Fields{field.Text("title").Required()}).Localized(),
		field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading").Required()}}),
	}})
	requirements := Detect(before, after, Renames{})
	audit := NewAudit(requirements)
	for id, values := range map[string]store.Values{
		"complete": {
			"title": store.Object(store.Values{"en": store.String("English")}), "tags": store.List(store.String("complete")),
			"seo":    store.Object(store.Values{"en": store.Object(store.Values{"title": store.String("SEO")})}),
			"layout": store.List(store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String("Heading")})),
		},
		"empty": {
			"title": store.Object(store.Values{"en": store.String("English"), "fr": store.String("")}), "tags": store.List(),
			"seo":    store.Object(store.Values{"fr": store.Object(store.Values{"title": store.Null()})}),
			"layout": store.List(store.Object(store.Values{"blockType": store.String("hero")}), store.Object(store.Values{"blockType": store.String("hero"), "heading": store.String("")})),
		},
		"absent": {"title": store.Object(store.Values{"fr": store.Null()})},
	} {
		audit.Inspect("posts", id, values)
	}
	err := audit.Err("every test document", false)
	var failure *MissingValuesError
	if !errors.As(err, &failure) {
		t.Fatalf("audit error = %v", err)
	}
	type finding struct {
		address     string
		locale      schema.LocaleCode
		everyLocale bool
		documents   int
	}
	var got []finding
	for _, found := range failure.Findings {
		got = append(got, finding{found.Address, found.Locale, found.EveryLocale, found.Documents})
	}
	want := []finding{
		{"posts.title", "", true, 1}, {"posts.title", "fr", false, 1},
		{"posts.tags", "", false, 2},
		{"posts.seo.title", "fr", false, 1},
		{"block hero.heading", "", false, 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %+v, want %+v", got, want)
	}
	message := err.Error()
	for _, fragment := range []string{
		Code + ": stored documents have no value for fields that become required: posts.title (no translation in any locale) in 1 document, for example absent;",
		"posts.tags in 2 documents, for example ",
		"The audit read every test document.",
		"--transform <transform>",
	} {
		if !strings.Contains(message, fragment) {
			t.Fatalf("message %q does not contain %q", message, fragment)
		}
	}
	if NewAudit(requirements).Err("", false) != nil {
		t.Fatal("an audit without missing values failed")
	}
}

// Audit step payloads name fields by resource and path. Resolve recovers the
// same requirements from the after manifest and refuses an address that is
// not a required stored field there.
func TestResolveRoundTripsAuditAddresses(t *testing.T) {
	before := resolve(t, core.Collection{Slug: "posts", Fields: field.Fields{
		field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Group("cta", field.Fields{field.Text("label")})}}),
		field.Text("summary"),
	}})
	after := resolve(t, core.Collection{Slug: "posts", Fields: field.Fields{
		field.Blocks("layout", field.Block{Slug: "hero", Fields: field.Fields{field.Group("cta", field.Fields{field.Text("label").Required()})}}),
		field.Text("summary").Required(),
	}})
	requirements := Detect(before, after, Renames{})
	resolved, err := Resolve(after, Addresses(requirements))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(addresses(resolved), []string{"posts.summary", "block hero.cta.label"}) ||
		!reflect.DeepEqual(resolved[1].Steps, []Step{{Name: "hero", Descend: true}, {Name: "cta"}, {Name: "label"}}) || resolved[1].Anchored() {
		t.Fatalf("resolved requirements = %#v", resolved)
	}
	payload := Payload(requirements)
	if payload.Fields[1].ResourceID != "" || payload.Fields[1].Path != "**.hero.cta.label" {
		t.Fatalf("block requirement address = %#v", payload.Fields[1])
	}
	payload.Fields[0].Path = "layout.hero"
	if _, err := Resolve(after, payload.Fields); err == nil {
		t.Fatal("an address of a block type resolved as a required field")
	}
	if _, err := Resolve(before, Addresses(requirements)); err == nil {
		t.Fatal("an optional field resolved as a requirement")
	}
}
