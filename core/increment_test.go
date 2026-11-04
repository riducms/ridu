package core_test

import (
	"context"
	"errors"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func incrementApplication(t *testing.T, hooks ridu.CollectionHooks) *ridu.App {
	t.Helper()
	application, err := ridu.New(ridu.Config{
		Name: "Increments",
		Collections: []ridu.Collection{
			{Slug: "teams", Fields: field.Fields{field.Text("key").Required(), field.Number("nextIssue").Max(1000), field.Text("title").Localized()},
				Access: ridu.CollectionAccess{Update: func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
					if ctx.Actor == nil {
						return ridu.Deny(), nil
					}
					return ridu.Allow(), nil
				}}},
			{Slug: "issues", Fields: field.Fields{field.Relationship("team", "teams").Required(), field.Number("number"), field.Text("title").Required()}, Hooks: hooks},
			{Slug: "drafts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Number("count")}},
		},
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	return application
}

func TestIncrementAddsToANumberFieldUnderTheRowLock(t *testing.T) {
	application := incrementApplication(t, ridu.CollectionHooks{})
	ctx, local, system := context.Background(), application.Local(), ridu.MutationOptions{System: true}
	team, err := local.Create(ctx, "teams", store.Values{"key": store.String("ENG")}, system)
	if err != nil {
		t.Fatal(err)
	}
	for want := 1.0; want <= 2; want++ {
		updated, err := local.Increment(ctx, "teams", team.ID, "nextIssue", 1, system)
		if got, _ := updated.Values["nextIssue"].NumberValue(); err != nil || got != want {
			t.Fatalf("increment %v = %v, %v", want, got, err)
		}
	}
	if updated, err := local.Increment(ctx, "teams", team.ID, "nextIssue", -0.5, system); err != nil {
		t.Fatal(err)
	} else if got, _ := updated.Values["nextIssue"].NumberValue(); got != 1.5 {
		t.Fatalf("negative increment = %v", got)
	}

	// The adapters' conformance suites and the SQLite increment test prove
	// that concurrent increments never read the same value.

	// Validation applies to the sum, as it would to a submitted value.
	if _, err := local.Increment(ctx, "teams", team.ID, "nextIssue", 1000, system); err == nil {
		t.Fatal("an increment past Max was saved")
	}
}

func TestIncrementInsideAHookRollsBackWithTheOperation(t *testing.T) {
	application := incrementApplication(t, ridu.CollectionHooks{BeforeChange: []ridu.Hook{func(ctx ridu.HookContext) error {
		if ctx.Operation != "create" {
			return nil
		}
		teamID, _ := ctx.Data["team"].StringValue()
		team, err := ctx.Local.Increment(ctx.Context, "teams", teamID, "nextIssue", 1, ridu.MutationOptions{System: true})
		if err != nil {
			return err
		}
		ctx.Data["number"] = team.Values["nextIssue"]
		return nil
	}}})
	ctx, local, system := context.Background(), application.Local(), ridu.MutationOptions{System: true}
	team, err := local.Create(ctx, "teams", store.Values{"key": store.String("ENG")}, system)
	if err != nil {
		t.Fatal(err)
	}
	first, err := local.Create(ctx, "issues", store.Values{"team": store.String(team.ID), "title": store.String("First")}, system)
	if number, _ := first.Values["number"].NumberValue(); err != nil || number != 1 {
		t.Fatalf("first issue number = %v, %v", number, err)
	}
	// The missing title fails validation after the hook took number 2, so the
	// rollback gives it back.
	if _, err := local.Create(ctx, "issues", store.Values{"team": store.String(team.ID)}, system); err == nil {
		t.Fatal("an issue without a title was created")
	}
	second, err := local.Create(ctx, "issues", store.Values{"team": store.String(team.ID), "title": store.String("Second")}, system)
	if number, _ := second.Values["number"].NumberValue(); err != nil || number != 2 {
		t.Fatalf("second issue number = %v, %v", number, err)
	}
}

func TestIncrementRefusesFieldsItCantCountSafely(t *testing.T) {
	application := incrementApplication(t, ridu.CollectionHooks{})
	ctx, local, system := context.Background(), application.Local(), ridu.MutationOptions{System: true}
	team, err := local.Create(ctx, "teams", store.Values{"key": store.String("ENG")}, system)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := local.Create(ctx, "drafts", store.Values{}, system)
	if err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []struct{ collection, id, name string }{
		{"teams", team.ID, "key"},
		{"teams", team.ID, "title"},
		{"teams", team.ID, "missing"},
		{"drafts", draft.ID, "count"},
	} {
		_, err := local.Increment(ctx, attempt.collection, attempt.id, attempt.name, 1, system)
		var operationError *ridu.OperationError
		if !errors.As(err, &operationError) || operationError.Status != 422 || len(operationError.Issues) != 1 || operationError.Issues[0].Code != "invalid_increment" {
			t.Errorf("increment of %s.%s = %v", attempt.collection, attempt.name, err)
		}
	}
	if _, err := local.Increment(ctx, "teams", team.ID, "nextIssue", 1, ridu.MutationOptions{}); err == nil {
		t.Fatal("an anonymous increment passed the update access rule")
	}
}
