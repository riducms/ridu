package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

func TestSQLiteConcurrentIncrementsReturnDistinctConsecutiveValues(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{Name: "SQLite increments", Collections: []ridu.Collection{
		{Slug: "teams", Fields: field.Fields{field.Text("key").Required(), field.Number("nextIssue")}},
	}}
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "increments.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	local, system := application.Local(), ridu.MutationOptions{System: true}
	team, err := local.Create(ctx, "teams", store.Values{"key": store.String("ENG")}, system)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	numbers := make([]float64, 24)
	failures := make([]error, len(numbers))
	for index := range numbers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			updated, err := local.Increment(ctx, "teams", team.ID, "nextIssue", 1, system)
			numbers[index], _ = updated.Values["nextIssue"].NumberValue()
			failures[index] = err
		}()
	}
	wait.Wait()
	if err := errors.Join(failures...); err != nil {
		t.Fatal(err)
	}
	sort.Float64s(numbers)
	for index, number := range numbers {
		if number != float64(index+1) {
			t.Fatalf("concurrent increments returned %v, want each of 1 to %d once", numbers, len(numbers))
		}
	}
}
