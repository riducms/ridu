package migrationartifact

import (
	"reflect"
	"strings"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestReplacementRetainsRecordedSemanticWorkAndLeavesNewWorkPending(t *testing.T) {
	initial := File{Name: "initial", Digest: "original"}
	transform := File{Name: "transform", Digest: "reviewed"}
	squashed := File{Name: "squashed", Digest: "new"}
	blocking := func(file File) string {
		if file.Name == "transform" {
			return "data_transform"
		}
		return ""
	}
	for _, test := range []struct {
		name      string
		old, next []File
		applied   int
		end       int
		want      string
	}{
		{"schema squash", []File{initial}, []File{squashed}, 1, 1, ""},
		{"discard data", []File{initial, transform}, []File{squashed}, 2, 0, "would be discarded"},
		{"preserve data", []File{initial, transform}, []File{initial, transform, squashed}, 2, 3, ""},
		// A new data step ends what the ledger records; migrate up runs it.
		{"new data after recorded history", []File{initial}, []File{initial, transform}, 1, 1, ""},
		{"new data after squash", []File{initial}, []File{squashed, transform, {Name: "later", Digest: "pending"}}, 1, 1, ""},
		{"new data first", []File{initial}, []File{transform}, 1, 0, "has not run on this database"},
		{"changed data", []File{initial, transform}, []File{initial, {Name: "transform", Digest: "edited"}}, 2, 0, "would be discarded"},
		{"no history", nil, []File{squashed}, 0, 0, "use ridu migrate baseline"},
	} {
		t.Run(test.name, func(t *testing.T) {
			end, err := CheckReplacement(test.next, test.old, test.applied, blocking)
			if test.want == "" {
				if err != nil || end != test.end {
					t.Fatalf("end = %d, %v; want %d", end, err, test.end)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRemovedResourcesListsMissingCollectionsAndGlobals(t *testing.T) {
	old := schema.Snapshot{
		Collections: []schema.Collection{{ID: "posts"}, {ID: "drafts"}},
		Globals:     []schema.Collection{{ID: "settings"}, {ID: "footer"}},
	}
	replacement := schema.Snapshot{
		Collections: []schema.Collection{{ID: "posts"}, {ID: "pages"}},
		Globals:     []schema.Collection{{ID: "settings"}},
	}
	if removed := RemovedResources(old, replacement); !reflect.DeepEqual(removed, []schema.StableID{"drafts", "footer"}) {
		t.Fatalf("removed = %v", removed)
	}
	if err := RequireRetainedResources(old, replacement); err == nil || !strings.Contains(err.Error(), "drafts, footer") {
		t.Fatalf("refusal = %v", err)
	}
	if err := RequireRetainedResources(schema.Snapshot{Collections: replacement.Collections[:1]}, replacement); err != nil {
		t.Fatalf("added resources refused: %v", err)
	}
}
