package schemadiff

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestRejectVersionsEnableFollowsSurvivingIdentity(t *testing.T) {
	base := schema.Collection{ID: "posts", Slug: "posts"}
	versioned := base
	versioned.Versions = &schema.VersionSettings{MaxPerDocument: 2}
	versioned.Capabilities.Versions = true
	for _, test := range []struct {
		name    string
		before  schema.Snapshot
		after   schema.Snapshot
		renames map[schema.StableID]schema.StableID
		blocked bool
	}{
		{name: "existing collection", before: schema.Snapshot{Collections: []schema.Collection{base}}, after: schema.Snapshot{Collections: []schema.Collection{versioned}}, blocked: true},
		{name: "existing global", before: schema.Snapshot{Globals: []schema.Global{base}}, after: schema.Snapshot{Globals: []schema.Global{versioned}}, blocked: true},
		{name: "confirmed rename", before: schema.Snapshot{Collections: []schema.Collection{base}}, after: schema.Snapshot{Collections: []schema.Collection{{ID: "articles", Slug: "articles", Versions: versioned.Versions, Capabilities: versioned.Capabilities}}}, renames: map[schema.StableID]schema.StableID{"posts": "articles"}, blocked: true},
		{name: "new collection", after: schema.Snapshot{Collections: []schema.Collection{versioned}}},
		{name: "retired and new collection", before: schema.Snapshot{Collections: []schema.Collection{base}}, after: schema.Snapshot{Collections: []schema.Collection{{ID: "articles", Slug: "articles", Versions: versioned.Versions, Capabilities: versioned.Capabilities}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := RejectVersionsEnable(test.before, test.after, test.renames)
			if test.blocked && (err == nil || !strings.Contains(err.Error(), "RIDU_VERSIONS_ENABLE_UNSUPPORTED")) {
				t.Fatalf("version enable = %v, want unsupported transition", err)
			}
			if !test.blocked && err != nil {
				t.Fatalf("new versioned resource = %v", err)
			}
		})
	}
}
