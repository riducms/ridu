package core_test

import (
	"strings"
	"testing"

	rootridu "github.com/riducms/ridu"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/cascade"
)

func TestLocalCascadeDeletes(t *testing.T) {
	cascade.Run(t, func(t *testing.T, config rootridu.Config) (store.Store, *rootridu.App) {
		t.Helper()
		backend := teststore.New()
		app, err := rootridu.New(config, backend)
		if err != nil {
			t.Fatal(err)
		}
		return backend, app
	})
}

// Cascade deletes the whole owner, so it only makes sense where one owner
// holds one reference: not has-many, repeated, localized or global fields.
func TestCascadeIsRejectedWhereItWouldDeleteForOneOfMany(t *testing.T) {
	learners := ridu.Collection{Slug: "learners", Fields: field.Fields{field.Text("name")}}
	for name, owner := range map[string]ridu.Collection{
		"has-many":  {Slug: "groups", Fields: field.Fields{field.Relationships("members", "learners").OnDelete(field.ReferenceDeleteCascade)}},
		"array row": {Slug: "groups", Fields: field.Fields{field.Array("rows", field.Fields{field.Relationship("learner", "learners").OnDelete(field.ReferenceDeleteCascade)})}},
		"localized": {Slug: "groups", Fields: field.Fields{field.Relationship("learner", "learners").Localized().OnDelete(field.ReferenceDeleteCascade)}},
	} {
		config := ridu.Config{Name: "Cascade shape", Collections: []ridu.Collection{learners, owner}}
		if name == "localized" {
			config.Localization = ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}}
		}
		if _, err := ridu.New(config, teststore.New()); err == nil || !strings.Contains(err.Error(), "cascade") {
			t.Errorf("%s cascade = %v, want a cascade error", name, err)
		}
	}
	global := ridu.Config{Name: "Cascade global", Collections: []ridu.Collection{learners}, Globals: []ridu.Global{{Slug: "settings", Fields: field.Fields{field.Relationship("owner", "learners").OnDelete(field.ReferenceDeleteCascade)}}}}
	if _, err := ridu.New(global, teststore.New()); err == nil || !strings.Contains(err.Error(), "cascade") {
		t.Errorf("global cascade = %v, want a cascade error", err)
	}
}
