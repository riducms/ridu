package core_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/tests/contracts/issuetargets"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
)

func TestAggregateIssueTargetsAgreeAcrossLocalAndREST(t *testing.T) {
	row := func(key, heading string, links ...store.Value) store.Value {
		return store.Object(store.Values{"_key": store.String(key), "heading": store.String(heading), "links": store.List(links...)})
	}
	link := store.Object(store.Values{"_key": store.String("link-b"), "url": store.String("invalid")})
	block := store.Object(store.Values{"_key": store.String("block-a"), "blockType": store.String("card"), "heading": store.String("invalid"), "links": store.List(link)})
	body := richtextblocks.Document(richtextblocks.Block("card", "embed-a", store.Values{"links": store.List(link)}))
	for _, test := range []struct {
		name, field, locale string
		value               store.Value
		paths, keys         []string
	}{
		{"array child", "sections", "en", store.List(row("section-a", "invalid")), []string{"sections.0.heading"}, []string{"section-a"}},
		{"nested array child", "sections", "en", store.List(row("section-a", "ok", link)), []string{"sections.0.links.0.url"}, []string{"section-a", "link-b"}},
		{"block child and nested array", "content", "en", store.List(block), []string{"content.0.heading", "content.0.links.0.url"}, []string{"block-a", "card"}},
		{"localized array", "localizedSections", "fr", store.List(row("section-a", "ok", link)), []string{"localizedSections.0.links.0.url"}, []string{"section-a", "link-b"}},
		{"embedded array", "body", "en", body, []string{"body.root.children.0.fields.links.0.url"}, []string{"embed-a", "link-b"}},
		{"localized embedded array", "localizedBody", "fr", body, []string{"localizedBody.root.children.0.fields.links.0.url"}, []string{"embed-a", "link-b"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, err := core.New(issuetargets.Config(), teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			values := store.Values{"title": store.String("Targets"), test.field: test.value}
			_, err = app.Local().CreateWithOptions(t.Context(), "issue-targets", values, core.MutationOptions{Locale: schema.LocaleCode(test.locale)})
			var failure *core.OperationError
			if !errors.As(err, &failure) || len(failure.Issues) != len(test.paths) {
				t.Fatalf("local validation = %v", err)
			}
			encoded, _ := json.Marshal(values)
			response := requestJSON(t, handlerClient(app.Handler(core.HandlerOptions{})), http.MethodPost, "http://ridu.test/api/collections/issue-targets?locale="+test.locale, strings.NewReader(string(encoded)), "")
			if response.StatusCode != 422 {
				t.Fatalf("REST = %d %s", response.StatusCode, readBody(t, response))
			}
			var wire protocol.ErrorEnvelope
			decodeResponse(t, response, &wire)
			localJSON, _ := json.Marshal(failure.Issues)
			wireJSON, _ := json.Marshal(wire.Error.Issues)
			if string(localJSON) != string(wireJSON) {
				t.Fatalf("Local/REST disagree: %s / %s", localJSON, wireJSON)
			}
			for i, issue := range failure.Issues {
				if issue.Path != test.paths[i] || issue.Locale != schema.LocaleCode(test.locale) || issue.CollectionID != "issue-targets" || issue.FieldID == "" || issue.GlobalID != "" {
					t.Fatalf("resolved issue = %+v", issue)
				}
				var target []string
				if err := json.Unmarshal([]byte(issue.Target), &target); err != nil {
					t.Fatal(err)
				}
				for _, key := range test.keys {
					if !slices.Contains(target, key) {
						t.Fatalf("target %v lost key/case %q", target, key)
					}
				}
			}
		})
	}
}

func TestAggregateIssueUsesKeysAfterCandidateReorder(t *testing.T) {
	config := issuetargets.Config()
	config.Collections[0].Hooks.BeforeChange = []core.Hook{func(ctx core.HookContext) error {
		rows, _ := ctx.Data["sections"].CopyList()
		slices.Reverse(rows)
		ctx.Data["sections"] = store.List(rows...)
		return nil
	}}
	app, err := core.New(config, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Local().Create(t.Context(), "issue-targets", store.Values{"title": store.String("Reordered"), "sections": store.List(
		store.Object(store.Values{"_key": store.String("a"), "heading": store.String("invalid")}),
		store.Object(store.Values{"_key": store.String("b"), "heading": store.String("ok")}),
	)}, nil)
	var failure *core.OperationError
	if !errors.As(err, &failure) || len(failure.Issues) != 1 || failure.Issues[0].Path != "sections.1.heading" {
		t.Fatalf("reordered issue = %v", err)
	}
	var target []string
	_ = json.Unmarshal([]byte(failure.Issues[0].Target), &target)
	if !slices.Contains(target, "a") || slices.Contains(target, "b") {
		t.Fatalf("wrong stable target: %v", target)
	}
}

func TestSimpleIssueTargetAndInvalidDescendants(t *testing.T) {
	for _, target := range []operation.IssueTarget{operation.At("seo.title"), operation.At("missing"), operation.At("seo").Row("a"), operation.At("seo.title").Field("impossible")} {
		group := field.Group("details", field.Fields{field.Group("seo", field.Fields{field.Text("title")})}).Validate(func(operation.ValidationContext, operation.Value[store.Value]) ([]operation.Issue, error) {
			return []operation.Issue{{Code: "title", Message: "Invalid title", Target: target}}, nil
		})
		app, err := core.New(core.Config{Name: "Targets", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{group}}}}, teststore.New())
		if err != nil {
			t.Fatal(err)
		}
		_, err = app.Local().Create(t.Context(), "pages", store.Values{"details": store.Object(store.Values{"seo": store.Object(store.Values{"title": store.String("invalid")})})}, nil)
		var failure *core.OperationError
		if !errors.As(err, &failure) {
			t.Fatalf("expected operation error: %v", err)
		}
		if reflect.DeepEqual(target.Segments(), operation.At("seo.title").Segments()) {
			if len(failure.Issues) != 1 || failure.Issues[0].Path != "details.seo.title" {
				t.Fatalf("relative group issue = %+v", failure)
			}
		} else if failure.Status != 500 || failure.Cause == nil || !strings.Contains(failure.Cause.Error(), "invalid validation issue target") {
			t.Fatalf("invalid target was not diagnosed: %v", err)
		}
	}
}

func TestAggregateIssueCannotLoseIdentityInsideAnAbsentGroup(t *testing.T) {
	for _, test := range []struct {
		name   string
		target operation.IssueTarget
		data   store.Values
		valid  bool
	}{
		{"absent enclosing group", operation.At().Row("a").Field("details.url"), nil, false},
		{"null enclosing group", operation.At().Row("a").Field("details.url"), store.Values{"details": store.Null()}, false},
		{"absent group itself", operation.At().Row("a").Field("details"), nil, true},
		{"missing scalar in existing group", operation.At().Row("a").Field("details.url"), store.Values{"details": store.Object(store.Values{})}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := field.Array("rows", field.Fields{field.Group("details", field.Fields{field.Text("url")})}).Validate(func(operation.ValidationContext, operation.Value[store.Value]) ([]operation.Issue, error) {
				return []operation.Issue{{Code: "link", Message: "Supply a link", Target: test.target}}, nil
			})
			config := core.Config{Name: "Targets", Collections: []core.Collection{{Slug: "pages", Fields: field.Fields{rows}}}}
			config.Collections[0].Hooks.BeforeChange = []core.Hook{func(ctx core.HookContext) error {
				values, _ := ctx.Data["rows"].CopyList()
				slices.Reverse(values)
				ctx.Data["rows"] = store.List(values...)
				return nil
			}}
			app, err := core.New(config, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			row := store.Values{"_key": store.String("a")}
			for key, value := range test.data {
				row[key] = value
			}
			_, err = app.Local().Create(t.Context(), "pages", store.Values{"rows": store.List(store.Object(row), store.Object(store.Values{"_key": store.String("b")}))}, nil)
			var failure *core.OperationError
			if !errors.As(err, &failure) {
				t.Fatalf("expected validation result: %v", err)
			}
			if !test.valid {
				if failure.Status != 500 || failure.Cause == nil || !strings.Contains(failure.Cause.Error(), "target the containing group instead") {
					t.Fatalf("unaddressable descendant silently accepted: %+v", failure)
				}
				return
			}
			if len(failure.Issues) != 1 || failure.Issues[0].Target == "" || !strings.HasPrefix(failure.Issues[0].Path, "rows.1.details") {
				t.Fatalf("addressable issue lost stable correlation: %+v", failure)
			}
			var target []string
			_ = json.Unmarshal([]byte(failure.Issues[0].Target), &target)
			if !slices.Contains(target, "a") || slices.Contains(target, "b") {
				t.Fatalf("issue followed the wrong row: %v", target)
			}
		})
	}
}
