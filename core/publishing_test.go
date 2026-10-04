package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/field"
	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

type publicationCancellationRaceStore struct {
	*teststore.Store
	beforeBegin func()
}

func (backend *publicationCancellationRaceStore) Begin(ctx context.Context) (store.Transaction, error) {
	if callback := backend.beforeBegin; callback != nil {
		backend.beforeBegin = nil
		callback()
	}
	return backend.Store.Begin(ctx)
}

func TestScheduledPublicationFromTaskRejectsInvalidMetadata(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{`{"collectionID":"posts","documentID":"post-1","expectedRevision":1}`, "invalid publication action"},
		{`{"action":"archive","collectionID":"posts","documentID":"post-1","expectedRevision":1}`, "invalid publication action"},
		{`{"action":"publish","collectionID":"posts","documentID":"post-1","expectedRevision":1,"timeZone":"Invalid/Timezone"}`, "invalid timezone"},
	} {
		_, err := scheduledPublicationFromTask(store.Task{ID: "task-1", Input: []byte(test.input)})
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("scheduled task error = %v, want %q", err, test.want)
		}
	}
}

func TestCancelScheduledPublicationRejectsPayloadTargetMismatch(t *testing.T) {
	backend := teststore.New()
	application, err := New(Config{Name: "scheduled target validation", Collections: []Collection{{
		Slug: "posts", Versions: true, VersionConfig: VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").Required()},
	}}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Scheduled")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job, err := application.SchedulePublish(t.Context(), "posts", document.ID, time.Now().Add(time.Hour), PublicationScheduleOptions{ExpectedRevision: document.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := backend.FindTask(t.Context(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	task.Input = []byte(`{"action":"publish","collectionID":"posts","documentID":"other","expectedRevision":1}`)
	if err := backend.DismissTaskForTarget(t.Context(), job.ID, task.Slug, *task.Target); err != nil {
		t.Fatal(err)
	}
	malformed, err := backend.EnqueueTask(t.Context(), task)
	if err != nil {
		t.Fatal(err)
	}
	err = application.CancelScheduledPublication(t.Context(), "posts", document.ID, malformed.ID, nil)
	var operationError *operationengine.Error
	if !errors.As(err, &operationError) || operationError.Code != "not_found" {
		t.Fatalf("payload target mismatch error = %v, want not_found", err)
	}
	if _, err := backend.FindTask(t.Context(), malformed.ID); err != nil {
		t.Fatalf("malformed task was dismissed: %v", err)
	}
}

func TestCancelScheduledPublicationKeepsFilteredAccessAtomicWithDismissal(t *testing.T) {
	title, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	publishScheduled := func(AccessContext) (AccessDecision, error) {
		return Where(query.Equal(title, "Scheduled")), nil
	}
	backend := &publicationCancellationRaceStore{Store: teststore.New()}
	application, err := New(Config{Name: "atomic scheduled cancellation", Collections: []Collection{{
		Slug: "posts", Versions: true, VersionConfig: VersionConfig{Drafts: true},
		Access: CollectionAccess{Publish: publishScheduled},
		Fields: field.Fields{field.Text("title").Required()},
	}}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Scheduled")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job, err := application.SchedulePublish(t.Context(), "posts", document.ID, time.Now().Add(time.Hour), PublicationScheduleOptions{ExpectedRevision: document.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var mutationError error
	backend.beforeBegin = func() {
		_, mutationError = application.Local().Update(t.Context(), "posts", document.ID, store.Values{"title": store.String("Blocked")}, MutationOptions{})
	}
	err = application.CancelScheduledPublication(t.Context(), "posts", document.ID, job.ID, nil)
	if mutationError != nil {
		t.Fatalf("interleaved document mutation: %v", mutationError)
	}
	var operationError *operationengine.Error
	if !errors.As(err, &operationError) || operationError.Code != "access_denied" {
		t.Fatalf("cancellation error = %v, want access_denied", err)
	}
	if _, err := backend.FindTask(t.Context(), job.ID); err != nil {
		t.Fatalf("task was dismissed after access changed: %v", err)
	}
}

func TestCancelScheduledPublicationDoesNotEvaluateUnrelatedAccessRules(t *testing.T) {
	backend := teststore.New()
	application, err := New(Config{Name: "focused publication cancellation", Collections: []Collection{{
		Slug: "posts", Versions: true, VersionConfig: VersionConfig{Drafts: true},
		Access: CollectionAccess{
			Publish: func(AccessContext) (AccessDecision, error) { return Allow(), nil },
			Delete: func(AccessContext) (AccessDecision, error) {
				return AccessDecision{}, errors.New("delete access must not run")
			},
		},
		Fields: field.Fields{field.Text("title").Required()},
	}}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Scheduled")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job, err := application.SchedulePublish(t.Context(), "posts", document.ID, time.Now().Add(time.Hour), PublicationScheduleOptions{ExpectedRevision: document.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.CancelScheduledPublication(t.Context(), "posts", document.ID, job.ID, nil); err != nil {
		t.Fatalf("cancel scheduled publication: %v", err)
	}
}

func TestVersionsOnlyCollectionDoesNotAdvertiseOrPerformUnpublish(t *testing.T) {
	application, err := New(Config{Name: "versions without drafts", Collections: []Collection{{
		Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Published")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := application.Local().Capabilities(t.Context(), "posts", document.ID, CapabilityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !capabilities.Operations.Publish || capabilities.Operations.Unpublish {
		t.Fatalf("publication capabilities = publish:%t unpublish:%t", capabilities.Operations.Publish, capabilities.Operations.Unpublish)
	}
	_, err = application.Local().Unpublish(t.Context(), "posts", document.ID, MutationOptions{ExpectedRevision: document.Revision})
	var operationError *operationengine.Error
	if !errors.As(err, &operationError) || operationError.Code != "bad_operation" || operationError.Message != "collection does not support drafts" {
		t.Fatalf("unpublish error = %v, want draft bad_operation", err)
	}
	unchanged, err := application.Local().Find(t.Context(), "posts", document.ID, FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != store.StatusPublished || unchanged.Revision != document.Revision {
		t.Fatalf("document changed after rejected unpublish: %#v", unchanged)
	}
}

func TestUnpublishRequiresPublishedDocument(t *testing.T) {
	application, err := New(Config{Name: "unpublish state", Collections: []Collection{{
		Slug: "posts", Versions: true, VersionConfig: VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	draft, err := application.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Draft")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.Local().Unpublish(t.Context(), "posts", draft.ID, MutationOptions{ExpectedRevision: draft.Revision})
	var operationError *operationengine.Error
	if !errors.As(err, &operationError) || operationError.Code != "validation" || operationError.Message != "only a published document can be unpublished" {
		t.Fatalf("unpublish draft error = %v, want published-document validation", err)
	}
	draftMode := true
	unchanged, err := application.Local().Find(t.Context(), "posts", draft.ID, FindOptions{Draft: &draftMode, System: true})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Status != store.StatusDraft || unchanged.Revision != draft.Revision {
		t.Fatalf("draft changed after rejected unpublish: %#v", unchanged)
	}
}

func TestRunningScheduledPublicationIsNotListedOrDismissed(t *testing.T) {
	backend := teststore.New()
	application, err := New(Config{Name: "running publication cancellation", Collections: []Collection{{
		Slug: "posts", Versions: true, VersionConfig: VersionConfig{Drafts: true},
		Fields: field.Fields{field.Text("title").Required()},
	}}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Scheduled")}, MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	document, err = application.Local().Publish(t.Context(), "posts", document.ID, MutationOptions{ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	job, err := application.SchedulePublish(t.Context(), "posts", document.ID, time.Now().Add(-time.Second), PublicationScheduleOptions{ExpectedRevision: document.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := backend.ClaimTasks(t.Context(), store.TaskClaim{Limit: 1, Slugs: []string{builtinPublishTask}, LeaseDuration: time.Minute})
	if err != nil || len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("claimed scheduled publication = %#v, %v", claimed, err)
	}
	listed, err := application.ScheduledPublications(t.Context(), "posts", document.ID, nil)
	if err != nil || len(listed) != 0 {
		t.Fatalf("listed running publication = %#v, %v", listed, err)
	}
	err = application.CancelScheduledPublication(t.Context(), "posts", document.ID, job.ID, nil)
	var operationError *operationengine.Error
	if !errors.As(err, &operationError) || operationError.Code != "not_found" {
		t.Fatalf("cancel running publication error = %v, want not_found", err)
	}
	retained, err := backend.FindTask(t.Context(), job.ID)
	if err != nil || retained.State != store.TaskStateRunning {
		t.Fatalf("running publication after cancellation attempt = %#v, %v", retained, err)
	}
}

func TestScheduledPublicationRetainsTimeZoneWithoutChangingInstant(t *testing.T) {
	backend := teststore.New()
	config := Config{Name: "schedule timezones", Collections: []Collection{{Slug: "posts", Versions: true, VersionConfig: VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required()}}}}
	application, err := New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	draft := false
	document, err := application.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Scheduled")}, MutationOptions{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	runAt := time.Date(2030, 3, 31, 1, 30, 0, 0, time.UTC)
	for _, zone := range []string{"", "UTC", "Europe/London", "America/New_York", "Asia/Kolkata", "+05:30"} {
		t.Run(zone, func(t *testing.T) {
			job, err := application.SchedulePublish(t.Context(), "posts", document.ID, runAt, PublicationScheduleOptions{TimeZone: zone}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if job.TimeZone != zone || !job.RunAt.Equal(runAt) || job.ExpectedRevision != document.Revision {
				t.Fatalf("schedule = %#v", job)
			}
			reopened, err := New(config, backend)
			if err != nil {
				t.Fatal(err)
			}
			jobs, err := reopened.ScheduledPublications(t.Context(), "posts", document.ID, nil)
			if err != nil || len(jobs) != 1 || jobs[0].TimeZone != zone || !jobs[0].RunAt.Equal(runAt) {
				t.Fatalf("reloaded schedules = %#v, %v", jobs, err)
			}
			if err := reopened.CancelScheduledPublication(t.Context(), "posts", document.ID, job.ID, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, zone := range []string{"London", "Europe/Unknown", " Europe/London", "../London", "+24:00"} {
		_, err := application.SchedulePublish(t.Context(), "posts", document.ID, runAt, PublicationScheduleOptions{TimeZone: zone}, nil)
		var operationError *operationengine.Error
		if !errors.As(err, &operationError) || operationError.Code != "validation" {
			t.Fatalf("timezone %q error = %v", zone, err)
		}
	}
	jobs, err := application.ScheduledPublications(t.Context(), "posts", document.ID, nil)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("invalid timezone queued a job: %#v, %v", jobs, err)
	}
}
