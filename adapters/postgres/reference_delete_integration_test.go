package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func TestPostgresHardDeleteReconcilesCurrentOwnersWithoutObservableOwnerWrites(t *testing.T) {
	ctx := context.Background()
	actor := &store.Document{ID: "editor"}
	automaticOwnerIDs := make(map[string]bool)
	automaticOwnerUpdates := 0
	config := ridu.Config{Name: "PostgreSQL reference deletes", Collections: []ridu.Collection{
		{Slug: "users", Fields: field.Fields{field.Text("name")}},
		{Slug: "teams", Fields: field.Fields{field.Text("name")}},
		{
			Slug: "posts", Trash: true, Versions: true,
			Fields: field.Fields{field.Text("title"), field.Relationship("guard", "users").OnDelete(field.ReferenceDeleteRestrict), field.Relationship("owner", "users"), field.Relationships("related", "users"), field.PolymorphicRelationship("subject", "users", "teams")},
			Hooks: ridu.CollectionHooks{AfterChange: []ridu.Hook{func(hook ridu.HookContext) error {
				if hook.Operation == operation.Update && hook.Document != nil && automaticOwnerIDs[hook.Document.ID] {
					automaticOwnerUpdates++
				}
				return nil
			}}},
		},
	}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Import(ctx, "users", store.Values{"name": store.String("Person")}, ridu.ImportOptions{ID: "shared", Status: store.StatusPublished, Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Import(ctx, "teams", store.Values{"name": store.String("Team")}, ridu.ImportOptions{ID: target.ID, Status: store.StatusPublished, Actor: actor}); err != nil {
		t.Fatal(err)
	}
	restricted, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Restricted"), "guard": store.String(target.ID),
	}, ridu.MutationOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	teamReference := store.Object(store.Values{"relationTo": store.String("teams"), "id": store.String(target.ID)})
	active, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Active"), "owner": store.String(target.ID),
		"related": store.List(store.String(target.ID), store.String(target.ID)), "subject": teamReference,
	}, ridu.MutationOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	trash, err := application.Local().Create(ctx, "posts", store.Values{
		"title": store.String("Trash"), "owner": store.String(target.ID),
		"related": store.List(store.String(target.ID), store.String(target.ID)), "subject": teamReference,
	}, ridu.MutationOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	automaticOwnerIDs[active.ID], automaticOwnerIDs[trash.ID] = true, true
	trash, err = application.Local().Delete(ctx, "posts", trash.ID, ridu.MutationOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}

	_, err = application.Local().Delete(ctx, "users", target.ID, ridu.MutationOptions{Actor: actor})
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "delete_restricted" || operationError.Status != 409 ||
		operationError.Message != "document deletion is restricted by current references" {
		t.Fatalf("restricted PostgreSQL delete = %#v, %v", operationError, err)
	}
	for _, secret := range []string{restricted.ID, active.ID, trash.ID} {
		if strings.Contains(operationError.Message, secret) {
			t.Fatalf("restricted error disclosed owner ID %q: %q", secret, operationError.Message)
		}
	}
	unchanged, err := application.Local().Find(ctx, "posts", active.ID, ridu.FindOptions{Actor: actor})
	if err != nil || stringValue(unchanged.Values["owner"]) != target.ID {
		t.Fatalf("restriction partially reconciled an owner: %#v, %v", unchanged.Values, err)
	}
	if _, err := application.Local().Find(ctx, "users", target.ID, ridu.FindOptions{Actor: actor}); err != nil {
		t.Fatalf("restricted target disappeared: %v", err)
	}
	if _, err := application.Local().PublishChanges(ctx, "posts", restricted.ID, store.Values{"guard": store.Null()}, ridu.MutationOptions{Actor: actor, ExpectedRevision: restricted.Revision}); err != nil {
		t.Fatal(err)
	}
	activeBefore, err := application.Local().Find(ctx, "posts", active.ID, ridu.FindOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	trashBefore, err := application.Local().Find(ctx, "posts", trash.ID, ridu.FindOptions{Actor: actor, TrashOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	activeVersionsBefore, err := application.Local().Versions(ctx, "posts", active.ID, ridu.FindOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	trashVersionsBefore, err := application.Local().Versions(ctx, "posts", trash.ID, ridu.FindOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	automaticOwnerUpdates = 0

	if _, err := application.Local().Delete(ctx, "users", target.ID, ridu.MutationOptions{Actor: actor}); err != nil {
		t.Fatal(err)
	}
	activeAfter, err := application.Local().Find(ctx, "posts", active.ID, ridu.FindOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	trashAfter, err := application.Local().Find(ctx, "posts", trash.ID, ridu.FindOptions{Actor: actor, TrashOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	for label, document := range map[string]store.Document{"active": activeAfter, "trash": trashAfter} {
		if document.Values["owner"].Kind() != store.ValueNull {
			t.Errorf("%s singular reference = %#v", label, document.Values["owner"])
		}
		if related, _ := document.Values["related"].CopyList(); len(related) != 0 {
			t.Errorf("%s duplicate members = %#v", label, related)
		}
		subject, _ := document.Values["subject"].CopyObject()
		if relationTo, id := stringValue(subject["relationTo"]), stringValue(subject["id"]); relationTo != "teams" || id != target.ID {
			t.Errorf("%s same-ID cross-collection relation = %#v", label, subject)
		}
	}
	if !activeAfter.UpdatedAt.Equal(activeBefore.UpdatedAt) || activeAfter.Revision != activeBefore.Revision ||
		!trashAfter.UpdatedAt.Equal(trashBefore.UpdatedAt) || trashAfter.Revision != trashBefore.Revision {
		t.Fatalf("automatic PostgreSQL reconciliation changed owner metadata: active %s/%d -> %s/%d, trash %s/%d -> %s/%d",
			activeBefore.UpdatedAt, activeBefore.Revision, activeAfter.UpdatedAt, activeAfter.Revision,
			trashBefore.UpdatedAt, trashBefore.Revision, trashAfter.UpdatedAt, trashAfter.Revision)
	}
	if automaticOwnerUpdates != 0 {
		t.Fatalf("automatic PostgreSQL reconciliation ran owner hooks: %d", automaticOwnerUpdates)
	}
	activeVersionsAfter, err := application.Local().Versions(ctx, "posts", active.ID, ridu.FindOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	trashVersionsAfter, err := application.Local().Versions(ctx, "posts", trash.ID, ridu.FindOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(activeVersionsAfter, activeVersionsBefore) || !reflect.DeepEqual(trashVersionsAfter, trashVersionsBefore) {
		t.Fatalf("automatic PostgreSQL reconciliation rewrote version history: active %t trash %t",
			reflect.DeepEqual(activeVersionsAfter, activeVersionsBefore), reflect.DeepEqual(trashVersionsAfter, trashVersionsBefore))
	}
	if len(activeVersionsBefore) == 0 {
		t.Fatal("versioned owner has no retained snapshot")
	}
	if _, err := application.Local().Restore(ctx, "posts", active.ID, activeVersionsBefore[len(activeVersionsBefore)-1].Revision, ridu.MutationOptions{Actor: actor, ExpectedRevision: activeAfter.Revision}); !hasRelationshipIssue(err, "owner") {
		t.Fatalf("restore admitted a historical dangling reference: %v", err)
	}
	afterRejectedRestore, err := application.Local().Find(ctx, "posts", active.ID, ridu.FindOptions{Actor: actor})
	if err != nil || afterRejectedRestore.Revision != activeAfter.Revision || afterRejectedRestore.Values["owner"].Kind() != store.ValueNull {
		t.Fatalf("rejected restore changed current owner: %#v, %v", afterRejectedRestore, err)
	}
	if _, err := application.Local().Find(ctx, "users", target.ID, ridu.FindOptions{Actor: actor}); !hasOperationCode(err, "not_found") {
		t.Fatalf("hard-deleted target remains visible: %v", err)
	}
}

func TestPostgresConcurrentReferenceAdmissionAndTargetDeleteCannotCommitDangling(t *testing.T) {
	ctx := context.Background()
	admitted := make(chan struct{})
	release := make(chan struct{})
	config := ridu.Config{Name: "Concurrent reference delete", Collections: []ridu.Collection{
		{Slug: "targets", Fields: field.Fields{field.Text("name")}},
		{
			Slug: "entries", Fields: field.Fields{field.Relationship("target", "targets")},
			Hooks: ridu.CollectionHooks{AfterOperation: []ridu.Hook{func(hook ridu.HookContext) error {
				if hook.Operation != operation.Create {
					return nil
				}
				close(admitted)
				select {
				case <-release:
					return nil
				case <-hook.Context.Done():
					return hook.Context.Err()
				}
			}}},
		},
	}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(ctx, "targets", store.Values{"name": store.String("Target")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	type createOutcome struct {
		document store.Document
		err      error
	}
	createResult := make(chan createOutcome, 1)
	go func() {
		document, createError := application.Local().Create(ctx, "entries", store.Values{"target": store.String(target.ID)}, ridu.MutationOptions{})
		createResult <- createOutcome{document: document, err: createError}
	}()
	select {
	case <-admitted:
	case <-time.After(5 * time.Second):
		t.Fatal("reference admission did not reach the post-write transaction boundary")
	}
	deleteResult := make(chan error, 1)
	go func() {
		_, deleteError := application.Local().Delete(ctx, "targets", target.ID, ridu.MutationOptions{})
		deleteResult <- deleteError
	}()
	select {
	case err := <-deleteResult:
		close(release)
		t.Fatalf("target delete completed before reference admission committed: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	var owner store.Document
	select {
	case result := <-createResult:
		if result.err != nil {
			t.Fatal(result.err)
		}
		owner = result.document
	case <-time.After(3 * time.Second):
		t.Fatal("reference admission did not commit")
	}
	select {
	case err := <-deleteResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("target delete did not resume after reference admission committed")
	}
	reconciled, err := application.Local().Find(ctx, "entries", owner.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Values["target"].Kind() != store.ValueNull {
		t.Fatalf("concurrent reference survived target deletion: %#v", reconciled.Values["target"])
	}
	if _, err := application.Local().Find(ctx, "targets", target.ID, ridu.FindOptions{}); !hasOperationCode(err, "not_found") {
		t.Fatalf("concurrent target delete did not commit: %v", err)
	}
}

func TestPostgresRetainedReferenceUpdateAndTargetDeleteResolveWithoutDangling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ownerLocked := make(chan struct{}, 1)
	deleteLocked := make(chan struct{}, 1)
	releaseUpdate := make(chan struct{})
	config := ridu.Config{Name: "Concurrent retained reference delete", Collections: []ridu.Collection{
		{
			Slug: "targets", Fields: field.Fields{field.Text("name")},
			Hooks: ridu.CollectionHooks{BeforeDelete: []ridu.Hook{func(hook ridu.HookContext) error {
				deleteLocked <- struct{}{}
				return nil
			}}},
		},
		{
			Slug: "entries", Fields: field.Fields{field.Text("label"), field.Relationship("target", "targets")},
			Hooks: ridu.CollectionHooks{BeforeOperation: []ridu.Hook{func(hook ridu.HookContext) error {
				label, _ := hook.Data["label"].StringValue()
				if hook.Operation != operation.Update || label != "Updated" {
					return nil
				}
				ownerLocked <- struct{}{}
				select {
				case <-releaseUpdate:
					return nil
				case <-hook.Context.Done():
					return hook.Context.Err()
				}
			}}},
		},
	}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	target, err := application.Local().Create(ctx, "targets", store.Values{"name": store.String("Target")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := application.Local().Create(ctx, "entries", store.Values{
		"label": store.String("Initial"), "target": store.String(target.ID),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	updateResult := make(chan error, 1)
	go func() {
		_, updateError := application.Local().Update(ctx, "entries", owner.ID, store.Values{"label": store.String("Updated")}, ridu.MutationOptions{})
		updateResult <- updateError
	}()
	select {
	case <-ownerLocked:
	case <-ctx.Done():
		t.Fatal("retained-reference update did not lock its owner")
	}
	deleteResult := make(chan error, 1)
	go func() {
		_, deleteError := application.Local().Delete(ctx, "targets", target.ID, ridu.MutationOptions{})
		deleteResult <- deleteError
	}()
	select {
	case <-deleteLocked:
	case <-ctx.Done():
		t.Fatal("target delete did not lock its target")
	}
	// Give the delete transaction time to reach the already-locked owner. The
	// retained-reference update will then request the target lock, forming the
	// real owner→target / target→owner cycle this regression protects.
	select {
	case <-time.After(200 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(releaseUpdate)
	updateError, deleteError := <-updateResult, <-deleteResult
	conflicts := 0
	for label, operationError := range map[string]error{"update": updateError, "delete": deleteError} {
		if operationError == nil {
			continue
		}
		if !hasOperationCode(operationError, "conflict") {
			t.Fatalf("%s lock-cycle outcome = %v", label, operationError)
		}
		conflicts++
	}
	if conflicts != 1 {
		t.Fatalf("retained-reference lock cycle = update %v, delete %v; want one retryable conflict", updateError, deleteError)
	}

	currentOwner, err := application.Local().Find(ctx, "entries", owner.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if retained, valid := currentOwner.Values["target"].StringValue(); valid && retained != "" {
		if retained != target.ID {
			t.Fatalf("owner retained unexpected target %q", retained)
		}
		if _, err := application.Local().Find(ctx, "targets", target.ID, ridu.FindOptions{}); err != nil {
			t.Fatalf("lock cycle committed a dangling reference: %v", err)
		}
	}
	if deleteError != nil {
		if _, err := application.Local().Delete(ctx, "targets", target.ID, ridu.MutationOptions{}); err != nil {
			t.Fatalf("caller delete retry failed: %v", err)
		}
	} else {
		if _, err := application.Local().Update(ctx, "entries", owner.ID, store.Values{"label": store.String("Retried")}, ridu.MutationOptions{}); err != nil {
			t.Fatalf("caller update retry failed: %v", err)
		}
	}
	finalOwner, err := application.Local().Find(ctx, "entries", owner.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if finalOwner.Values["target"].Kind() != store.ValueNull {
		t.Fatalf("caller retry left a dangling reference: %#v", finalOwner.Values)
	}
}

func TestPostgresHardDeleteReconcilesLocalizedNestedUploadColumns(t *testing.T) {
	ctx := context.Background()
	storageBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := ridu.Config{
		Name: "PostgreSQL localized nested upload deletes", Storage: storageBackend, StorageNamespace: "postgres-reference-delete",
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{
			{Code: "en", Label: "English"}, {Code: "fr", Label: "French"},
		}},
		Collections: []ridu.Collection{
			{
				Slug: "media", Upload: true,
				UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}},
				Fields:       field.Fields{field.Text("label")},
			},
			{
				Slug:   "posts",
				Fields: field.Fields{field.Blocks("gallery", field.Block{Slug: "image", Fields: field.Fields{field.Upload("asset", "media"), field.Upload("guard", "media").OnDelete(field.ReferenceDeleteRestrict)}}).Localized()},
			},
		},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	englishAsset, err := application.Upload(ctx, "media", ridu.UploadInput{
		Filename: "english.txt", Reader: strings.NewReader("English"), Data: store.Values{"label": store.String("English")},
	})
	if err != nil {
		t.Fatal(err)
	}
	frenchAsset, err := application.Upload(ctx, "media", ridu.UploadInput{
		Filename: "french.txt", Reader: strings.NewReader("French"), Data: store.Values{"label": store.String("French")},
	})
	if err != nil {
		t.Fatal(err)
	}
	image := func(asset, guard string) store.Value {
		values := store.Values{"blockType": store.String("image"), "asset": store.String(asset)}
		if guard != "" {
			values["guard"] = store.String(guard)
		}
		return store.Object(values)
	}
	post, err := application.Local().Create(ctx, "posts", store.Values{
		"gallery": store.List(image(englishAsset.ID, englishAsset.ID)),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Update(ctx, "posts", post.ID, store.Values{
		"gallery": store.List(image(frenchAsset.ID, "")),
	}, ridu.MutationOptions{Locale: "fr"}); err != nil {
		t.Fatal(err)
	}

	if _, err := application.Local().Delete(ctx, "media", englishAsset.ID, ridu.MutationOptions{}); !hasOperationCode(err, "delete_restricted") {
		t.Fatalf("localized nested upload restriction = %v", err)
	}
	if _, err := application.Local().Update(ctx, "posts", post.ID, store.Values{
		"gallery": store.List(image(englishAsset.ID, "")),
	}, ridu.MutationOptions{Locale: "en"}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(ctx, "media", englishAsset.ID, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}

	reconciled, err := application.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{AllLocales: true})
	if err != nil {
		t.Fatal(err)
	}
	locales, valid := reconciled.Values["gallery"].CopyObject()
	if !valid {
		t.Fatalf("localized gallery = %#v", reconciled.Values["gallery"])
	}
	englishBlocks, _ := locales["en"].CopyList()
	english, _ := englishBlocks[0].CopyObject()
	asset, hasAsset := english["asset"]
	guard, hasGuard := english["guard"]
	if hasAsset && asset.Kind() != store.ValueNull || hasGuard && guard.Kind() != store.ValueNull {
		t.Fatalf("English nested upload values = %#v", english)
	}
	frenchBlocks, _ := locales["fr"].CopyList()
	french, _ := frenchBlocks[0].CopyObject()
	if got := stringValue(french["asset"]); got != frenchAsset.ID {
		t.Fatalf("French nested upload changed from %q to %#v", frenchAsset.ID, french["asset"])
	}
}

func TestPostgresBulkHardDeleteOnlyIgnoresOwnersAlreadyDeletedInTheTransaction(t *testing.T) {
	ctx := context.Background()
	config := ridu.Config{Name: "PostgreSQL ordered batch deletes", Collections: []ridu.Collection{{
		Slug: "nodes", Fields: field.Fields{field.Text("name"), field.Relationship("parent", "nodes").OnDelete(field.ReferenceDeleteRestrict)},
	}}}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	createPair := func(prefix string) (store.Document, store.Document) {
		t.Helper()
		parent, createError := application.Local().Create(ctx, "nodes", store.Values{"name": store.String(prefix + " parent")}, ridu.MutationOptions{})
		if createError != nil {
			t.Fatal(createError)
		}
		child, createError := application.Local().Create(ctx, "nodes", store.Values{"name": store.String(prefix + " child"), "parent": store.String(parent.ID)}, ridu.MutationOptions{})
		if createError != nil {
			t.Fatal(createError)
		}
		return parent, child
	}
	parent, child := createPair("ordered")
	if _, err := application.Local().BulkDelete(ctx, "nodes", []string{child.ID, parent.ID}, ridu.BulkOptions{}); err != nil {
		t.Fatalf("owner-before-target PostgreSQL batch failed: %v", err)
	}
	parent, child = createPair("future")
	if _, err := application.Local().BulkDelete(ctx, "nodes", []string{parent.ID, child.ID}, ridu.BulkOptions{}); !hasOperationCode(err, "delete_restricted") {
		t.Fatalf("target-before-owner PostgreSQL batch bypassed restriction: %v", err)
	}
	for _, document := range []store.Document{parent, child} {
		if _, err := application.Local().Find(ctx, "nodes", document.ID, ridu.FindOptions{}); err != nil {
			t.Errorf("failed PostgreSQL batch did not roll back %q: %v", document.ID, err)
		}
	}
}
