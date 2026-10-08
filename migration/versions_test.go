package migration

import (
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestVersionsEnabledFollowsSurvivingIdentity(t *testing.T) {
	base := schema.Collection{ID: "posts", Slug: "posts"}
	versioned := base
	versioned.Versions = &schema.VersionSettings{MaxPerDocument: 2}
	versioned.Capabilities.Versions = true
	renamed := schema.Collection{ID: "articles", Slug: "articles", Versions: versioned.Versions, Capabilities: versioned.Capabilities}
	for _, test := range []struct {
		name    string
		before  schema.Snapshot
		after   schema.Snapshot
		renames map[schema.StableID]schema.StableID
		enabled schema.StableID
	}{
		{name: "existing collection", before: schema.Snapshot{Collections: []schema.Collection{base}}, after: schema.Snapshot{Collections: []schema.Collection{versioned}}, enabled: "posts"},
		{name: "existing global", before: schema.Snapshot{Globals: []schema.Global{base}}, after: schema.Snapshot{Globals: []schema.Global{versioned}}, enabled: "posts"},
		{name: "confirmed rename", before: schema.Snapshot{Collections: []schema.Collection{base}}, after: schema.Snapshot{Collections: []schema.Collection{renamed}}, renames: map[schema.StableID]schema.StableID{"posts": "articles"}, enabled: "articles"},
		{name: "already versioned", before: schema.Snapshot{Collections: []schema.Collection{versioned}}, after: schema.Snapshot{Collections: []schema.Collection{versioned}}},
		{name: "new collection", after: schema.Snapshot{Collections: []schema.Collection{versioned}}},
		{name: "retired and new collection", before: schema.Snapshot{Collections: []schema.Collection{base}}, after: schema.Snapshot{Collections: []schema.Collection{renamed}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			enabled := VersionsEnabled(test.before, test.after, test.renames)
			if test.enabled == "" && len(enabled) != 0 || test.enabled != "" && (len(enabled) != 1 || enabled[0].ID != test.enabled) {
				t.Fatalf("VersionsEnabled = %#v, want %q", enabled, test.enabled)
			}
		})
	}
}
