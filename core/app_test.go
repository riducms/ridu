package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestAppRejectsCrossKindStableResourceIdentityCollision(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Colliding resource identities",
		Collections: []ridu.Collection{{
			Slug: "global-site-settings", Fields: field.Fields{field.Text("title")},
		}},
		Globals: []ridu.Global{{
			Slug: "site-settings", Fields: field.Fields{field.Text("title")},
		}},
	}, teststore.New())
	if application != nil {
		t.Fatal("application was returned for colliding collection/global stable IDs")
	}
	var validation *schema.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("New error = %T %v, want *schema.ValidationError", err, err)
	}
	for _, issue := range validation.Issues {
		if issue.Code == "duplicate_resource_id" && issue.Path == "globals[0].slug" && strings.Contains(issue.Message, `collections[0].slug`) {
			return
		}
	}
	t.Fatalf("New issues = %#v, want duplicate_resource_id at globals[0].slug", validation.Issues)
}

func TestLocalCRUDUsesValidationAccessAndOnePipeline(t *testing.T) {
	backend := teststore.New()
	title, err := query.NewPath("title")
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(ridu.Config{
		Name: "Operations",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title").Required()},
			Access: ridu.CollectionAccess{
				Create: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
				Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					return ridu.Where(query.Equal(title, query.String("public"))), nil
				},
				Update: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
				Delete: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
			},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := application.Local().Create(context.Background(), "posts", store.Values{}, nil); err == nil {
		t.Fatal("create without title succeeded")
	}
	if got := backend.Events(); !slices.Equal(got, []string{"begin", "rollback"}) {
		t.Fatalf("validation failure transaction events = %v", got)
	}
	private, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("private")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	public, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("public")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Find(context.Background(), "posts", private.ID, nil); !operationCode(err, "not_found") {
		t.Fatalf("filtered find error = %v, want not_found", err)
	}
	page, err := application.Local().List(context.Background(), "posts", ridu.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Documents) != 1 || page.Documents[0].ID != public.ID {
		t.Fatalf("access-filtered page = %#v", page.Documents)
	}
	updated, err := application.Local().Update(context.Background(), "posts", private.ID, store.Values{"title": store.String("public")}, nil)
	if err != nil || updated.ID != private.ID {
		t.Fatalf("update = %#v, %v", updated, err)
	}
	deleted, err := application.Local().Delete(context.Background(), "posts", public.ID, nil)
	if err != nil || deleted.ID != public.ID {
		t.Fatalf("delete = %#v, %v", deleted, err)
	}
}

func TestLocalReadPreservesMetadataOnlyRootAndPopulationSelections(t *testing.T) {
	authorPath, _ := query.NewPath("author")
	statusPath, _ := query.NewPath("_status")
	revisionPath, _ := query.NewPath("_revision")
	deletedAtPath, _ := query.NewPath("deletedAt")
	application, err := ridu.New(ridu.Config{
		Name: "Projection presence",
		Collections: []ridu.Collection{
			{Slug: "authors", Versions: true, Trash: true, Fields: field.Fields{field.Text("name"), field.Text("bio")}},
			{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Relationship("author", "authors")}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	author, err := application.Local().Create(t.Context(), "authors", store.Values{"name": store.String("Ada"), "bio": store.String("Writer")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	post, err := application.Local().Create(t.Context(), "posts", store.Values{"title": store.String("Projection"), "author": store.String(author.ID)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadataOnly, err := application.Local().FindWithOptions(t.Context(), "posts", post.ID, ridu.FindOptions{Select: []query.Path{}})
	if err != nil || len(metadataOnly.Values) != 0 {
		t.Fatalf("metadata-only root = %#v, %v", metadataOnly.Values, err)
	}
	populated, err := application.Local().FindWithOptions(t.Context(), "posts", post.ID, ridu.FindOptions{
		Select:   []query.Path{authorPath},
		Populate: []query.Population{{Path: authorPath, Depth: 1, Select: []query.Path{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	projectedAuthor, ok := populated.Values["author"].CopyDocument()
	if !ok || projectedAuthor.ID != author.ID || len(projectedAuthor.Values) != 0 {
		t.Fatalf("metadata-only populated author = %#v, %t", projectedAuthor, ok)
	}
	generatedMetadata, err := application.Local().FindWithOptions(t.Context(), "posts", post.ID, ridu.FindOptions{
		Select: []query.Path{authorPath},
		Populate: []query.Population{{
			Path: authorPath, Depth: 1,
			Select: []query.Path{statusPath, revisionPath, deletedAtPath},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	metadataAuthor, ok := generatedMetadata.Values["author"].CopyDocument()
	if !ok || metadataAuthor.Status != store.StatusPublished || metadataAuthor.Revision < 1 || len(metadataAuthor.Values) != 0 {
		t.Fatalf("generated metadata populated author = %#v, %t", metadataAuthor, ok)
	}
}

func TestTrashLifecycleHidesRestoresAndPermanentlyDeletes(t *testing.T) {
	backend := teststore.New()
	var operations []operation.Kind
	application, err := ridu.New(ridu.Config{Name: "Trash", Collections: []ridu.Collection{{
		Slug: "posts", Trash: true, Fields: field.Fields{field.Text("title").Required()},
		Hooks: ridu.CollectionHooks{AfterOperation: []ridu.Hook{func(ctx ridu.HookContext) error {
			operations = append(operations, ctx.Operation)
			return nil
		}}},
	}}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Recoverable")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := application.Local().Delete(context.Background(), "posts", document.ID, nil)
	if err != nil || deleted.DeletedAt == nil {
		t.Fatalf("trash = %#v, %v", deleted, err)
	}
	if _, err := application.Local().Find(context.Background(), "posts", document.ID, nil); !operationCode(err, "not_found") {
		t.Fatalf("ordinary find after trash = %v", err)
	}
	trash, err := application.Local().List(context.Background(), "posts", ridu.ListOptions{TrashOnly: true})
	if err != nil || trash.Total != 1 || trash.Documents[0].ID != document.ID {
		t.Fatalf("trash list = %#v, %v", trash, err)
	}
	restored, err := application.Local().RestoreDeleted(context.Background(), "posts", document.ID, nil)
	if err != nil || restored.DeletedAt != nil {
		t.Fatalf("restore = %#v, %v", restored, err)
	}
	if _, err := application.Local().Delete(context.Background(), "posts", document.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().DeletePermanent(context.Background(), "posts", document.ID, nil); err != nil {
		t.Fatal(err)
	}
	trash, err = application.Local().List(context.Background(), "posts", ridu.ListOptions{TrashOnly: true})
	if err != nil || trash.Total != 0 {
		t.Fatalf("trash after permanent delete = %#v, %v", trash, err)
	}
	want := []operation.Kind{operation.Create, operation.Delete, operation.Read, operation.RestoreDeleted, operation.Delete, operation.DeletePermanent, operation.Read}
	if !slices.Equal(operations, want) {
		t.Fatalf("operations = %v, want %v", operations, want)
	}
}

func TestTrashCapabilitiesMatchDeleteAccessWhenReadIsDenied(t *testing.T) {
	denyRead := false
	application, err := ridu.New(ridu.Config{Name: "Unreadable trash capabilities", Collections: []ridu.Collection{{
		Slug: "posts", Trash: true, Fields: field.Fields{field.Text("title").Required()},
		Access: ridu.CollectionAccess{
			Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				if denyRead {
					return ridu.Deny(), nil
				}
				return ridu.Allow(), nil
			},
			Delete: func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Allow(), nil },
		},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Recoverable")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Delete(context.Background(), "posts", document.ID, nil); err != nil {
		t.Fatal(err)
	}
	denyRead = true
	capabilities, err := application.Local().Capabilities(context.Background(), "posts", document.ID, ridu.CapabilityOptions{TrashOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !capabilities.Operations.RestoreDeleted || !capabilities.Operations.DeletePermanent || capabilities.Operations.Read {
		t.Fatalf("trash capabilities = %#v", capabilities.Operations)
	}
	if _, err := application.Local().RestoreDeleted(context.Background(), "posts", document.ID, nil); err != nil {
		t.Fatalf("restore allowed by capability failed: %v", err)
	}
	if _, err := application.Local().Delete(context.Background(), "posts", document.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().DeletePermanent(context.Background(), "posts", document.ID, nil); err != nil {
		t.Fatalf("permanent delete allowed by capability failed: %v", err)
	}
}

func TestBulkTrashRestoreAndPermanentDeleteAreAtomic(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Bulk trash", Collections: []ridu.Collection{{
		Slug: "posts", Trash: true, Fields: field.Fields{field.Text("title").Required()},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("First")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Second")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{first.ID, second.ID}
	if deleted, err := application.Local().BulkDelete(context.Background(), "posts", ids, nil); err != nil || len(deleted) != 2 {
		t.Fatalf("bulk trash = %#v, %v", deleted, err)
	}
	if restored, err := application.Local().BulkRestoreDeleted(context.Background(), "posts", ids, nil); err != nil || len(restored) != 2 {
		t.Fatalf("bulk restore = %#v, %v", restored, err)
	}
	if _, err := application.Local().BulkDelete(context.Background(), "posts", ids, nil); err != nil {
		t.Fatal(err)
	}
	if deleted, err := application.Local().BulkDeletePermanent(context.Background(), "posts", ids, nil); err != nil || len(deleted) != 2 {
		t.Fatalf("bulk permanent delete = %#v, %v", deleted, err)
	}
	third, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Third")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fourth, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Fourth")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().BulkDelete(context.Background(), "posts", []string{third.ID, fourth.ID}, nil); err != nil {
		t.Fatal(err)
	}
	if emptied, err := application.Local().EmptyTrash(context.Background(), "posts", nil); err != nil || len(emptied) != 2 {
		t.Fatalf("empty trash = %#v, %v", emptied, err)
	}
	trash, err := application.Local().List(context.Background(), "posts", ridu.ListOptions{TrashOnly: true})
	if err != nil || trash.Total != 0 {
		t.Fatalf("trash after bulk permanent delete = %#v, %v", trash, err)
	}
}

func TestDuplicateHooksAndAtomicBulkRollback(t *testing.T) {
	backend := teststore.New()
	titlePath, _ := query.NewPath("title")
	application, err := ridu.New(ridu.Config{Name: "Bulk and duplicate", Collections: []ridu.Collection{{
		Slug:   "posts",
		Fields: field.Fields{field.Text("title").Required(), field.Text("slug").Unique()},
		Access: ridu.CollectionAccess{
			Create: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				if _, hasCopiedTitle := ctx.Data["title"]; !hasCopiedTitle {
					return ridu.Deny(), nil
				}
				return ridu.Allow(), nil
			},
			Update: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				return ridu.Where(query.Equal(titlePath, query.String("First"))), nil
			},
		},
		Hooks: ridu.CollectionHooks{BeforeValidate: []ridu.Hook{func(ctx ridu.HookContext) error {
			if ctx.Operation == operation.Duplicate {
				title, _ := ctx.Data["title"].StringValue()
				ctx.Data["title"] = store.String("Copy of " + title)
				delete(ctx.Data, "slug")
			}
			return nil
		}}},
	}}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	first, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("First"), "slug": store.String("first")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Second"), "slug": store.String("second")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := application.Local().Duplicate(context.Background(), "posts", first.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	duplicateTitle, _ := duplicate.Values["title"].StringValue()
	if duplicate.ID == first.ID || duplicateTitle != "Copy of First" {
		t.Fatalf("duplicate = %#v", duplicate)
	}
	if _, exists := duplicate.Values["slug"]; exists {
		t.Fatalf("duplicate retained unique slug: %#v", duplicate.Values)
	}
	if _, err := application.Local().BulkUpdate(context.Background(), "posts", []string{first.ID, second.ID}, store.Values{"title": store.String("Changed")}, nil); !operationCode(err, "not_found") {
		t.Fatalf("bulk update error = %v, want not_found", err)
	}
	unchanged, err := application.Local().Find(context.Background(), "posts", first.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	unchangedTitle, _ := unchanged.Values["title"].StringValue()
	if unchangedTitle != "First" {
		t.Fatalf("first document survived partial bulk commit with title %q", unchangedTitle)
	}
	if _, err := application.Local().BulkDelete(context.Background(), "posts", []string{first.ID, first.ID}, nil); !operationCode(err, "bad_request") {
		t.Fatalf("duplicate bulk targets error = %v, want bad_request", err)
	}
}

func TestDuplicateCannotLaunderSourceHiddenFieldsThroughNewOwnership(t *testing.T) {
	application, err := ridu.New(ridu.Config{Name: "Duplicate source field access", Collections: []ridu.Collection{{
		Slug: "posts",
		Fields: field.Fields{field.Text("owner").Required(), field.Text("secret").Access(field.Access{Read: func(ctx operation.AccessContext,

		) (bool, error) {
			owner, _ := ctx.Root.Get("owner").
				StringValue()
			return ctx.Actor.ID != "" &&
					owner ==
						string(ctx.Actor.ID),

				nil
		}})},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ownerA := &store.Document{ID: "owner-a"}
	ownerB := &store.Document{ID: "owner-b"}
	source, err := application.Local().Create(context.Background(), "posts", store.Values{
		"owner": store.String(ownerA.ID), "secret": store.String("source-only"),
	}, ownerA)
	if err != nil {
		t.Fatal(err)
	}
	hiddenSource, err := application.Local().Find(context.Background(), "posts", source.ID, ownerB)
	if err != nil {
		t.Fatal(err)
	}
	if _, exposed := hiddenSource.Values["secret"]; exposed {
		t.Fatalf("source secret was readable before duplicate: %#v", hiddenSource.Values)
	}
	duplicate, err := application.Local().Duplicate(context.Background(), "posts", source.ID, store.Values{
		"owner": store.String(ownerB.ID),
	}, ownerB)
	if err != nil {
		t.Fatal(err)
	}
	if _, copied := duplicate.Values["secret"]; copied {
		t.Fatalf("duplicate laundered source-hidden secret: %#v", duplicate.Values)
	}
	persisted, err := application.Local().Find(context.Background(), "posts", duplicate.ID, ownerB)
	if err != nil {
		t.Fatal(err)
	}
	if _, copied := persisted.Values["secret"]; copied {
		t.Fatalf("persisted duplicate retained source-hidden secret: %#v", persisted.Values)
	}
}

func TestFullDocumentGlobalAndErrorHookMatrix(t *testing.T) {
	backend := teststore.New()
	seen := make(map[string][]operation.Kind)
	record := func(name string) ridu.Hook {
		return func(ctx ridu.HookContext) error {
			seen[name] = append(seen[name], ctx.Operation)
			if name == "afterRead" && ctx.Document != nil {
				ctx.Document.Values["readMarker"] = store.String("hooked")
			}
			if name == "afterError" || name == "rootAfterError" {
				if ctx.Error == nil {
					t.Fatalf("%s hook received no error", name)
				}
			}
			return nil
		}
	}
	hooks := ridu.CollectionHooks{
		BeforeDuplicate: []ridu.Hook{record("beforeDuplicate")},
		BeforeValidate:  []ridu.Hook{record("beforeValidate")},
		BeforeChange:    []ridu.Hook{record("beforeChange")},
		BeforeOperation: []ridu.Hook{record("beforeOperation")},
		BeforeRead:      []ridu.Hook{record("beforeRead")},
		BeforeDelete:    []ridu.Hook{record("beforeDelete")},
		AfterChange:     []ridu.Hook{record("afterChange")},
		AfterRead:       []ridu.Hook{record("afterRead")},
		AfterDelete:     []ridu.Hook{record("afterDelete")},
		AfterOperation:  []ridu.Hook{record("afterOperation")},
		AfterError:      []ridu.Hook{record("afterError")},
	}
	globalHooks := hooks
	globalHooks.BeforeDuplicate = nil
	globalHooks.BeforeDelete = nil
	globalHooks.AfterDelete = nil
	application, err := ridu.New(ridu.Config{
		Name:  "Hook matrix",
		Hooks: ridu.RootHooks{AfterError: []ridu.Hook{record("rootAfterError")}},
		Collections: []ridu.Collection{{
			Slug: "posts", Fields: field.Fields{field.Text("title").Required().Hooks(field.Hooks[string]{
				BeforeDuplicate: []field.RawTransform{func(ctx operation.WriteContext, _ operation.Value[store.Value]) (operation.Change[store.Value], error) {
					seen["fieldBeforeDuplicate"] = append(seen["fieldBeforeDuplicate"], ctx.Operation)
					return operation.Keep[store.Value](), nil
				}},
				BeforeChange: []field.Transform[string]{func(ctx operation.WriteContext, _ operation.Value[string]) (operation.Change[string], error) {
					seen["fieldBeforeChange"] = append(seen["fieldBeforeChange"], ctx.Operation)
					return operation.Keep[string](), nil
				}},
				AfterChange: []field.Observer[string]{func(ctx operation.EventContext, _ operation.Value[string]) error {
					seen["fieldAfterChange"] = append(seen["fieldAfterChange"], ctx.Operation)
					return nil
				}},
				BeforeDelete: []field.Observer[string]{func(ctx operation.EventContext, _ operation.Value[string]) error {
					seen["fieldBeforeDelete"] = append(seen["fieldBeforeDelete"], ctx.Operation)
					return nil
				}},
				AfterDelete: []field.Observer[string]{func(ctx operation.EventContext, _ operation.Value[string]) error {
					seen["fieldAfterDelete"] = append(seen["fieldAfterDelete"], ctx.Operation)
					return nil
				}},
			}).ReadHooks(field.ReadHooks[string]{AfterRead: []field.OutputTransform[string]{func(ctx operation.ReadContext, _ operation.Value[string]) (operation.Change[string], error) {
				seen["fieldAfterRead"] = append(seen["fieldAfterRead"], ctx.Operation)
				return operation.Keep[string](), nil
			}}})}, Hooks: hooks,
		}},
		Globals: []ridu.Global{{Slug: "settings", Fields: field.Fields{field.Text("title").Required()}, Hooks: globalHooks}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("First")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Duplicate(context.Background(), "posts", created.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	read, err := application.Local().Find(context.Background(), "posts", created.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if marker, _ := read.Values["readMarker"].StringValue(); marker != "hooked" {
		t.Fatalf("afterRead response marker = %q", marker)
	}
	if _, err := application.Local().Delete(context.Background(), "posts", created.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(context.Background(), "posts", store.Values{}, nil); !operationCode(err, "validation") {
		t.Fatalf("invalid create error = %v", err)
	}
	if _, err := application.Local().UpdateGlobal(context.Background(), "settings", store.Values{"title": store.String("Site")}, 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Global(context.Background(), "settings", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Find(context.Background(), "missing", "id", nil); !operationCode(err, "unknown_collection") {
		t.Fatalf("unknown collection error = %v", err)
	}

	wantOperations := map[string][]operation.Kind{
		"beforeDuplicate":      {operation.Duplicate},
		"fieldBeforeDuplicate": {operation.Duplicate},
		"beforeRead":           {operation.Read, operation.Read},
		"beforeDelete":         {operation.Delete},
		"fieldBeforeDelete":    {operation.Delete},
		"afterDelete":          {operation.Delete},
		"fieldAfterDelete":     {operation.Delete},
		"afterRead":            {operation.Create, operation.Duplicate, operation.Read, operation.Delete, operation.Update, operation.Read},
		"fieldAfterRead":       {operation.Create, operation.Duplicate, operation.Read, operation.Delete},
		"afterError":           {operation.Create},
		"rootAfterError":       {operation.Create, operation.Read},
	}
	for name, want := range wantOperations {
		if !slices.Equal(seen[name], want) {
			t.Errorf("%s operations = %v, want %v", name, seen[name], want)
		}
	}
	for _, name := range []string{"beforeChange", "afterChange", "afterRead", "fieldBeforeChange", "fieldAfterChange", "fieldAfterRead"} {
		if len(seen[name]) == 0 {
			t.Errorf("%s hook did not run", name)
		}
	}
}

func TestOmittedOptionalFieldHookCanSupplyAValue(t *testing.T) {
	afterCommit := 0
	application, err := ridu.New(ridu.Config{Name: "Optional field hook", Collections: []ridu.Collection{{
		Slug: "posts", Fields: field.Fields{field.Text("optional").Hooks(field.Hooks[string]{BeforeValidate: []field.RawTransform{func(_ operation.WriteContext, input operation.Value[store.Value]) (operation.Change[store.Value], error) {
			if _, supplied := input.Get(); !supplied {
				return operation.Replace(operation.Present(store.String("hook default"))), nil
			}
			return operation.Keep[store.Value](), nil
		}}, AfterCommit: []field.Observer[string]{func(operation.EventContext, operation.Value[string]) error { afterCommit++; return nil }}})},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(context.Background(), "posts", store.Values{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(document.Values["optional"]) != "hook default" || afterCommit != 1 {
		t.Fatalf("optional field hook result = %#v, afterCommit=%d", document.Values, afterCommit)
	}
}

func TestNestedLocalOperationReusesTransactionAndDefersAfterCommit(t *testing.T) {
	backend := teststore.New()
	dispatcher := &recordingDispatcher{}
	var lifecycle []string
	application, err := ridu.New(ridu.Config{
		Name:        "Nested operations",
		AfterCommit: dispatcher,
		Collections: []ridu.Collection{
			{
				Slug:   "audits",
				Fields: field.Fields{field.Text("message").Required()},
				Hooks: ridu.CollectionHooks{AfterCommit: []ridu.Hook{func(ridu.HookContext) error {
					lifecycle = append(lifecycle, "audit committed")
					return nil
				}}},
			},
			{
				Slug:   "posts",
				Fields: field.Fields{field.Text("title").Required()},
				Hooks: ridu.CollectionHooks{
					AfterOperation: []ridu.Hook{func(ctx ridu.HookContext) error {
						lifecycle = append(lifecycle, "post stored")
						_, err := ctx.Local.Create(ctx.Context, "audits", store.Values{"message": store.String("created")}, nil)
						return err
					}},
					AfterCommit: []ridu.Hook{func(ridu.HookContext) error {
						lifecycle = append(lifecycle, "post committed")
						return nil
					}},
				},
			},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("nested")}, nil); err != nil {
		t.Fatal(err)
	}
	if got := backend.Events(); count(got, "begin") != 1 || count(got, "commit") != 1 {
		t.Fatalf("transaction events = %v, want one begin and commit", got)
	}
	want := []string{"post stored", "audit committed", "post committed"}
	if !slices.Equal(lifecycle, want) {
		t.Fatalf("lifecycle = %v, want %v", lifecycle, want)
	}
	if len(dispatcher.effects) != 2 || dispatcher.effects[0].CollectionID != "audits" || dispatcher.effects[1].CollectionID != "posts" {
		t.Fatalf("after-commit dispatcher effects = %#v", dispatcher.effects)
	}
	page, err := application.Local().List(context.Background(), "audits", ridu.ListOptions{})
	if err != nil || page.Total != 1 {
		t.Fatalf("audit page = %#v, %v", page, err)
	}
}

type recordedEffect struct {
	CollectionID string
	DocumentID   string
}

type recordingDispatcher struct{ effects []recordedEffect }

func (dispatcher *recordingDispatcher) Dispatch(ctx context.Context, effect ridu.AfterCommitEffect) error {
	dispatcher.effects = append(dispatcher.effects, recordedEffect{CollectionID: string(effect.CollectionID), DocumentID: effect.DocumentID})
	return effect.Run(ctx)
}

func TestHookFailureAndCancellationRollbackInOrder(t *testing.T) {
	backend := teststore.New()
	application, err := ridu.New(ridu.Config{
		Name: "Rollback",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title").Required()},
			Hooks: ridu.CollectionHooks{AfterOperation: []ridu.Hook{func(ridu.HookContext) error {
				return errors.New("stop")
			}}},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("rollback")}, nil); !operationCode(err, "hook_failed") {
		t.Fatalf("hook failure = %v", err)
	}
	events := backend.Events()
	if !slices.Equal(events, []string{"begin", "create", "rollback"}) {
		t.Fatalf("rollback events = %v", events)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := application.Local().List(canceled, "posts", ridu.ListOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled list error = %v", err)
	}
	if !slices.Equal(backend.Events(), events) {
		t.Fatal("canceled request touched the store")
	}
}

func TestRecursiveFieldVocabularyValidatesAndRoundTrips(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Recursive fields",
		Collections: []ridu.Collection{{
			Slug:   "questions",
			Fields: field.Fields{field.Email("owner").Required().Admin(field.Admin{Description: "Editorial contact"}), field.Number("score").Required().Admin(field.Admin{Columns: 6, Tab: "Scoring"}), field.Checkbox("published").Required().Admin(field.Admin{Columns: 6, Tab: "Scoring"}), field.Date("due").Format(field.DateTime).Admin(field.Admin{VisibleWhen: field.Equal(field.Sibling("published"), true), Tab: "Scoring"}), field.JSON("metadata"), field.Array("answers", field.Fields{field.Textarea("copy").Required()}), field.Blocks("content", field.Block{Slug: "heading", Fields: field.Fields{field.Text("text").Required()}}, field.Block{Slug: "notice", Fields: field.Fields{field.Textarea("body").Required()}})},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	resolvedFields := application.Manifest().Snapshot().Collections[0].Fields
	if resolvedFields[1].Admin.Columns != 6 || resolvedFields[1].Admin.Tab != "Scoring" || resolvedFields[3].Admin.Condition == nil || resolvedFields[3].Date == nil || resolvedFields[3].Date.Format != schema.DateTime {
		t.Fatalf("admin row/tab/condition metadata = %#v, %#v", resolvedFields[1].Admin, resolvedFields[3].Admin)
	}
	created, err := application.Local().Create(context.Background(), "questions", store.Values{
		"owner": store.String("ada@example.test"), "score": store.Number(4.5), "published": store.Boolean(true),
		"due": store.String("2026-08-05T09:30:00Z"), "metadata": store.Object(store.Values{"source": store.String("fixture")}),
		"answers": store.List(store.Object(store.Values{"_key": store.String("row-1"), "copy": store.String("Yes")})),
		"content": store.List(store.Object(store.Values{"_key": store.String("block-1"), "blockType": store.String("heading"), "text": store.String("Hello")})),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	answers, valid := created.Values["answers"].CopyList()
	if !valid || len(answers) != 1 {
		t.Fatalf("answers = %#v", created.Values["answers"])
	}
	answer, _ := answers[0].CopyObject()
	if key, _ := answer["_key"].StringValue(); key != "row-1" {
		t.Fatalf("stable row key = %q", key)
	}
	if _, err := application.Local().Create(context.Background(), "questions", store.Values{
		"owner": store.String("not-an-email"), "score": store.String("four"), "published": store.Boolean(true),
	}, nil); !operationCode(err, "validation") {
		t.Fatalf("invalid recursive values error = %v", err)
	}
}

func TestHooksReceiveDetachedAuthenticatedActor(t *testing.T) {
	var received *store.Document
	application, err := ridu.New(ridu.Config{
		Name: "Hook actor",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title").Required()},
			Hooks: ridu.CollectionHooks{BeforeValidate: []ridu.Hook{func(ctx ridu.HookContext) error {
				received = ctx.Actor
				ctx.Actor.Values["role"] = store.String("mutated")
				return nil
			}}},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	actor := store.Document{ID: "actor-1", Values: store.Values{"role": store.String("editor")}}
	if _, err := application.Local().Create(context.Background(), "posts", store.Values{"title": store.String("Hello")}, &actor); err != nil {
		t.Fatal(err)
	}
	if received == nil || received.ID != actor.ID {
		t.Fatalf("hook actor = %#v", received)
	}
	role, _ := actor.Values["role"].StringValue()
	if role != "editor" {
		t.Fatalf("hook mutated caller actor = %q", role)
	}
}

func TestHasManyPolymorphicPopulationHonorsTargetAccessAndRedaction(t *testing.T) {
	publicPath, _ := query.NewPath("public")
	application, err := ridu.New(ridu.Config{Name: "Relationship shapes", Collections: []ridu.Collection{
		{Slug: "people", Fields: field.Fields{field.Text("name").Required(), field.Checkbox("public").Required(), field.Text("secret").Access(field.Access{Read: func(operation.AccessContext,

		) (bool, error) {
			return false, nil
		}})}, Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
			return ridu.Where(query.Equal(publicPath, query.Boolean(true))), nil
		}}},
		{Slug: "teams", Fields: field.Fields{field.Text("name").Required(), field.Relationship("owner", "people")}},
		{Slug: "feeds", Fields: field.Fields{field.Relationships("watchers", "people"), field.PolymorphicRelationship("subject", "people", "teams")}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	visible, _ := application.Local().Create(context.Background(), "people", store.Values{"name": store.String("Visible"), "public": store.Boolean(true), "secret": store.String("redact")}, nil)
	private, _ := application.Local().Create(context.Background(), "people", store.Values{"name": store.String("Private"), "public": store.Boolean(false)}, nil)
	team, _ := application.Local().Create(context.Background(), "teams", store.Values{"name": store.String("Core"), "owner": store.String(visible.ID)}, nil)
	if _, err := application.Local().Create(context.Background(), "feeds", store.Values{
		"watchers": store.List(store.String(visible.ID), store.String(private.ID)),
		"subject":  store.Object(store.Values{"relationTo": store.String("teams"), "id": store.String(team.ID)}),
	}, nil); !relationshipIssue(err, "watchers.1") {
		t.Fatalf("inaccessible relationship error = %v", err)
	}
	feed, err := application.Local().Create(context.Background(), "feeds", store.Values{
		"watchers": store.List(store.String(visible.ID)),
		"subject":  store.Object(store.Values{"relationTo": store.String("teams"), "id": store.String(team.ID)}),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	watchersPath, _ := query.NewPath("watchers")
	subjectPath, _ := query.NewPath("subject")
	page, err := application.Local().List(context.Background(), "feeds", ridu.ListOptions{Populate: []query.Population{{Path: watchersPath}, {Path: subjectPath, Depth: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	watchers, _ := page.Documents[0].Values["watchers"].CopyList()
	person, populated := watchers[0].CopyDocument()
	if !populated || person.ID != visible.ID {
		t.Fatalf("has-many population = %#v", watchers)
	}
	if _, leaked := person.Values["secret"]; leaked {
		t.Fatal("populated field access leaked secret")
	}
	subject, _ := page.Documents[0].Values["subject"].CopyObject()
	teamDocument, populated := subject["id"].CopyDocument()
	if !populated || teamDocument.ID != team.ID || page.Documents[0].ID != feed.ID {
		t.Fatalf("polymorphic population = %#v", subject)
	}
	owner, populated := teamDocument.Values["owner"].CopyDocument()
	if !populated || owner.ID != visible.ID {
		t.Fatalf("depth-2 population = %#v", teamDocument.Values["owner"])
	}
}

func TestRelationshipWritesRejectMissingFilteredAndNestedTargets(t *testing.T) {
	visiblePath, _ := query.NewPath("visible")
	var checkedIDs []string
	var deniedID string
	application, err := ridu.New(ridu.Config{Name: "Relationship integrity", Collections: []ridu.Collection{
		{Slug: "people", Fields: field.Fields{field.Text("name").Required(), field.Checkbox("visible").Required()}, Access: ridu.CollectionAccess{Read: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
			if ctx.ID == "" || ctx.Local == nil || len(ctx.Data) != 0 {
				return ridu.Deny(), errors.New("relationship access context is incomplete")
			}
			checkedIDs = append(checkedIDs, ctx.ID)
			if ctx.ID == deniedID {
				return ridu.Deny(), nil
			}
			return ridu.Where(query.Equal(visiblePath, query.Boolean(true))), nil
		}}},
		{Slug: "teams", Fields: field.Fields{field.Text("name").Required()}},
		{Slug: "entries", Fields: field.Fields{field.Relationship("owner", "people"), field.Relationships("watchers", "people"), field.PolymorphicRelationship("subject", "people", "teams"), field.Group("meta", field.Fields{field.Relationship("reviewer", "people")}), field.Array("sections", field.Fields{field.Relationship("editor", "people")}), field.Blocks("content", field.Block{Slug: "quote", Fields: field.Fields{field.Relationship("source", "people")}})}},
	}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	denied, err := application.Local().Create(context.Background(), "people", store.Values{
		"name": store.String("Denied"), "visible": store.Boolean(true),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	deniedID = denied.ID
	visible, err := application.Local().Create(context.Background(), "people", store.Values{
		"name": store.String("Visible"), "visible": store.Boolean(true),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := application.Local().Create(context.Background(), "people", store.Values{
		"name": store.String("Hidden"), "visible": store.Boolean(false),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	team, err := application.Local().Create(context.Background(), "teams", store.Values{"name": store.String("Core")}, nil)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, path string
		values     store.Values
	}{
		{name: "missing singular", path: "owner", values: store.Values{"owner": store.String("missing")}},
		{name: "filtered has many", path: "watchers.1", values: store.Values{"watchers": store.List(store.String(visible.ID), store.String(hidden.ID))}},
		{name: "missing polymorphic", path: "subject", values: store.Values{"subject": store.Object(store.Values{"relationTo": store.String("people"), "id": store.String("missing")})}},
		{name: "denied nested group", path: "meta.reviewer", values: store.Values{"meta": store.Object(store.Values{"reviewer": store.String(denied.ID)})}},
		{name: "nested array", path: "sections.0.editor", values: store.Values{"sections": store.List(store.Object(store.Values{"editor": store.String(hidden.ID)}))}},
		{name: "nested block", path: "content.0.source", values: store.Values{"content": store.List(store.Object(store.Values{"blockType": store.String("quote"), "source": store.String(hidden.ID)}))}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := application.Local().Create(context.Background(), "entries", test.values, nil); !relationshipIssue(err, test.path) {
				t.Fatalf("relationship error = %v", err)
			}
		})
	}
	created, err := application.Local().Create(context.Background(), "entries", store.Values{
		"owner":    store.String(visible.ID),
		"watchers": store.List(store.String(visible.ID), store.String(visible.ID)),
		"subject":  store.Object(store.Values{"relationTo": store.String("teams"), "id": store.String(team.ID)}),
		"meta":     store.Object(store.Values{"reviewer": store.String(visible.ID)}),
		"sections": store.List(store.Object(store.Values{"editor": store.String(visible.ID)})),
		"content":  store.List(store.Object(store.Values{"blockType": store.String("quote"), "source": store.String(visible.ID)})),
	}, nil)
	if err != nil || created.ID == "" {
		t.Fatalf("valid relationship document = %#v, %v", created, err)
	}
	if !slices.Contains(checkedIDs, hidden.ID) || !slices.Contains(checkedIDs, "missing") {
		t.Fatalf("target access received IDs %v", checkedIDs)
	}
}

func TestAccessContextsReuseTransactionAndExposeWriteState(t *testing.T) {
	backend := teststore.New()
	var policyID string
	var collectionContext ridu.AccessContext
	var fieldContext operation.AccessContext
	application, err := ridu.New(ridu.Config{Name: "Access contexts", Collections: []ridu.Collection{
		{Slug: "policies", Fields: field.Fields{field.Text("name").Required()}},
		{
			Slug: "notes", Fields: field.Fields{field.Text("title").Required().Access(field.Access{Update: func(ctx operation.AccessContext,

			) (bool, error) {
				fieldContext = ctx
				_, err := ctx.Local.FindByID(ctx.Context, "policies", operation.ID(policyID))
				return true, err
			}}), field.Text("summary")},
			Access: ridu.CollectionAccess{Update: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
				collectionContext = ctx
				ctx.Data["title"] = store.String("mutated access snapshot")
				_, err := ctx.Local.Find(ctx.Context, "policies", policyID, ctx.Actor)
				return ridu.Allow(), err
			}},
		},
	}}, backend)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := application.Local().Create(context.Background(), "policies", store.Values{"name": store.String("Editors")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	policyID = policy.ID
	note, err := application.Local().Create(context.Background(), "notes", store.Values{
		"title": store.String("Before"), "summary": store.String("Sibling"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	beforeBegins := count(backend.Events(), "begin")
	updated, err := application.Local().Update(context.Background(), "notes", note.ID, store.Values{
		"title": store.String("After"), "summary": store.String("Updated sibling"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := updated.Values["title"].StringValue(); got != "After" {
		t.Fatalf("access mutated operation data: %q", got)
	}
	if count(backend.Events(), "begin") != beforeBegins+1 {
		t.Fatalf("nested access calls started another transaction: %v", backend.Events())
	}
	if collectionContext.ID != note.ID || collectionContext.Local == nil {
		t.Fatalf("collection access context = %#v", collectionContext)
	}
	if got, _ := collectionContext.Data["title"].StringValue(); got != "mutated access snapshot" {
		t.Fatalf("captured detached access data = %q", got)
	}
	value, _ := fieldContext.Siblings.String("title")
	sibling, _ := fieldContext.Siblings.String("summary")
	original, _ := fieldContext.Prior.String("title")
	document, _ := fieldContext.Root.String("title")
	if string(fieldContext.ID) != note.ID || fieldContext.Local == nil || value != "After" || sibling != "Updated sibling" || original != "Before" || document != "After" {
		t.Fatalf("field access context = %#v", fieldContext)
	}
}

func TestNestedFieldAccessUsesRuntimePathsAndSiblingRows(t *testing.T) {
	var readPaths []string
	application, err := ridu.New(ridu.Config{Name: "Nested field access", Collections: []ridu.Collection{{
		Slug: "notes",
		Fields: field.Fields{field.Array("rows", field.Fields{field.Text("secret").Access(field.Access{
			Read: func(ctx operation.AccessContext,

			) (bool, error) {
				readPaths = append(readPaths, string(ctx.OccurrenceID))
				visible, _ := ctx.Siblings.Get("visible").
					BooleanValue()
				return visible, nil
			},
			Update: func(ctx operation.AccessContext,

			) (bool, error) {
				visible, _ := ctx.Siblings.Get("visible").
					BooleanValue()
				return visible, nil
			},
		}), field.Checkbox("visible").Required()})},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.Local().Create(context.Background(), "notes", store.Values{
		"rows": store.List(
			store.Object(store.Values{"secret": store.String("shown"), "visible": store.Boolean(true)}),
			store.Object(store.Values{"secret": store.String("hidden"), "visible": store.Boolean(false)}),
		),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := created.Values["rows"].CopyList()
	first, _ := rows[0].CopyObject()
	second, _ := rows[1].CopyObject()
	if _, exists := first["secret"]; !exists {
		t.Fatal("visible row secret was redacted")
	}
	if _, exists := second["secret"]; exists {
		t.Fatal("hidden row secret was not redacted")
	}
	if len(readPaths) != 2 || readPaths[0] == "" || readPaths[0] == readPaths[1] {
		t.Fatalf("nested read runtime paths = %v", readPaths)
	}
	if _, err := application.Local().Update(context.Background(), "notes", created.ID, store.Values{
		"rows": store.List(
			store.Object(store.Values{"secret": store.String("allowed"), "visible": store.Boolean(true)}),
			store.Object(store.Values{"secret": store.String("denied"), "visible": store.Boolean(false)}),
		),
	}, nil); !fieldAccessIssue(err, "rows.1.secret") {
		t.Fatalf("nested field access error = %v", err)
	}
}

func TestNestedPatchesPreserveOmittedProtectedFields(t *testing.T) {
	protectedUpdateCalls := 0
	denyUpdate := func(operation.AccessContext,

	) (bool, error) {
		protectedUpdateCalls++
		return false, nil
	}
	application, err := ridu.New(ridu.Config{Name: "Nested patch protection", Collections: []ridu.Collection{{
		Slug:   "pages",
		Fields: field.Fields{field.Group("meta", field.Fields{field.Text("public"), field.Text("secret").Access(field.Access{Update: denyUpdate})}), field.Array("rows", field.Fields{field.Text("public"), field.Text("secret").Access(field.Access{Update: denyUpdate})}), field.Blocks("content", field.Block{Slug: "quote", Fields: field.Fields{field.Text("public"), field.Text("secret").Access(field.Access{Update: denyUpdate})}})},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.Local().Create(context.Background(), "pages", store.Values{
		"meta": store.Object(store.Values{"public": store.String("old meta"), "secret": store.String("meta secret")}),
		"rows": store.List(store.Object(store.Values{
			"_key": store.String("row-1"), "public": store.String("old row"), "secret": store.String("row secret"),
		})),
		"content": store.List(store.Object(store.Values{
			"_key": store.String("block-1"), "blockType": store.String("quote"),
			"public": store.String("old block"), "secret": store.String("block secret"),
		})),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := application.Local().Update(context.Background(), "pages", created.ID, store.Values{
		"meta": store.Object(store.Values{"public": store.String("new meta")}),
		"rows": store.List(store.Object(store.Values{"_key": store.String("row-1"), "public": store.String("new row")})),
		"content": store.List(store.Object(store.Values{
			"_key": store.String("block-1"), "blockType": store.String("quote"), "public": store.String("new block"),
		})),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if protectedUpdateCalls != 0 {
		t.Fatalf("omitted protected fields ran update access %d times", protectedUpdateCalls)
	}
	meta, _ := updated.Values["meta"].CopyObject()
	rows, _ := updated.Values["rows"].CopyList()
	row, _ := rows[0].CopyObject()
	content, _ := updated.Values["content"].CopyList()
	block, _ := content[0].CopyObject()
	if stringValue(meta["public"]) != "new meta" || stringValue(meta["secret"]) != "meta secret" ||
		stringValue(row["public"]) != "new row" || stringValue(row["secret"]) != "row secret" ||
		stringValue(block["public"]) != "new block" || stringValue(block["secret"]) != "block secret" {
		t.Fatalf("nested patch result = %#v", updated.Values)
	}
	protectedUpdateCalls = 0
	for _, test := range []struct {
		name   string
		patch  store.Values
		denied string
	}{
		{name: "group clear", patch: store.Values{"meta": store.Null()}, denied: "meta.secret"},
		{name: "array row removal", patch: store.Values{"rows": store.List()}, denied: "rows.0.secret"},
		{name: "block removal", patch: store.Values{"content": store.List()}, denied: "content.0.secret"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := application.Local().Update(context.Background(), "pages", created.ID, test.patch, nil); !fieldAccessIssue(err, test.denied) {
				t.Fatalf("nested removal error = %v", err)
			}
		})
	}
	if protectedUpdateCalls != 3 {
		t.Fatalf("protected removal access calls = %d, want 3", protectedUpdateCalls)
	}
	current, err := application.Local().Find(context.Background(), "pages", created.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta, _ = current.Values["meta"].CopyObject()
	rows, _ = current.Values["rows"].CopyList()
	row, _ = rows[0].CopyObject()
	content, _ = current.Values["content"].CopyList()
	block, _ = content[0].CopyObject()
	if stringValue(meta["secret"]) != "meta secret" || stringValue(row["secret"]) != "row secret" || stringValue(block["secret"]) != "block secret" {
		t.Fatalf("denied nested removal changed storage: %#v", current.Values)
	}
}

func TestDuplicateStructuredRowKeysCannotBypassFieldAccess(t *testing.T) {
	denySecret := func(operation.AccessContext,

	) (bool, error) {
		return false, nil
	}
	application, err := ridu.New(ridu.Config{Name: "Structured row identity", Collections: []ridu.Collection{{
		Slug:   "pages",
		Fields: field.Fields{field.Array("rows", field.Fields{field.Text("secret").Access(field.Access{Update: denySecret})}), field.Blocks("content", field.Block{Slug: "quote", Fields: field.Fields{field.Text("secret").Access(field.Access{Update: denySecret})}})},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		data store.Values
		path string
	}{
		{
			name: "array",
			data: store.Values{"rows": store.List(
				store.Object(store.Values{"_key": store.String("duplicate"), "secret": store.String("first")}),
				store.Object(store.Values{"_key": store.String("duplicate"), "secret": store.String("second")}),
			)},
			path: "rows.1._key",
		},
		{
			name: "blocks",
			data: store.Values{"content": store.List(
				store.Object(store.Values{"_key": store.String("duplicate"), "blockType": store.String("quote"), "secret": store.String("first")}),
				store.Object(store.Values{"_key": store.String("duplicate"), "blockType": store.String("quote"), "secret": store.String("second")}),
			)},
			path: "content.1._key",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := application.Local().Create(context.Background(), "pages", test.data, nil)
			var operationError *ridu.OperationError
			if !errors.As(err, &operationError) {
				t.Fatalf("create error = %v, want validation error", err)
			}
			found := false
			for _, issue := range operationError.Issues {
				found = found || issue.Code == "duplicate_row_key" && issue.Path == test.path
			}
			if !found {
				t.Fatalf("validation issues = %#v, want duplicate_row_key at %s", operationError.Issues, test.path)
			}
		})
	}
}

func TestBlockFieldAccessUsesCanonicalTypeAndRuntimeRows(t *testing.T) {
	var readPaths []string
	application, err := ridu.New(ridu.Config{Name: "Block field access", Collections: []ridu.Collection{{
		Slug: "pages",
		Fields: field.Fields{field.Blocks("content", field.Block{Slug: "quote", Fields: field.Fields{field.Text("source").Access(field.Access{
			Read: func(ctx operation.AccessContext,

			) (bool, error) {
				readPaths = append(readPaths, string(ctx.OccurrenceID))
				visible, _ := ctx.Siblings.Get("visible").
					BooleanValue()
				return visible, nil
			},
			Update: func(ctx operation.AccessContext,

			) (bool, error) {
				visible, _ := ctx.Siblings.Get("visible").
					BooleanValue()
				return visible, nil
			},
		}), field.Checkbox("visible").Required()}}, field.Block{Slug: "heading", Fields: field.Fields{field.Text("text")}})},
	}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.Local().Create(context.Background(), "pages", store.Values{
		"content": store.List(
			store.Object(store.Values{"blockType": store.String("quote"), "source": store.String("shown"), "visible": store.Boolean(true)}),
			store.Object(store.Values{"blockType": store.String("heading"), "text": store.String("Unrelated")}),
			store.Object(store.Values{"blockType": store.String("quote"), "source": store.String("hidden"), "visible": store.Boolean(false)}),
		),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := created.Values["content"].CopyList()
	shown, _ := content[0].CopyObject()
	hidden, _ := content[2].CopyObject()
	if _, exists := shown["source"]; !exists {
		t.Fatal("visible quote source was redacted")
	}
	if _, exists := hidden["source"]; exists {
		t.Fatal("hidden quote source was not redacted")
	}
	if len(readPaths) != 2 || readPaths[0] == "" || readPaths[0] == readPaths[1] {
		t.Fatalf("block read runtime paths = %v", readPaths)
	}
	if _, err := application.Local().Update(context.Background(), "pages", created.ID, store.Values{
		"content": store.List(
			store.Object(store.Values{"blockType": store.String("quote"), "source": store.String("allowed"), "visible": store.Boolean(true)}),
			store.Object(store.Values{"blockType": store.String("heading"), "text": store.String("Unrelated")}),
			store.Object(store.Values{"blockType": store.String("quote"), "source": store.String("denied"), "visible": store.Boolean(false)}),
		),
	}, nil); !fieldAccessIssue(err, "content.2.source") {
		t.Fatalf("block field access error = %v", err)
	}
}

func TestFieldAccessHookMutationOriginalDocumentAndRecursionGuard(t *testing.T) {
	backend := teststore.New()
	var originalTitle string
	var fieldHookID operation.OccurrenceID
	application, err := ridu.New(ridu.Config{
		Name: "Lifecycle security",
		Collections: []ridu.Collection{{
			Slug: "notes",
			Fields: field.Fields{field.Text("title").Required().Hooks(field.Hooks[string]{BeforeValidate: []field.RawTransform{func(ctx operation.WriteContext, _ operation.Value[store.Value]) (operation.Change[store.Value], error) {
				fieldHookID = ctx.OccurrenceID
				return operation.Keep[store.Value](), nil
			}}}), field.Text("secret").Access(field.Access{
				Read: func(operation.AccessContext,

				) (bool, error) {
					return false, nil
				},
				Update: func(operation.AccessContext,

				) (bool, error) {
					return false, nil
				},
			}), field.Group("meta", field.Fields{field.Text("hidden").Access(field.Access{Read: func(operation.AccessContext,

			) (bool, error) {
				return false, nil
			}})})},
			Hooks: ridu.CollectionHooks{
				BeforeValidate: []ridu.Hook{func(ctx ridu.HookContext) error {
					if _, exists := ctx.Data["title"]; !exists {
						ctx.Data["title"] = store.String("hook default")
					}
					return nil
				}},
				BeforeOperation: []ridu.Hook{func(ctx ridu.HookContext) error {
					if ctx.Original != nil {
						originalTitle, _ = ctx.Original.Values["title"].StringValue()
					}
					return nil
				}},
			},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.Local().Create(context.Background(), "notes", store.Values{"secret": store.String("hidden"), "meta": store.Object(store.Values{"hidden": store.String("nested")})}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if title, _ := created.Values["title"].StringValue(); title != "hook default" {
		t.Fatalf("hook-mutated title = %q", title)
	}
	if _, leaked := created.Values["secret"]; leaked {
		t.Fatal("field read access leaked secret on create result")
	}
	meta, _ := created.Values["meta"].CopyObject()
	if _, leaked := meta["hidden"]; leaked {
		t.Fatal("nested field read access leaked meta.hidden")
	}
	if fieldHookID == "" {
		t.Fatalf("field hook path = %q", fieldHookID)
	}
	if _, err := application.Local().Update(context.Background(), "notes", created.ID, store.Values{"secret": store.String("changed")}, nil); !operationCode(err, "field_access_denied") {
		t.Fatalf("protected field update error = %v", err)
	}
	if _, err := application.Local().Update(context.Background(), "notes", created.ID, store.Values{"title": store.String("updated")}, nil); err != nil {
		t.Fatal(err)
	}
	if originalTitle != "hook default" {
		t.Fatalf("before-operation original title = %q", originalTitle)
	}

	recursiveBackend := teststore.New()
	recursive, err := ridu.New(ridu.Config{Name: "Recursion", Collections: []ridu.Collection{{
		Slug: "loops", Fields: field.Fields{field.Text("title").Required()},
		Hooks: ridu.CollectionHooks{AfterOperation: []ridu.Hook{func(ctx ridu.HookContext) error {
			_, err := ctx.Local.Create(ctx.Context, "loops", store.Values{"title": store.String("again")}, nil)
			return err
		}}},
	}}}, recursiveBackend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recursive.Local().Create(context.Background(), "loops", store.Values{"title": store.String("start")}, nil); err == nil {
		t.Fatal("recursive hook succeeded")
	}
	if slices.Contains(recursiveBackend.Events(), "commit") {
		t.Fatalf("recursive operation committed: %v", recursiveBackend.Events())
	}
}

func operationCode(err error, code string) bool {
	var operationError *ridu.OperationError
	return errors.As(err, &operationError) && operationError.Code == code
}

func relationshipIssue(err error, path string) bool {
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "validation" {
		return false
	}
	for _, issue := range operationError.Issues {
		if issue.Code == "invalid_relationship" && issue.Path == path && issue.Message == "relationship target is unavailable" {
			return true
		}
	}
	return false
}

func fieldAccessIssue(err error, path string) bool {
	var operationError *ridu.OperationError
	return errors.As(err, &operationError) && operationError.Code == "field_access_denied" &&
		len(operationError.Issues) == 1 && operationError.Issues[0].Path == path
}

func count(values []string, target string) int {
	count := 0
	for _, value := range values {
		if value == target {
			count++
		}
	}
	return count
}
