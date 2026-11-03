package core_test

import (
	"errors"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

// A hook may look up a document that might be missing, handle the miss, and
// keep using the transaction it shares with its operation.
func TestNestedReadMissLeavesTheSharedTransactionUsable(t *testing.T) {
	mode := "clear"
	app, err := ridu.New(ridu.Config{
		Name: "nested read miss",
		Collections: []ridu.Collection{
			{
				Slug:   "issues",
				Fields: field.Fields{field.Text("title").Required(), field.Text("parent")},
				Hooks: ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(hook ridu.HookContext) error {
					parent, _ := hook.Data["parent"].StringValue()
					if parent == "" {
						return nil
					}
					_, findError := hook.Local.Find(hook.Context, "issues", parent, ridu.FindOptions{})
					if findError == nil {
						return nil
					}
					var failure *ridu.OperationError
					if !errors.As(findError, &failure) || failure.Code != "not_found" {
						return findError
					}
					switch mode {
					case "reject":
						return ridu.Reject("The parent issue no longer exists")
					case "write":
						if _, writeError := hook.Local.Create(hook.Context, "activity", store.Values{}, ridu.MutationOptions{}); writeError == nil {
							t.Error("activity without a note was saved")
						}
						return nil
					}
					hook.Data["parent"] = store.Null()
					_, writeError := hook.Local.Create(hook.Context, "activity", store.Values{"note": store.String("cleared a missing parent")}, ridu.MutationOptions{})
					return writeError
				}}},
			},
			{Slug: "activity", Fields: field.Fields{field.Text("note").Required()}},
		},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	local := app.Local()

	created, err := local.Create(t.Context(), "issues", store.Values{"title": store.String("Child"), "parent": store.String("missing")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("create after a handled miss: %v", err)
	}
	if parent, _ := created.Values["parent"].StringValue(); parent != "" {
		t.Fatalf("parent = %q, want it cleared", parent)
	}
	activity, err := local.List(t.Context(), "activity", ridu.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(activity.Documents) != 1 {
		t.Fatalf("activity rows = %d, want the hook's nested write committed", len(activity.Documents))
	}

	mode = "reject"
	_, err = local.Create(t.Context(), "issues", store.Values{"title": store.String("Orphan"), "parent": store.String("missing")}, ridu.MutationOptions{})
	var failure *ridu.OperationError
	if !errors.As(err, &failure) || failure.Code != "rejected" || failure.Status != 422 {
		t.Fatalf("hook rejection after a miss = %#v (%v), want rejected 422", failure, err)
	}

	// A failed nested write still ends the shared transaction.
	mode = "write"
	_, err = local.Create(t.Context(), "issues", store.Values{"title": store.String("Ignored failure"), "parent": store.String("missing")}, ridu.MutationOptions{})
	if err == nil {
		t.Fatal("create succeeded after a nested write failed")
	}
	issues, err := local.List(t.Context(), "issues", ridu.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues.Documents) != 1 {
		t.Fatalf("issues = %d, want only the first create saved", len(issues.Documents))
	}
}
