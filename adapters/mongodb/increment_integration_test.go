package mongodb

import (
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

// MongoDB fails a concurrent locker with a conflict instead of waiting, so
// each increment retries until it wins the lock; no two may read one value.
func TestMongoDBConcurrentIncrementsReturnDistinctConsecutiveValues(t *testing.T) {
	backend := mongoIntegrationStore(t)
	application, err := ridu.New(ridu.Config{Name: "MongoDB increments", Collections: []ridu.Collection{
		{Slug: "teams", Fields: field.Fields{field.Text("key").Required(), field.Number("nextIssue")}},
	}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.syncIndexes(t.Context(), application.Manifest()); err != nil {
		t.Fatal(err)
	}
	local, system := application.Local(), ridu.MutationOptions{System: true}
	team, err := local.Create(t.Context(), "teams", store.Values{"key": store.String("ENG")}, system)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	numbers := make([]float64, 12)
	failures := make([]error, len(numbers))
	for index := range numbers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for attempt := 0; ; attempt++ {
				updated, err := local.Increment(t.Context(), "teams", team.ID, "nextIssue", 1, system)
				var operationError *ridu.OperationError
				if errors.As(err, &operationError) && operationError.Code == "conflict" && attempt < 200 {
					time.Sleep(time.Duration(attempt%10+1) * time.Millisecond)
					continue
				}
				numbers[index], _ = updated.Values["nextIssue"].NumberValue()
				failures[index] = err
				return
			}
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
