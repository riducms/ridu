package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestPostgresReversedRelationshipUpdatesReturnStableRetryableConflicts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	barrier := newPostgresLockCycleBarrier(2)
	config := ridu.Config{Name: "PostgreSQL relationship deadlock availability", Collections: []ridu.Collection{{
		Slug:   "nodes",
		Fields: field.Fields{field.Text("name").Required(), field.Text("attempt").Required(), field.Relationship("peer", "nodes")},
		Hooks: ridu.CollectionHooks{BeforeValidate: []ridu.Hook{func(hook ridu.HookContext) error {
			if hook.Operation != operation.Update || hook.Original == nil {
				return nil
			}
			attempt, _ := hook.Data["attempt"].StringValue()
			if attempt != "cycle-a" && attempt != "cycle-b" {
				return nil
			}
			return barrier.Wait(hook.Context)
		}}},
	}}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	nodeA, err := application.Local().Create(ctx, "nodes", store.Values{
		"name": store.String("A"), "attempt": store.String("initial-a"),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	nodeB, err := application.Local().Create(ctx, "nodes", store.Values{
		"name": store.String("B"), "attempt": store.String("initial-b"),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}

	type updateAttempt struct {
		label, id, target, initial, value string
		err                               error
	}
	specifications := []updateAttempt{
		{label: "A to B", id: nodeA.ID, target: nodeB.ID, initial: "initial-a", value: "cycle-a"},
		{label: "B to A", id: nodeB.ID, target: nodeA.ID, initial: "initial-b", value: "cycle-b"},
	}
	start := make(chan struct{})
	results := make(chan updateAttempt, len(specifications))
	for _, specification := range specifications {
		specification := specification
		go func() {
			<-start
			operationContext, operationCancel := context.WithTimeout(ctx, 8*time.Second)
			defer operationCancel()
			_, specification.err = application.Local().Update(operationContext, "nodes", specification.id, store.Values{
				"attempt": store.String(specification.value), "peer": store.String(specification.target),
			}, ridu.MutationOptions{})
			results <- specification
		}()
	}
	close(start)
	outcomes := []updateAttempt{<-results, <-results}
	conflicts, successes := 0, 0
	for _, outcome := range outcomes {
		if outcome.err == nil {
			successes++
			continue
		}
		assertPostgresAvailabilityConflict(t, outcome.label, outcome.err)
		conflicts++
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("reversed relationship outcomes = successes %d conflicts %d: %#v", successes, conflicts, outcomes)
	}

	var retry updateAttempt
	for _, outcome := range outcomes {
		document, findError := application.Local().Find(ctx, "nodes", outcome.id, ridu.FindOptions{})
		if findError != nil {
			t.Fatal(findError)
		}
		if outcome.err != nil {
			if got := stringValue(document.Values["attempt"]); got != outcome.initial || document.Values["peer"].Kind() != store.ValueNull {
				t.Fatalf("failed %s update left partial state: %#v", outcome.label, document.Values)
			}
			retry = outcome
			continue
		}
		if got := stringValue(document.Values["attempt"]); got != outcome.value || stringValue(document.Values["peer"]) != outcome.target {
			t.Fatalf("successful %s update state = %#v", outcome.label, document.Values)
		}
	}
	if retry.id == "" {
		t.Fatal("reversed relationship updates did not produce a retryable loser")
	}
	retryContext, retryCancel := context.WithTimeout(ctx, 5*time.Second)
	defer retryCancel()
	if _, err := application.Local().Update(retryContext, "nodes", retry.id, store.Values{
		"attempt": store.String(retry.value), "peer": store.String(retry.target),
	}, ridu.MutationOptions{}); err != nil {
		t.Fatalf("caller retry after the relationship lock cycle failed: %v", err)
	}
	retried, err := application.Local().Find(ctx, "nodes", retry.id, ridu.FindOptions{})
	if err != nil || stringValue(retried.Values["attempt"]) != retry.value || stringValue(retried.Values["peer"]) != retry.target {
		t.Fatalf("retried relationship update = %#v, %v", retried.Values, err)
	}
}

func TestPostgresReversedExecuteBatchOperationsReturnStableRetryableConflicts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	barrier := newPostgresLockCycleBarrier(2)
	var firstID, secondID string
	config := ridu.Config{Name: "PostgreSQL batch deadlock availability", Collections: []ridu.Collection{{
		Slug:   "nodes",
		Fields: field.Fields{field.Text("name").Required(), field.Text("leftMark").Required(), field.Text("rightMark").Required()},
		Hooks: ridu.CollectionHooks{BeforeValidate: []ridu.Hook{func(hook ridu.HookContext) error {
			if hook.Operation != operation.Update || hook.Original == nil {
				return nil
			}
			_, left := hook.Data["leftMark"]
			_, right := hook.Data["rightMark"]
			if left && hook.Original.ID == firstID || right && hook.Original.ID == secondID {
				return barrier.Wait(hook.Context)
			}
			return nil
		}}},
	}}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.Local().Create(ctx, "nodes", store.Values{
		"name": store.String("First"), "leftMark": store.String("left-initial"), "rightMark": store.String("right-initial"),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Local().Create(ctx, "nodes", store.Values{
		"name": store.String("Second"), "leftMark": store.String("left-initial"), "rightMark": store.String("right-initial"),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	firstID, secondID = first.ID, second.ID

	type batchAttempt struct {
		label, field, initial, value string
		ids                          []string
		err                          error
	}
	specifications := []batchAttempt{
		{label: "left A then B", field: "leftMark", initial: "left-initial", value: "left-attempt", ids: []string{first.ID, second.ID}},
		{label: "right B then A", field: "rightMark", initial: "right-initial", value: "right-attempt", ids: []string{second.ID, first.ID}},
	}
	start := make(chan struct{})
	results := make(chan batchAttempt, len(specifications))
	for _, specification := range specifications {
		specification := specification
		go func() {
			<-start
			operationContext, operationCancel := context.WithTimeout(ctx, 8*time.Second)
			defer operationCancel()
			_, specification.err = application.Local().BulkUpdate(operationContext, "nodes", specification.ids, store.Values{
				specification.field: store.String(specification.value),
			}, ridu.BulkOptions{})
			results <- specification
		}()
	}
	close(start)
	outcomes := []batchAttempt{<-results, <-results}
	conflicts, successes := 0, 0
	for _, outcome := range outcomes {
		if outcome.err == nil {
			successes++
			continue
		}
		assertPostgresAvailabilityConflict(t, outcome.label, outcome.err)
		conflicts++
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("reversed batch outcomes = successes %d conflicts %d: %#v", successes, conflicts, outcomes)
	}

	documents := make([]store.Document, 0, 2)
	for _, id := range []string{first.ID, second.ID} {
		document, findError := application.Local().Find(ctx, "nodes", id, ridu.FindOptions{})
		if findError != nil {
			t.Fatal(findError)
		}
		documents = append(documents, document)
	}
	var retry batchAttempt
	for _, outcome := range outcomes {
		expected := outcome.value
		if outcome.err != nil {
			expected = outcome.initial
			retry = outcome
		}
		for _, document := range documents {
			if got := stringValue(document.Values[outcome.field]); got != expected {
				t.Fatalf("%s transaction left partial %s state on %s: got %q want %q", outcome.label, outcome.field, document.ID, got, expected)
			}
		}
	}
	if retry.field == "" {
		t.Fatal("reversed ExecuteBatch operations did not produce a retryable loser")
	}
	retryContext, retryCancel := context.WithTimeout(ctx, 5*time.Second)
	defer retryCancel()
	if _, err := application.Local().BulkUpdate(retryContext, "nodes", retry.ids, store.Values{
		retry.field: store.String(retry.value),
	}, ridu.BulkOptions{}); err != nil {
		t.Fatalf("caller retry after the ExecuteBatch lock cycle failed: %v", err)
	}
	for _, id := range []string{first.ID, second.ID} {
		document, findError := application.Local().Find(ctx, "nodes", id, ridu.FindOptions{})
		if findError != nil || stringValue(document.Values[retry.field]) != retry.value {
			t.Fatalf("retried ExecuteBatch value on %s = %#v, %v", id, document.Values, findError)
		}
	}
}

type postgresLockCycleBarrier struct {
	mu       sync.Mutex
	want     int
	arrivals int
	release  chan struct{}
}

func newPostgresLockCycleBarrier(want int) *postgresLockCycleBarrier {
	return &postgresLockCycleBarrier{want: want, release: make(chan struct{})}
}

func (barrier *postgresLockCycleBarrier) Wait(ctx context.Context) error {
	barrier.mu.Lock()
	barrier.arrivals++
	if barrier.arrivals == barrier.want {
		close(barrier.release)
	}
	release := barrier.release
	barrier.mu.Unlock()
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func assertPostgresAvailabilityConflict(t *testing.T, label string, err error) {
	t.Helper()
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "conflict" || operationError.Status != 409 {
		t.Fatalf("%s lock-cycle error = %#v, %v; want stable 409 conflict", label, operationError, err)
	}
}
