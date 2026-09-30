package migrationartifact

import (
	"strings"
	"testing"
)

func TestAdoptionEndStopsAtTheNewestMatchingPhysicalMigration(t *testing.T) {
	files := []File{{Name: "0001_initial"}, {Name: "0002_summary"}, {Name: "0003_rename"}, {Name: "0004_later"}}
	physicalOnly := func(File) string { return "" }
	renameBlocks := func(file File) string {
		if file.Name == "0003_rename" {
			return "rename_content"
		}
		return ""
	}
	at := func(index int) func(int) (bool, error) {
		return func(candidate int) (bool, error) { return candidate == index, nil }
	}
	for _, test := range []struct {
		name     string
		applied  int
		blocking func(File) string
		schemaAt int
		want     int
		wantErr  string
	}{
		{name: "dev database at head", applied: 0, blocking: physicalOnly, schemaAt: 3, want: 4},
		{name: "dev database behind head", applied: 0, blocking: physicalOnly, schemaAt: 1, want: 2},
		{name: "ledger behind, schema ahead", applied: 1, blocking: physicalOnly, schemaAt: 2, want: 3},
		{name: "paused before a rename", applied: 0, blocking: renameBlocks, schemaAt: 1, want: 2},
		{name: "schema past a rename", applied: 0, blocking: renameBlocks, schemaAt: 3, wantErr: "0003_rename has a rename_content step"},
		{name: "already current", applied: 2, blocking: physicalOnly, schemaAt: 1, want: 2},
		{name: "unknown schema", applied: 0, blocking: physicalOnly, schemaAt: -1, wantErr: "matches no committed migration"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := AdoptionEnd(files, test.applied, test.blocking, at(test.schemaAt))
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("AdoptionEnd error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("AdoptionEnd = %d, %v; want %d", got, err, test.want)
			}
		})
	}
}
