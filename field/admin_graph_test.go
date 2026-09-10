package field

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/store"
)

func TestGraphEditAdminRoundTripsExistingLocalAndPairedEditors(t *testing.T) {
	for _, input := range []View{
		Snapshot(Text("code").Admin(Admin{Editor: Component("app:Code", store.Object(store.Values{"mode": store.String("dense")}))})),
		Snapshot(Text("code").Admin(Admin{Editor: PluginComponent("paired", "Code", store.Object(store.Values{"mode": store.String("dense")}))})),
	} {
		text, err := AsText(input)
		if err != nil {
			t.Fatal(err)
		}
		edited := Snapshot(text.EditAdmin(func(admin *Admin) { admin.Description = "Edited" }))
		oldRef, oldConfig := input.Editor()
		ref, config := edited.Editor()
		if ref != oldRef || string(config) != string(oldConfig) {
			t.Fatalf("targeted edit changed local editor: %q %s; want %q %s", ref, config, oldRef, oldConfig)
		}
		oldPlugin, oldComponent, oldConfig, oldPresent := input.AdminComponent()
		plugin, component, config, present := edited.AdminComponent()
		if plugin != oldPlugin || component != oldComponent || string(config) != string(oldConfig) || present != oldPresent {
			t.Fatal("targeted edit changed paired editor")
		}
		policy := edited.AdminPolicy()
		if policy.Description != "Edited" || policy.Editor.PluginKey != oldPlugin {
			t.Fatal("admin policy failed to retain the editor owner")
		}
		cleared := Snapshot(text.Admin(Admin{}))
		if reference, _ := cleared.Editor(); reference != "" {
			t.Fatal("whole admin replacement retained local editor")
		}
		if _, _, _, present := cleared.AdminComponent(); present {
			t.Fatal("whole admin replacement retained paired editor")
		}
	}
}

func TestGraphEditAdminRoundTripsExistingLocalAndPairedRowLabels(t *testing.T) {
	labels := RowLabels{Singular: "Entry", Plural: "Entries", SingularTranslations: map[string]string{"fr": "Entrée"}, PluralTranslations: map[string]string{"fr": "Entrées"}}
	for _, rowComponent := range []ComponentRef{Component("app:Summary", store.Object(store.Values{"mode": store.String("dense")})), PluginComponent("paired", "Summary", store.Object(store.Values{"mode": store.String("dense")}))} {
		array := Array("rows", Fields{Text("title")}).Admin(Admin{RowLabel: rowComponent, RowLabelPath: "title", RowLabels: labels, Description: "Original", DescriptionTranslations: map[string]string{"fr": "Initial"}, Tab: "Details", TabTranslations: map[string]string{"fr": "Détails"}})
		input := Snapshot(array)
		var escaped *Admin
		edited := Snapshot(array.EditAdmin(func(admin *Admin) { escaped = admin; admin.Description = "Edited" }))
		escaped.DescriptionTranslations["fr"] = "Escaped"
		escaped.TabTranslations["fr"] = "Escaped"
		escaped.RowLabels.SingularTranslations["fr"] = "Escaped"
		if !reflect.DeepEqual(input.RowLabels(), edited.RowLabels()) || edited.RowLabel() != "title" || edited.DescriptionTranslations()["fr"] != "Initial" || edited.TabTranslations()["fr"] != "Détails" {
			t.Fatal("targeted edit dropped or aliased current static presentation metadata")
		}
		oldRef, oldConfig := input.LocalRowLabel()
		ref, config := edited.LocalRowLabel()
		if ref != oldRef || string(config) != string(oldConfig) {
			t.Fatal("targeted edit changed local row label")
		}
		oldPlugin, oldComponent, oldConfig, oldPresent := input.RowLabelComponent()
		plugin, component, config, present := edited.RowLabelComponent()
		if plugin != oldPlugin || component != oldComponent || string(config) != string(oldConfig) || present != oldPresent {
			t.Fatal("targeted edit changed paired row label")
		}
		cleared := Snapshot(array.Admin(Admin{}))
		if reference, _ := cleared.LocalRowLabel(); reference != "" {
			t.Fatal("whole admin replacement retained local row label")
		}
		if _, _, _, present := cleared.RowLabelComponent(); present {
			t.Fatal("whole admin replacement retained paired row label")
		}
		if cleared.RowLabel() != "" || cleared.RowLabels().Singular != "" || cleared.Tab() != "" || len(cleared.DescriptionTranslations()) != 0 {
			t.Fatal("whole admin replacement retained omitted static settings")
		}
	}
}

func TestGraphAdminComponentDomainReplacementClearsStaleSlots(t *testing.T) {
	config := store.Object(store.Values{"mode": store.String("dense")})
	local := Text("code").Admin(Admin{Editor: Component("app:Code", config)})
	paired := local.EditAdmin(func(admin *Admin) { admin.Editor = PluginComponent("paired", "Code", config) })
	if ref, _ := Snapshot(paired).Editor(); ref != "" {
		t.Fatal("switching to paired editor retained local selection")
	}
	if plugin, component, _, present := Snapshot(paired).AdminComponent(); !present || plugin != "paired" || component != "Code" {
		t.Fatal("paired component did not lower to existing metadata")
	}
	restored := paired.EditAdmin(func(admin *Admin) { admin.Editor = Component("app:Code", config) })
	if _, _, _, present := Snapshot(restored).AdminComponent(); present {
		t.Fatal("switching to local editor retained paired selection")
	}
	row := Array("rows", Fields{Text("title")}).Admin(Admin{RowLabel: PluginComponent("paired", "Summary", config), RowLabelPath: "title"})
	row = row.EditAdmin(func(admin *Admin) { admin.RowLabel = Component("app:Summary", config) })
	if _, _, _, present := Snapshot(row).RowLabelComponent(); present {
		t.Fatal("switching to local row label retained paired selection")
	}
	if ref, _ := Snapshot(row).LocalRowLabel(); ref != "app:Summary" || Snapshot(row).RowLabel() != "title" {
		t.Fatal("local row label or accessible fallback disappeared")
	}
}

func TestGraphAdminRejectsMalformedAndNonFiniteComponentConfiguration(t *testing.T) {
	for _, component := range []ComponentRef{
		Component("invalid"),
		Component("app:Code", store.Null()),
		Component("app:Code", store.String("not an object")),
		Component("app:Code", store.Object(store.Values{"notFinite": store.Number(math.NaN())})),
		PluginComponent("Invalid", "Code"),
		PluginComponent("paired", "not.valid"),
	} {
		invalid := Text("code").Admin(Admin{Editor: component})
		issues := Snapshot(invalid).Issues()
		if len(issues) != 1 || issues[0].Code != "invalid_admin_component" || issues[0].Path != "admin.editor" {
			t.Fatalf("invalid component failed to produce a stable authored diagnostic: %#v", issues)
		}
		if len(Snapshot(invalid.EditAdmin(func(admin *Admin) { admin.Description = "Unrelated" })).Issues()) != 1 {
			t.Fatal("unrelated edit silently discarded invalid component configuration")
		}
		if len(Snapshot(invalid.Admin(Admin{})).Issues()) != 0 {
			t.Fatal("whole replacement retained stale component diagnostics")
		}
		if _, err := json.Marshal(component); err == nil {
			t.Fatal("invalid component serialized without a diagnostic")
		}
	}
	if issues := Snapshot(Text("code").Admin(Admin{Editor: Component("app:Code")})).Issues(); len(issues) != 0 {
		t.Fatalf("absent optional component configuration rejected: %#v", issues)
	}
}

func TestGraphEditAdminPreservesConditionAndOtherPresentation(t *testing.T) {
	input := Snapshot(Text("code").Admin(Admin{Placeholder: "Enter code", PlaceholderTranslations: map[string]string{"fr": "Code"}, VisibleWhen: Equal(Sibling("kind"), "link")}))
	text, err := AsText(input)
	if err != nil {
		t.Fatal(err)
	}
	edited := Snapshot(text.EditAdmin(func(admin *Admin) { admin.Description = "Edited" }))
	if !reflect.DeepEqual(input.AdminPolicy().VisibleWhen, edited.AdminPolicy().VisibleWhen) || edited.PlaceholderTranslations()["fr"] != "Code" {
		t.Fatal("targeted admin edit lost condition or placeholder translations")
	}
	if !Snapshot(text.Admin(Admin{})).AdminPolicy().VisibleWhen.IsZero() {
		t.Fatal("whole admin replacement retained condition")
	}
	layout := Collapsible("details", Fields{Text("code")}).Admin(Admin{InitiallyCollapsed: true})
	if !Snapshot(layout.EditAdmin(func(admin *Admin) { admin.Description = "Edited" })).InitiallyCollapsed() {
		t.Fatal("targeted edit lost collapsed presentation")
	}
}

func TestOptionalComponentConfigurationPreservesAbsentVersusSupplied(t *testing.T) {
	for _, component := range []ComponentRef{Component("app:Code"), PluginComponent("paired", "Code")} {
		encoded, err := json.Marshal(component)
		if err != nil || strings.Contains(string(encoded), `"config"`) {
			t.Fatalf("absent config serialized as supplied: %s %v", encoded, err)
		}
	}
	empty := Component("app:Code", store.Object(store.Values{}))
	encoded, err := json.Marshal(empty)
	if err != nil || !strings.Contains(string(encoded), `"config":{}`) {
		t.Fatalf("explicit object config was dropped: %s %v", encoded, err)
	}
	for _, invalid := range []ComponentRef{
		Component("app:Code", store.Value{}),
		PluginComponent("paired", "Code", store.Value{}),
		Component("app:Code", store.Object(nil), store.Object(nil)),
	} {
		if invalid.Err() == nil {
			t.Fatalf("invalid supplied configuration accepted: %#v", invalid)
		}
	}
}
