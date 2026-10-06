package fieldchange_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/fieldchange"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestFieldKindRecoveryRequiresEmptyCurrentValuesAndSnapshots(t *testing.T) {
	base := fieldchange.Report{ResourceSlug: "posts", Path: "body", Before: "json", After: "group"}
	if err := fieldchange.RequireEmpty([]fieldchange.Report{base}); err != nil {
		t.Fatalf("empty values were refused: %v", err)
	}
	for _, snapshot := range []bool{false, true} {
		report := base
		if snapshot {
			report.Snapshots = 1
		} else {
			report.Documents = 1
		}
		if err := fieldchange.RequireEmpty([]fieldchange.Report{report}); err == nil || !strings.Contains(err.Error(), "RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM") || !strings.Contains(err.Error(), "posts.body") {
			t.Fatalf("stored values were admitted without recovery: %v", err)
		}
	}
}

func changeManifest(t *testing.T, numeric bool) schema.Manifest {
	t.Helper()
	leaf := func(name string) field.Node {
		if numeric {
			return field.Number(name)
		}
		return field.Text(name)
	}
	manifest, err := core.Resolve(core.Config{Name: "Nested recovery", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []core.Collection{{Slug: "posts", Fields: field.Fields{
		field.Group("meta", field.Fields{leaf("score"), field.Text("keep")}),
		field.Group("localized", field.Fields{leaf("score")}).Localized(),
		field.Array("rows", field.Fields{leaf("score"), field.Text("keep")}),
		field.Blocks("body", field.Block{Slug: "paragraph", Fields: field.Fields{leaf("score"), field.Text("keep")}}, field.Block{Slug: "card", Fields: field.Fields{field.Text("score")}}),
		field.JSON("opaque"),
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestFieldKindRecoveryFollowsOnlyDeclaredContainers(t *testing.T) {
	before, after := changeManifest(t, false), changeManifest(t, true)
	changes := fieldchange.Detect(before.Snapshot(), after.Snapshot())
	if len(changes) != 4 {
		t.Fatalf("changes = %#v", changes)
	}
	// A block definition's change is one change, applied at every placement.
	if block := changes[3]; block.Block != "paragraph" || len(block.Containers) != 0 || block.TopLevel() || !block.AppliesTo("posts") {
		t.Fatalf("block change = %#v", block)
	}
	posts := before.Snapshot().Collections[0]
	values := store.Values{
		"meta":      store.Object(store.Values{"score": store.String("old"), "keep": store.String("kept")}),
		"localized": store.Object(store.Values{"en": store.Object(store.Values{"score": store.String("old")}), "fr": store.Object(store.Values{"score": store.String("ancien")})}),
		"rows":      store.List(store.Object(store.Values{"score": store.String("one"), "keep": store.String("kept"), "_key": store.String("row-1")}), store.Object(store.Values{"score": store.String("two"), "keep": store.String("kept"), "_key": store.String("row-2")})),
		"body":      store.List(store.Object(store.Values{"blockType": store.String("paragraph"), "score": store.String("old"), "keep": store.String("kept")}), store.Object(store.Values{"blockType": store.String("card"), "score": store.String("unchanged")})),
		"opaque":    store.Object(store.Values{"score": store.String("opaque")}),
	}
	original := store.CloneValues(values)
	reports := fieldchange.Reports(changes)
	counted, found, err := fieldchange.Process(changes, reports, posts, values, false, false)
	if err != nil || !found || !reflect.DeepEqual(values, counted) {
		t.Fatalf("count changed values: %#v, %v", counted, err)
	}
	for _, report := range reports {
		if report.Documents != 1 || report.Snapshots != 0 {
			t.Fatalf("array/locales counted repeatedly: %#v", report)
		}
	}
	cleared, found, err := fieldchange.Process(changes, reports, posts, values, true, true)
	if err != nil || !found {
		t.Fatalf("clear = %t, %v", found, err)
	}
	if !reflect.DeepEqual(original, values) {
		t.Fatal("clear mutated retained source values")
	}
	if _, exists := cleared["meta"].Lookup("score"); exists {
		t.Fatal("group value survived")
	}
	for _, localized := range cleared["localized"].Entries() {
		if _, exists := localized.Lookup("score"); exists {
			t.Fatal("localized group value survived")
		}
	}
	for item := range cleared["rows"].Elements() {
		if _, exists := item.Lookup("score"); exists {
			t.Fatal("array value survived")
		}
		if _, exists := item.Lookup("_key"); !exists {
			t.Fatal("array metadata removed")
		}
	}
	paragraph, _ := cleared["body"].ListItem(0)
	card, _ := cleared["body"].ListItem(1)
	if _, exists := paragraph.Lookup("score"); exists {
		t.Fatal("declared block value survived")
	}
	if value, _ := card.Get("score").StringValue(); value != "unchanged" {
		t.Fatal("clear crossed block discriminator")
	}
	if !reflect.DeepEqual(cleared["opaque"], values["opaque"]) {
		t.Fatal("clear rewrote opaque JSON")
	}
	for _, report := range reports {
		if report.Documents != 1 || report.Snapshots != 1 {
			t.Fatalf("snapshot counts = %#v", report)
		}
	}
}

func TestFieldKindRecoveryIgnoresPresentationAndMissingValues(t *testing.T) {
	before := changeManifest(t, false)
	afterSnapshot := before.Snapshot()
	afterSnapshot.Collections[0].Fields[0].Admin.Label = "Different presentation"
	if changes := fieldchange.Detect(before.Snapshot(), afterSnapshot); len(changes) != 0 {
		t.Fatalf("presentation generated recovery: %#v", changes)
	}
	changes := fieldchange.Detect(before.Snapshot(), changeManifest(t, true).Snapshot())
	reports := fieldchange.Reports(changes)
	if _, found, err := fieldchange.Process(changes, reports, before.Snapshot().Collections[0], store.Values{}, false, false); err != nil || found {
		t.Fatalf("absent field = %t, %v", found, err)
	}
	for _, report := range reports {
		if report.Documents != 0 || report.Snapshots != 0 {
			t.Fatalf("empty document counted: %#v", report)
		}
	}
}

func TestFieldKindRecoveryDescribesCardinalityChanges(t *testing.T) {
	resolve := func(node field.Node) schema.Manifest {
		t.Helper()
		manifest, err := core.Resolve(core.Config{Name: "Cardinality", Collections: []core.Collection{{Slug: "posts", Fields: field.Fields{node}}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	for _, test := range []struct {
		name          string
		before, after field.Node
		listBefore    bool
	}{
		{"select-to-many", field.Select("body", "one", "two"), field.MultiSelect("body", "one", "two"), false},
		{"many-to-select", field.MultiSelect("body", "one", "two"), field.Select("body", "one", "two"), true},
		{"text-to-list", field.Text("body"), field.TextList("body"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, after := resolve(test.before), resolve(test.after)
			changes := fieldchange.Detect(before.Snapshot(), after.Snapshot())
			if len(changes) != 1 || fieldchange.RequireTransform(before.Snapshot(), after.Snapshot()) == nil {
				t.Fatalf("cardinality change escaped recovery: %#v", changes)
			}
			report := fieldchange.Reports(changes)[0]
			if strings.HasSuffix(report.Before, "[]") != test.listBefore || strings.HasSuffix(report.After, "[]") == test.listBefore {
				t.Fatalf("cardinality labels = %q -> %q", report.Before, report.After)
			}
		})
	}
}

func TestFieldKindRecoveryIgnoresNullLocalizedLeaves(t *testing.T) {
	resolve := func(node field.Node) schema.Manifest {
		t.Helper()
		manifest, err := core.Resolve(core.Config{Name: "Localized recovery", Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}, Collections: []core.Collection{{Slug: "posts", Fields: field.Fields{node}}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	previous := resolve(field.Text("score").Localized()).Snapshot()
	changes := fieldchange.Detect(previous, resolve(field.Number("score").Localized()).Snapshot())
	posts := previous.Collections[0]
	for _, value := range []store.Value{store.Null(), store.Object(store.Values{}), store.Object(store.Values{"en": store.Null(), "fr": store.Null()})} {
		values := store.Values{"score": value}
		reports := fieldchange.Reports(changes)
		result, found, err := fieldchange.Process(changes, reports, posts, values, false, true)
		if err != nil || found || reports[0].Documents != 0 || !reflect.DeepEqual(values, result) {
			t.Fatalf("empty localized field counted or cleared: %#v, found=%t, reports=%#v, error=%v", value, found, reports, err)
		}
	}
	reports := fieldchange.Reports(changes)
	values := store.Values{"score": store.Object(store.Values{"en": store.Null(), "fr": store.String("retained")})}
	result, found, err := fieldchange.Process(changes, reports, posts, values, true, true)
	if err != nil || !found || reports[0].Snapshots != 1 {
		t.Fatalf("populated localized field escaped recovery: found=%t, reports=%#v, error=%v", found, reports, err)
	}
	if _, exists := result["score"]; exists {
		t.Fatal("cleared localized leaf survived")
	}
}

func resolveChange(t *testing.T, node field.Node) schema.Manifest {
	t.Helper()
	manifest, err := core.Resolve(core.Config{Name: "Kinds", Plugins: []core.Plugin{richtext.New()}, Collections: []core.Collection{{Slug: "posts", Fields: field.Fields{node}}}})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// Kinds that store the same plain string, where the target accepts every
// value the source can hold, are not kind changes. The relation is directional.
func TestFieldKindChangesAdmitOnlyFittingStoredValues(t *testing.T) {
	for _, test := range []struct {
		name          string
		before, after field.Node
		compatible    bool
	}{
		{"text-to-textarea", field.Text("body"), field.Textarea("body"), true},
		{"textarea-to-code", field.Textarea("body"), field.Code("body"), true},
		{"email-to-text", field.Email("body"), field.Text("body"), true},
		{"date-to-text", field.Date("body"), field.Text("body"), true},
		{"select-to-text", field.Select("body", "one", "two"), field.Text("body"), true},
		{"select-to-radio", field.Select("body", "one", "two"), field.Radio("body", "one", "two"), true},
		{"radio-to-wider-select", field.Radio("body", "one"), field.Select("body", "one", "two"), true},
		{"text-to-select", field.Text("body"), field.Select("body", "one", "two"), false},
		{"text-to-email", field.Text("body"), field.Email("body"), false},
		{"text-to-date", field.Text("body"), field.Date("body"), false},
		{"select-to-narrower-radio", field.Select("body", "one", "two"), field.Radio("body", "one"), false},
		{"text-to-number", field.Text("body"), field.Number("body"), false},
		{"text-to-text-list", field.Text("body"), field.TextList("body"), false},
		{"multiselect-to-text", field.MultiSelect("body", "one", "two"), field.Text("body"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, after := resolveChange(t, test.before), resolveChange(t, test.after)
			changes := fieldchange.Detect(before.Snapshot(), after.Snapshot())
			if test.compatible != (len(changes) == 0) || test.compatible != (fieldchange.RequireTransform(before.Snapshot(), after.Snapshot()) == nil) {
				t.Fatalf("compatible=%t, changes=%#v", test.compatible, changes)
			}
		})
	}
}

// Plugins own their stored envelope, so a kind change of an embedded payload
// field is attributed to the whole plugin value and can never be cleared.
func TestFieldKindChangesInsideEmbeddedPayloadsFailClosed(t *testing.T) {
	resolve := func(level field.Node) schema.Manifest {
		return resolveChange(t, field.Group("meta", field.Fields{richtext.Field("body", richtext.Config{Blocks: []field.Block{{Slug: "callout", Fields: field.Fields{field.Group("style", field.Fields{level})}}}})}))
	}
	before, after := resolve(field.Text("level")), resolve(field.Number("level"))
	if changes := fieldchange.Detect(before.Snapshot(), resolve(field.Textarea("level")).Snapshot()); len(changes) != 0 {
		t.Fatalf("compatible embedded change was reported: %#v", changes)
	}
	changes := fieldchange.Detect(before.Snapshot(), after.Snapshot())
	if len(changes) != 1 || changes[0].Payload == nil || changes[0].Before.Name != "body" || len(changes[0].Containers) != 1 {
		t.Fatalf("embedded change = %#v", changes)
	}
	report := fieldchange.Reports(changes)[0]
	if report.Path != "meta.body" || report.Payload != "callout.style.level" || report.Before != "text" || report.After != "number" {
		t.Fatalf("embedded report = %#v", report)
	}
	if err := fieldchange.RequireTransform(before.Snapshot(), after.Snapshot()); err == nil || !strings.Contains(err.Error(), "posts.meta.body embedded field callout.style.level") {
		t.Fatalf("migration creation admitted embedded change: %v", err)
	}
	if err := fieldchange.ValidateClear(changes); err == nil || !strings.Contains(err.Error(), "every complete posts.meta.body value") {
		t.Fatalf("embedded change could be cleared: %v", err)
	}
	// Any stored plugin value counts: adapters never interpret its envelope.
	reports := fieldchange.Reports(changes)
	values := store.Values{"meta": store.Object(store.Values{"body": store.Object(store.Values{"root": store.Object(store.Values{})})})}
	posts := before.Snapshot().Collections[0]
	if _, found, err := fieldchange.Process(changes, reports, posts, values, false, false); err != nil || !found || reports[0].Documents != 1 {
		t.Fatalf("embedded change count = %t, %#v, %v", found, reports, err)
	}
	if _, _, err := fieldchange.Process(changes, reports, posts, values, false, true); err == nil {
		t.Fatal("embedded change was cleared")
	}
}

// A required top-level replacement is NOT NULL on PostgreSQL and required on
// every adapter, so clearing cannot leave it empty.
func TestFieldKindClearRefusesRequiredReplacements(t *testing.T) {
	for _, test := range []struct {
		name          string
		before, after field.Node
		refused       bool
	}{
		{"required-to-required", field.Text("body").Required(), field.Number("body").Required(), true},
		{"optional-to-required", field.Text("body"), field.Number("body").Required(), true},
		{"required-to-optional", field.Text("body").Required(), field.Number("body"), false},
		{"nested-required", field.Group("meta", field.Fields{field.Text("body").Required()}), field.Group("meta", field.Fields{field.Number("body").Required()}), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, after := resolveChange(t, test.before), resolveChange(t, test.after)
			err := fieldchange.ValidateClear(fieldchange.Detect(before.Snapshot(), after.Snapshot()))
			if test.refused != (err != nil && strings.Contains(err.Error(), "stays required")) || !test.refused && err != nil {
				t.Fatalf("clear validation = %v", err)
			}
		})
	}
}
