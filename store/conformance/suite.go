// Package conformance provides the reusable black-box contract suite for Ridu
// document-store adapters.
package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Factory opens a clean store prepared for manifest. Implementations should
// register cleanup with t and fail the current test when setup cannot finish.
type Factory func(t *testing.T, manifest schema.Manifest) store.Store

// Run executes the shared semantic contract for one adapter. Adapter packages
// should invoke Run from their own tests so their ordinary integration setup
// remains responsible for schema creation and external-service availability.
func Run(t *testing.T, factory Factory) {
	t.Helper()
	if factory == nil {
		t.Fatal("store conformance requires a factory")
	}
	t.Run("primitive-lists", func(t *testing.T) { runPrimitiveLists(t, factory) })
	manifest := conformanceManifest()
	runTest := func(name string, test func(*testing.T)) {
		t.Run(name, test)
	}

	runTest("filter-and-access-compose", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		createRecords(t, fixture, []recordFixture{
			{id: id(1), state: "public", rank: 1},
			{id: id(2), state: "private", rank: 2},
			{id: id(3), state: "public", rank: 3},
		})
		rank := mustPath("rank")
		state := mustPath("state")
		filter := query.GreaterThanEqual(rank, query.Number(2)).Node()
		access := query.Equal(state, query.String("public")).Node()
		transaction := begin(t, fixture.backend)
		page, err := transaction.List(t.Context(), fixture.request(store.Request{
			Collection: fixture.records, Collections: fixture.collections,
			Filter: &filter, Access: &access, Page: 1, Limit: 10,
		}))
		rollback(t, transaction)
		if err != nil {
			t.Fatal(err)
		}
		assertDocumentIDs(t, page.Documents, id(3))
		if page.Total != 1 {
			t.Fatalf("composed predicate total = %d, want 1", page.Total)
		}
	})

	runTest("stable-ordering-and-pagination", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		createRecords(t, fixture, []recordFixture{
			{id: id(1), state: "public", rank: 2},
			{id: id(2), state: "public", rank: 1},
			{id: id(3), state: "public", rank: 1},
			{id: id(4), state: "public", rank: 3},
			{id: id(5), state: "public", rank: 2},
		})
		sortTerm, err := query.NewSort(mustPath("rank"), query.Ascending)
		if err != nil {
			t.Fatal(err)
		}
		want := [][]string{{id(2), id(3)}, {id(1), id(5)}, {id(4)}}
		for pageNumber, expected := range want {
			transaction := begin(t, fixture.backend)
			page, listError := transaction.List(t.Context(), fixture.request(store.Request{
				Collection: fixture.records, Collections: fixture.collections,
				Page: pageNumber + 1, Limit: 2, Sort: []query.Sort{sortTerm},
			}))
			rollback(t, transaction)
			if listError != nil {
				t.Fatal(listError)
			}
			assertDocumentIDs(t, page.Documents, expected...)
			if page.Total != 5 || page.Page != pageNumber+1 || page.Limit != 2 {
				t.Fatalf("page metadata = %#v, want total 5 page %d limit 2", page, pageNumber+1)
			}
		}
	})

	runTest("maximum-page-is-empty-with-exact-metadata", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		createRecords(t, fixture, []recordFixture{
			{id: id(1), state: "public", rank: 1},
			{id: id(2), state: "public", rank: 2},
		})
		transaction := begin(t, fixture.backend)
		page, err := transaction.List(t.Context(), fixture.request(store.Request{
			Collection: fixture.records, Collections: fixture.collections,
			Page: math.MaxInt, Limit: 2,
		}))
		rollback(t, transaction)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Documents) != 0 || page.Total != 2 || page.Page != math.MaxInt || page.Limit != 2 {
			t.Fatalf("maximum page = documents:%d total:%d page:%d limit:%d, want 0/2/%d/2", len(page.Documents), page.Total, page.Page, page.Limit, math.MaxInt)
		}
	})

	runTest("distinct-composes-filter-access-and-pagination", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		createRecords(t, fixture, []recordFixture{
			{id: id(1), state: "public", rank: 2},
			{id: id(2), state: "public", rank: 1},
			{id: id(3), state: "public", rank: 1},
			{id: id(4), state: "public", rank: 3},
			{id: id(5), state: "private", rank: 99},
			{id: id(6), state: "public", rank: 0},
			{id: id(7), state: "public", rank: math.Copysign(0, -1)},
		})
		rank := mustPath("rank")
		state := mustPath("state")
		filter := query.GreaterThanEqual(rank, query.Number(0)).Node()
		access := query.Equal(state, query.String("public")).Node()
		for pageNumber, want := range [][]float64{{0, 1}, {2, 3}} {
			transaction := begin(t, fixture.backend)
			distinct, ok := transaction.(store.DistinctTransaction)
			if !ok {
				rollback(t, transaction)
				t.Fatal("transaction does not implement store.DistinctTransaction")
			}
			page, err := distinct.Distinct(t.Context(), fixture.distinctRequest(store.DistinctRequest{
				Collection: fixture.records, Field: rank, Filter: &filter, Access: &access,
				Page: pageNumber + 1, Limit: 2,
			}))
			rollback(t, transaction)
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != 4 || page.Page != pageNumber+1 || page.Limit != 2 {
				t.Fatalf("distinct page metadata = %#v, want total 4 page %d limit 2", page, pageNumber+1)
			}
			if len(page.Values) != len(want) {
				t.Fatalf("distinct page %d values = %#v", pageNumber+1, page.Values)
			}
			for index, expected := range want {
				value, ok := page.Values[index].NumberValue()
				if !ok || value != expected {
					t.Fatalf("distinct page %d = %#v, want %#v", pageNumber+1, page.Values, want)
				}
			}
		}
	})

	runTest("list-limit-clamps-to-100", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		records := make([]recordFixture, 101)
		for index := range records {
			records[index] = recordFixture{id: id(index + 1), state: "public", rank: float64(index + 1)}
		}
		createRecords(t, fixture, records)
		transaction := begin(t, fixture.backend)
		page, err := transaction.List(t.Context(), fixture.request(store.Request{
			Collection: fixture.records, Collections: fixture.collections, Page: 1, Limit: 101,
		}))
		rollback(t, transaction)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Documents) != 100 || page.Total != 101 || page.Page != 1 || page.Limit != 100 {
			t.Fatalf("clamped page = documents:%d total:%d page:%d limit:%d, want 100/101/1/100", len(page.Documents), page.Total, page.Page, page.Limit)
		}
	})

	runTest("list-window-rejects-unsupported-request-state", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		base := fixture.request(store.Request{
			Collection: fixture.windows,
			IndexWindow: &store.IndexWindow{
				Path: mustPath("slug"), LowerBound: "a", UpperBound: "z",
			},
			Limit: 10,
		})
		for _, test := range []struct {
			name   string
			mutate func(*store.Request)
		}{
			{name: "expected-revision", mutate: func(request *store.Request) { request.ExpectedRevision = 1 }},
			{name: "trash", mutate: func(request *store.Request) { request.Deletion = store.DeletionTrash }},
			{name: "all", mutate: func(request *store.Request) { request.Deletion = store.DeletionAll }},
			{name: "reference-lock", mutate: func(request *store.Request) { request.Lock = store.LockReference }},
			{name: "mutation-lock", mutate: func(request *store.Request) { request.Lock = store.LockMutation }},
		} {
			t.Run(test.name, func(t *testing.T) {
				request := base
				test.mutate(&request)
				transaction := begin(t, fixture.backend)
				windows, ok := transaction.(store.WindowTransaction)
				if !ok {
					rollback(t, transaction)
					t.Fatal("transaction does not implement store.WindowTransaction")
				}
				_, err := windows.ListWindow(t.Context(), request)
				rollback(t, transaction)
				if err == nil {
					t.Fatal("list window accepted unsupported request state")
				}
			})
		}
	})

	runTest("checkbox-ordering", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		transaction := begin(t, fixture.backend)
		for _, item := range []struct {
			id   string
			flag bool
		}{
			{id: id(1), flag: true},
			{id: id(2), flag: false},
		} {
			values := recordValues("public", 1)
			values["flag"] = store.Boolean(item.flag)
			if _, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{Collection: fixture.records, ID: item.id, Values: values})); err != nil {
				rollback(t, transaction)
				t.Fatal(err)
			}
		}
		commit(t, transaction)
		flag := mustPath("flag")
		sortTerm, err := query.NewSort(flag, query.Ascending)
		if err != nil {
			t.Fatal(err)
		}
		read := begin(t, fixture.backend)
		page, err := read.List(t.Context(), fixture.request(store.Request{
			Collection: fixture.records, Collections: fixture.collections,
			Page: 1, Limit: 10, Sort: []query.Sort{sortTerm},
		}))
		rollback(t, read)
		if err != nil {
			t.Fatal(err)
		}
		assertDocumentIDs(t, page.Documents, id(2), id(1))

		for _, test := range []struct {
			name  string
			value bool
			want  string
		}{
			{name: "equal-true", value: true, want: id(1)},
			{name: "equal-false", value: false, want: id(2)},
		} {
			t.Run(test.name, func(t *testing.T) {
				filter := query.Equal(flag, query.Boolean(test.value)).Node()
				read := begin(t, fixture.backend)
				page, err := read.List(t.Context(), fixture.request(store.Request{
					Collection: fixture.records, Collections: fixture.collections,
					Filter: &filter, Page: 1, Limit: 10,
				}))
				rollback(t, read)
				if err != nil {
					t.Fatal(err)
				}
				assertDocumentIDs(t, page.Documents, test.want)
			})
		}
	})

	runTest("null-and-missing", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		transaction := begin(t, fixture.backend)
		for _, item := range []struct {
			id     string
			values store.Values
		}{
			{id: id(1), values: recordValues("public", 1)},
			{id: id(2), values: withOptional(recordValues("public", 2), store.Null())},
			{id: id(3), values: withOptional(recordValues("public", 3), store.String("present"))},
		} {
			if _, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{Collection: fixture.records, ID: item.id, Values: item.values})); err != nil {
				rollback(t, transaction)
				t.Fatal(err)
			}
		}
		commit(t, transaction)

		optional := mustPath("optional")
		for _, test := range []struct {
			name string
			node query.Node
			want []string
		}{
			{name: "equal-null", node: query.Equal(optional, query.Null()).Node(), want: []string{id(1), id(2)}},
			{name: "not-equal-null", node: query.NotEqual(optional, query.Null()).Node(), want: []string{id(3)}},
		} {
			t.Run(test.name, func(t *testing.T) {
				transaction := begin(t, fixture.backend)
				page, err := transaction.List(t.Context(), fixture.request(store.Request{
					Collection: fixture.records, Collections: fixture.collections,
					Filter: &test.node, Page: 1, Limit: 10,
				}))
				rollback(t, transaction)
				if err != nil {
					t.Fatal(err)
				}
				assertDocumentIDs(t, page.Documents, test.want...)
			})
		}
	})

	runTest("repeated-predicate-logic-is-document-scoped", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		transaction := begin(t, fixture.backend)
		for _, item := range []struct {
			id     string
			values store.Values
		}{
			{
				id: id(1),
				values: withRepeatedValues(recordValues("public", 1),
					store.List(
						store.Object(store.Values{"_key": store.String("split-kind"), "kind": store.String("target"), "label": store.String("other")}),
						store.Object(store.Values{"_key": store.String("split-label"), "kind": store.String("other"), "label": store.String("target")}),
					),
					store.List(
						store.Object(store.Values{"_key": store.String("hero-heading"), "blockType": store.String("hero"), "heading": store.String("target"), "tone": store.String("other")}),
						store.Object(store.Values{"_key": store.String("hero-tone"), "blockType": store.String("hero"), "heading": store.String("other"), "tone": store.String("target")}),
						store.Object(store.Values{"_key": store.String("quote-decoy"), "blockType": store.String("quote"), "heading": store.String("decoy")}),
					),
					store.List(store.String("alpha"), store.String("beta")),
				),
			},
			{
				id: id(2),
				values: withRepeatedValues(recordValues("public", 2),
					store.List(store.Object(store.Values{"_key": store.String("same-row"), "kind": store.String("target"), "label": store.String("target")})),
					store.List(store.Object(store.Values{"_key": store.String("same-block"), "blockType": store.String("hero"), "heading": store.String("target"), "tone": store.String("target")})),
					store.List(store.String("beta")),
				),
			},
			{
				id: id(3),
				values: withRepeatedValues(recordValues("public", 3),
					store.List(store.Object(store.Values{"_key": store.String("kind-only"), "kind": store.String("target"), "label": store.String("other")})),
					store.List(store.Object(store.Values{"_key": store.String("heading-only"), "blockType": store.String("hero"), "heading": store.String("target"), "tone": store.String("other")})),
					store.List(store.String("alpha")),
				),
			},
			{id: id(4), values: recordValues("public", 4)},
		} {
			if _, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{Collection: fixture.records, ID: item.id, Values: item.values})); err != nil {
				rollback(t, transaction)
				t.Fatal(err)
			}
		}
		commit(t, transaction)

		rowKind := mustPath("rows.kind")
		rowLabel := mustPath("rows.label")
		rowAnd, err := query.And(
			query.Equal(rowKind, query.String("target")),
			query.Equal(rowLabel, query.String("target")),
		)
		if err != nil {
			t.Fatal(err)
		}
		rowOr, err := query.Or(
			query.Equal(rowKind, query.String("target")),
			query.Equal(rowLabel, query.String("target")),
		)
		if err != nil {
			t.Fatal(err)
		}
		rowNot, err := query.Not(rowAnd)
		if err != nil {
			t.Fatal(err)
		}
		blockAnd, err := query.And(
			query.Equal(mustPath("layout.hero.heading"), query.String("target")),
			query.Equal(mustPath("layout.hero.tone"), query.String("target")),
		)
		if err != nil {
			t.Fatal(err)
		}
		tags := mustPath("tags")
		tagsExist, err := query.Compare(tags, query.OperatorExists, query.Boolean(true))
		if err != nil {
			t.Fatal(err)
		}
		tagsMissing, err := query.Compare(tags, query.OperatorExists, query.Boolean(false))
		if err != nil {
			t.Fatal(err)
		}

		for _, test := range []struct {
			name   string
			filter *query.Node
			access *query.Node
			want   []string
		}{
			{name: "and-may-match-different-rows", filter: nodePointer(rowAnd.Node()), want: []string{id(1), id(2)}},
			{name: "or-combines-document-results", filter: nodePointer(rowOr.Node()), want: []string{id(1), id(2), id(3)}},
			{name: "not-negates-the-document-result", filter: nodePointer(rowNot.Node()), want: []string{id(3), id(4)}},
			{name: "access-uses-the-same-cross-row-semantics", access: nodePointer(rowAnd.Node()), want: []string{id(1), id(2)}},
			{name: "block-siblings-may-match-different-blocks", filter: nodePointer(blockAnd.Node()), want: []string{id(1), id(2)}},
			{name: "block-discriminator-stays-on-the-comparison-row", filter: nodePointer(query.Equal(mustPath("layout.hero.heading"), query.String("decoy")).Node()), want: []string{}},
			{name: "has-many-select-contains-is-membership", filter: nodePointer(query.Contains(tags, "alpha").Node()), want: []string{id(1), id(3)}},
			{name: "has-many-select-equal-is-not-membership", filter: nodePointer(query.Equal(tags, query.String("alpha")).Node()), want: []string{}},
			{name: "has-many-select-in-is-not-membership", filter: nodePointer(query.In(tags, query.String("alpha")).Node()), want: []string{}},
			{name: "has-many-select-null-equality-matches-missing", filter: nodePointer(query.Equal(tags, query.Null()).Node()), want: []string{id(4)}},
			{name: "has-many-select-including-null-matches-missing", filter: nodePointer(query.In(tags, query.String("alpha"), query.Null()).Node()), want: []string{id(4)}},
			{name: "has-many-select-not-equal-compares-the-list-value", filter: nodePointer(query.NotEqual(tags, query.String("alpha")).Node()), want: []string{id(1), id(2), id(3), id(4)}},
			{name: "has-many-select-not-equal-null-requires-a-list", filter: nodePointer(query.NotEqual(tags, query.Null()).Node()), want: []string{id(1), id(2), id(3)}},
			{name: "has-many-select-exists-requires-a-list", filter: nodePointer(tagsExist.Node()), want: []string{id(1), id(2), id(3)}},
			{name: "has-many-select-exists-false-matches-missing", filter: nodePointer(tagsMissing.Node()), want: []string{id(4)}},
		} {
			t.Run(test.name, func(t *testing.T) {
				read := begin(t, fixture.backend)
				page, listErr := read.List(t.Context(), fixture.request(store.Request{
					Collection: fixture.records, Collections: fixture.collections,
					Filter: test.filter, Access: test.access, Page: 1, Limit: 10,
				}))
				rollback(t, read)
				if listErr != nil {
					t.Fatal(listErr)
				}
				assertDocumentIDs(t, page.Documents, test.want...)
			})
		}
	})

	runTest("commit-and-rollback", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		committed := begin(t, fixture.backend)
		if _, err := committed.Create(t.Context(), fixture.createRequest(store.CreateRequest{
			Collection: fixture.records, ID: id(1), Values: recordValues("public", 1),
		})); err != nil {
			rollback(t, committed)
			t.Fatal(err)
		}
		commit(t, committed)

		rolledBack := begin(t, fixture.backend)
		if _, err := rolledBack.Create(t.Context(), fixture.createRequest(store.CreateRequest{
			Collection: fixture.records, ID: id(2), Values: recordValues("public", 2),
		})); err != nil {
			rollback(t, rolledBack)
			t.Fatal(err)
		}
		rollback(t, rolledBack)

		transaction := begin(t, fixture.backend)
		if _, err := transaction.Find(t.Context(), fixture.request(store.Request{Collection: fixture.records, ID: id(1)})); err != nil {
			rollback(t, transaction)
			t.Fatalf("committed document: %v", err)
		}
		if _, err := transaction.Find(t.Context(), fixture.request(store.Request{Collection: fixture.records, ID: id(2)})); !errors.Is(err, store.ErrNotFound) {
			rollback(t, transaction)
			t.Fatalf("rolled-back document error = %v, want not found", err)
		}
		rollback(t, transaction)
	})

	runTest("update-replacement-is-explicit-and-exact", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		initial := recordValues("current", 1)
		initial["optional"] = store.String("remove me")
		initial["rows"] = store.List(store.Object(store.Values{
			"_key": store.String("row-1"), "kind": store.String("article"), "label": store.String("remove me"),
		}))
		initial["localized"] = store.Object(store.Values{
			"en": store.String("Current"), "fr": store.String("Courant"),
		})

		transaction := begin(t, fixture.backend)
		created, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{
			Collection: fixture.records, ID: id(1), Values: initial, Status: store.StatusPublished,
		}))
		if err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		patched, err := transaction.Update(t.Context(), fixture.updateRequest(store.UpdateRequest{
			Request: store.Request{Collection: fixture.records, ID: created.ID, ExpectedRevision: created.Revision},
			Values:  store.Values{"state": store.String("patched")},
		}))
		if err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		if optional, valid := patched.Values["optional"].StringValue(); !valid || optional != "remove me" {
			rollback(t, transaction)
			t.Fatalf("ordinary patch removed omitted value: %#v", patched.Values)
		}

		snapshot := recordValues("snapshot", 1)
		snapshot["rows"] = store.List(store.Object(store.Values{
			"_key": store.String("row-1"), "kind": store.String("article"),
		}))
		snapshot["localized"] = store.Object(store.Values{"en": store.String("Snapshot")})
		replaced, err := transaction.Update(t.Context(), fixture.updateRequest(store.UpdateRequest{
			Request:       store.Request{Collection: fixture.records, ID: created.ID, ExpectedRevision: patched.Revision},
			Values:        snapshot,
			ReplaceValues: true,
		}))
		if err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		if optional, exists := replaced.Values["optional"]; exists && optional.Kind() != store.ValueNull {
			rollback(t, transaction)
			t.Fatalf("replacement retained omitted top-level value: %#v", replaced.Values)
		}
		rows, valid := replaced.Values["rows"].CopyList()
		if !valid || len(rows) != 1 {
			rollback(t, transaction)
			t.Fatalf("replacement rows = %#v", replaced.Values["rows"])
		}
		row, valid := rows[0].CopyObject()
		if !valid {
			rollback(t, transaction)
			t.Fatalf("replacement row = %#v", rows[0])
		}
		if label, exists := row["label"]; exists && label.Kind() != store.ValueNull {
			rollback(t, transaction)
			t.Fatalf("replacement retained omitted nested value: %#v", row)
		}
		localized, valid := replaced.Values["localized"].CopyObject()
		english, englishValid := localized["en"].StringValue()
		if !valid || !englishValid || english != "Snapshot" {
			rollback(t, transaction)
			t.Fatalf("replacement localized value = %#v", replaced.Values["localized"])
		}
		if french, exists := localized["fr"]; exists && french.Kind() != store.ValueNull {
			rollback(t, transaction)
			t.Fatalf("replacement retained omitted locale: %#v", localized)
		}
		commit(t, transaction)
	})

	runTest("snapshot-is-read-only", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		snapshots, ok := fixture.backend.(store.SnapshotStore)
		if !ok {
			t.Fatal("store does not implement store.SnapshotStore")
		}
		transaction, err := snapshots.BeginSnapshot(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{
			Collection: fixture.records, ID: id(1), Values: recordValues("public", 1),
		})); err == nil {
			rollback(t, transaction)
			t.Fatal("read snapshot accepted a document mutation")
		}
		rollback(t, transaction)
	})

	runTest("document-id-validation", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		transaction := begin(t, fixture.backend)
		if _, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{
			Collection: fixture.records, ID: "unsafe\x00id", Values: recordValues("public", 1),
		})); err == nil {
			rollback(t, transaction)
			t.Fatal("store accepted a document ID containing NUL")
		}
		rollback(t, transaction)
	})

	runTest("unique-values", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		transaction := begin(t, fixture.backend)
		first := recordValues("public", 1)
		first["slug"] = store.String("one-slug")
		if _, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{Collection: fixture.records, ID: id(1), Values: first})); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		commit(t, transaction)

		duplicate := begin(t, fixture.backend)
		values := recordValues("public", 2)
		values["slug"] = store.String("one-slug")
		if _, err := duplicate.Create(t.Context(), fixture.createRequest(store.CreateRequest{Collection: fixture.records, ID: id(2), Values: values})); !errors.Is(err, store.ErrConflict) {
			rollback(t, duplicate)
			t.Fatalf("duplicate unique value error = %v, want conflict", err)
		}
		rollback(t, duplicate)

		nulls := begin(t, fixture.backend)
		for _, documentID := range []string{id(3), id(4)} {
			if _, err := nulls.Create(t.Context(), fixture.createRequest(store.CreateRequest{
				Collection: fixture.records, ID: documentID, Values: recordValues("public", 3),
			})); err != nil {
				rollback(t, nulls)
				t.Fatalf("missing unique value for %s: %v", documentID, err)
			}
		}
		commit(t, nulls)
	})

	runTest("reference-restrict-and-nullify", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		target := store.DocumentReference{CollectionID: fixture.users.ID, DocumentID: id(1)}
		transaction := begin(t, fixture.backend)
		if _, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{
			Collection: fixture.users, ID: target.DocumentID, Values: store.Values{"email": store.String("target@example.test")},
		})); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		for _, item := range []struct {
			id     string
			field  string
			values store.Values
		}{
			{id: id(2), field: "restrictedUser", values: recordValues("public", 2)},
			{id: id(3), field: "nullableUser", values: recordValues("public", 3)},
		} {
			item.values[item.field] = store.String(target.DocumentID)
			if _, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{Collection: fixture.records, ID: item.id, Values: item.values})); err != nil {
				rollback(t, transaction)
				t.Fatal(err)
			}
		}
		commit(t, transaction)

		blocked := begin(t, fixture.backend)
		err := blocked.ApplyReferenceDelete(t.Context(), store.ReferenceDeleteRequest{Target: target, Collections: fixture.collections})
		if !errors.Is(err, store.ErrDeleteRestricted) {
			rollback(t, blocked)
			t.Fatalf("restricted delete error = %v, want delete restricted", err)
		}
		unchanged, findError := blocked.Find(t.Context(), fixture.request(store.Request{Collection: fixture.records, ID: id(3)}))
		if findError != nil {
			rollback(t, blocked)
			t.Fatal(findError)
		}
		if value, valid := unchanged.Values["nullableUser"].StringValue(); !valid || value != target.DocumentID {
			rollback(t, blocked)
			t.Fatalf("nullify ran before restrict planning: %#v", unchanged.Values["nullableUser"])
		}
		rollback(t, blocked)

		removeRestriction := begin(t, fixture.backend)
		if _, err := removeRestriction.Delete(t.Context(), fixture.request(store.Request{Collection: fixture.records, ID: id(2)})); err != nil {
			rollback(t, removeRestriction)
			t.Fatal(err)
		}
		commit(t, removeRestriction)

		allowed := begin(t, fixture.backend)
		if err := allowed.ApplyReferenceDelete(t.Context(), store.ReferenceDeleteRequest{Target: target, Collections: fixture.collections}); err != nil {
			rollback(t, allowed)
			t.Fatal(err)
		}
		nullified, err := allowed.Find(t.Context(), fixture.request(store.Request{Collection: fixture.records, ID: id(3)}))
		if err != nil {
			rollback(t, allowed)
			t.Fatal(err)
		}
		if value, exists := nullified.Values["nullableUser"]; exists && value.Kind() != store.ValueNull {
			rollback(t, allowed)
			t.Fatalf("nullified reference = %#v, want null or omission", value)
		}
		if _, err := allowed.Delete(t.Context(), fixture.request(store.Request{Collection: fixture.users, ID: target.DocumentID})); err != nil {
			rollback(t, allowed)
			t.Fatal(err)
		}
		if err := allowed.DeleteDocumentState(t.Context(), target); err != nil {
			rollback(t, allowed)
			t.Fatal(err)
		}
		commit(t, allowed)
	})

	runTest("versions", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		transaction := begin(t, fixture.backend)
		created, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{
			Collection: fixture.records, ID: id(1), Status: store.StatusPublished,
			Values: recordValues("first", 1),
		}))
		if err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		versions := requireVersionTransaction(t, transaction)
		if _, err := versions.SaveVersion(t.Context(), fixture.records, created, 10); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		updated, err := transaction.Update(t.Context(), fixture.updateRequest(store.UpdateRequest{
			Request: store.Request{Collection: fixture.records, ID: created.ID, ExpectedRevision: created.Revision},
			Values:  store.Values{"state": store.String("second")},
		}))
		if err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		if _, err := versions.SaveVersion(t.Context(), fixture.records, updated, 10); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		commit(t, transaction)

		read := begin(t, fixture.backend)
		versionRead := requireVersionTransaction(t, read)
		items, err := versionRead.ListVersions(t.Context(), fixture.versionRequest(store.VersionRequest{Collection: fixture.records, DocumentID: created.ID}))
		if err != nil {
			rollback(t, read)
			t.Fatal(err)
		}
		if len(items) != 2 || items[0].Revision != updated.Revision || items[1].Revision != created.Revision {
			rollback(t, read)
			t.Fatalf("versions = %#v, want newest revision then original", items)
		}
		first, err := versionRead.FindVersion(t.Context(), fixture.records, created.ID, created.Revision)
		state, valid := first.Snapshot.Values["state"].StringValue()
		if err != nil || !valid || state != "first" {
			rollback(t, read)
			t.Fatalf("first version = %#v, %v", first, err)
		}
		rollback(t, read)
	})

	runTest("auth-session-state", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		passwordHash := []byte("conformance-password-hash")
		transaction := begin(t, fixture.backend)
		user, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{
			Collection: fixture.users, ID: id(1), Values: store.Values{"email": store.String("user@example.test")},
		}))
		if err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		credentials, ok := transaction.(store.AuthTransaction)
		if !ok {
			rollback(t, transaction)
			t.Fatal("transaction does not implement store.AuthTransaction")
		}
		if err := credentials.CreateAuthCredential(t.Context(), fixture.users, user.ID, passwordHash, true); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		commit(t, transaction)

		auth := requireAuthStore(t, fixture.backend)
		credential, err := auth.FindAuthCredential(t.Context(), fixture.users, "user@example.test")
		if err != nil || credential.User.ID != user.ID || !credential.Verified {
			t.Fatalf("credential = %#v, %v", credential, err)
		}
		now := conformanceTime()
		session := store.AuthSession{
			ID: id(20), TokenHash: "session-hash", CollectionID: fixture.users.ID, UserID: user.ID,
			ExpiresAt: now.Add(time.Hour), CreatedAt: now, LastSeenAt: now,
		}
		if err := auth.CreateSession(t.Context(), session, passwordHash); err != nil {
			t.Fatal(err)
		}
		found, err := auth.FindSession(t.Context(), session.TokenHash, now)
		if err != nil || found.ID != session.ID || found.UserID != user.ID {
			t.Fatalf("session = %#v, %v", found, err)
		}
		wrong := session
		wrong.ID = id(21)
		wrong.TokenHash = "wrong-password-session"
		if err := auth.CreateSession(t.Context(), wrong, []byte("wrong")); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("wrong-password session error = %v, want conflict", err)
		}
		if err := auth.DeleteSession(t.Context(), session.TokenHash); err != nil {
			t.Fatal(err)
		}
		if _, err := auth.FindSession(t.Context(), session.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleted session error = %v, want not found", err)
		}
	})

	runTest("permanent-delete-cleans-framework-state", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		passwordHash := []byte("cleanup-password-hash")
		transaction := begin(t, fixture.backend)
		user, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{
			Collection: fixture.users, ID: id(1), Status: store.StatusPublished,
			Values: store.Values{"email": store.String("cleanup@example.test")},
		}))
		if err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		if err := transaction.(store.AuthTransaction).CreateAuthCredential(t.Context(), fixture.users, user.ID, passwordHash, true); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		if _, err := requireVersionTransaction(t, transaction).SaveVersion(t.Context(), fixture.users, user, 10); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		record, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{
			Collection: fixture.records, ID: id(2), Values: recordValues("public", 2),
		}))
		if err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
		commit(t, transaction)

		now := conformanceTime()
		session := store.AuthSession{
			ID: id(20), TokenHash: "cleanup-session", CollectionID: fixture.users.ID, UserID: user.ID,
			ExpiresAt: now.Add(time.Hour), CreatedAt: now, LastSeenAt: now,
		}
		auth := requireAuthStore(t, fixture.backend)
		if err := auth.CreateSession(t.Context(), session, passwordHash); err != nil {
			t.Fatal(err)
		}
		preferences, ok := fixture.backend.(store.PreferenceStore)
		if !ok {
			t.Fatal("store does not implement store.PreferenceStore")
		}
		if _, err := preferences.SetPreference(t.Context(), store.Preference{
			CollectionID: fixture.users.ID, UserID: user.ID, Key: "theme", Value: json.RawMessage(`"dark"`),
		}); err != nil {
			t.Fatal(err)
		}
		locks, ok := fixture.backend.(store.DocumentLockStore)
		if !ok {
			t.Fatal("store does not implement store.DocumentLockStore")
		}
		lock := store.DocumentLock{
			CollectionID: fixture.records.ID, DocumentID: record.ID,
			OwnerCollectionID: fixture.users.ID, OwnerID: user.ID, OwnerLabel: "User",
			CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour),
		}
		if _, acquired, err := locks.AcquireDocumentLock(t.Context(), lock, now, false); err != nil || !acquired {
			t.Fatalf("acquire lock = %t, %v", acquired, err)
		}
		tasks, ok := fixture.backend.(store.TaskStore)
		if !ok {
			t.Fatal("store does not implement store.TaskStore")
		}
		task, err := tasks.EnqueueTask(t.Context(), store.Task{
			Slug: "conformance-cleanup", Queue: "default", Input: json.RawMessage(`{}`), RunAt: now.Add(time.Hour),
			MaxAttempts: 1, RetryDelay: time.Second, MaxRetryDelay: time.Second, Backoff: store.TaskBackoffFixed,
			Timeout: time.Minute, Retention: time.Hour,
			Target:      &store.DocumentReference{CollectionID: fixture.records.ID, DocumentID: record.ID},
			RequestedBy: &store.DocumentReference{CollectionID: fixture.users.ID, DocumentID: user.ID},
		})
		if err != nil {
			t.Fatal(err)
		}

		cleanup := begin(t, fixture.backend)
		if _, err := cleanup.Delete(t.Context(), fixture.request(store.Request{Collection: fixture.users, ID: user.ID})); err != nil {
			rollback(t, cleanup)
			t.Fatal(err)
		}
		reference := store.DocumentReference{CollectionID: fixture.users.ID, DocumentID: user.ID}
		if err := cleanup.DeleteDocumentState(t.Context(), reference); err != nil {
			rollback(t, cleanup)
			t.Fatal(err)
		}
		commit(t, cleanup)

		if _, err := auth.FindAuthCredential(t.Context(), fixture.users, "cleanup@example.test"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("credential after cleanup = %v, want not found", err)
		}
		if _, err := auth.FindSession(t.Context(), session.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("session after cleanup = %v, want not found", err)
		}
		if _, err := preferences.GetPreference(t.Context(), fixture.users.ID, user.ID, "theme"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("preference after cleanup = %v, want not found", err)
		}
		if _, err := locks.FindDocumentLock(t.Context(), fixture.records.ID, record.ID, now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("lock after cleanup = %v, want not found", err)
		}
		if _, err := tasks.FindTask(t.Context(), task.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("task after cleanup = %v, want not found", err)
		}
		read := begin(t, fixture.backend)
		if _, err := requireVersionTransaction(t, read).FindVersion(t.Context(), fixture.users, user.ID, user.Revision); !errors.Is(err, store.ErrNotFound) {
			rollback(t, read)
			t.Fatalf("version after cleanup = %v, want not found", err)
		}
		rollback(t, read)
	})

	runTest("system-timestamp-filter-access-and-sort", func(t *testing.T) {
		fixture := openFixture(t, factory, manifest)
		base := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
		transaction := begin(t, fixture.backend)
		for _, item := range []struct {
			id        string
			createdAt time.Time
			updatedAt time.Time
		}{
			{id: id(1), createdAt: base, updatedAt: base.Add(48 * time.Hour)},
			{id: id(2), createdAt: base.Add(24 * time.Hour), updatedAt: base.Add(24 * time.Hour)},
			{id: id(3), createdAt: base.Add(48 * time.Hour), updatedAt: base},
		} {
			if _, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{
				Collection: fixture.records, ID: item.id, Values: recordValues("public", 1),
				CreatedAt: item.createdAt, UpdatedAt: item.updatedAt,
			})); err != nil {
				rollback(t, transaction)
				t.Fatal(err)
			}
		}
		commit(t, transaction)

		createdAt := mustPath("createdAt")
		updatedAt := mustPath("updatedAt")
		filter := query.GreaterThanEqual(createdAt, query.String("2026-01-02T13:00:00+01:00")).Node()
		access := query.GreaterThanEqual(updatedAt, query.String(base.Add(24*time.Hour).Format(time.RFC3339Nano))).Node()
		read := begin(t, fixture.backend)
		page, err := read.List(t.Context(), fixture.request(store.Request{
			Collection: fixture.records, Collections: fixture.collections,
			Filter: &filter, Access: &access, Page: 1, Limit: 10,
		}))
		rollback(t, read)
		if err != nil {
			t.Fatal(err)
		}
		assertDocumentIDs(t, page.Documents, id(2))

		invalidAccess := query.NotEqual(createdAt, query.Number(0)).Node()
		read = begin(t, fixture.backend)
		page, err = read.List(t.Context(), fixture.request(store.Request{
			Collection: fixture.records, Collections: fixture.collections,
			Access: &invalidAccess, Page: 1, Limit: 10,
		}))
		rollback(t, read)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 0 || len(page.Documents) != 0 {
			t.Fatalf("invalid timestamp access matched documents: %#v", page)
		}

		for _, test := range []struct {
			name      string
			path      query.Path
			direction query.Direction
			want      []string
		}{
			{name: "created-descending", path: createdAt, direction: query.Descending, want: []string{id(3), id(2), id(1)}},
			{name: "updated-descending", path: updatedAt, direction: query.Descending, want: []string{id(1), id(2), id(3)}},
		} {
			t.Run(test.name, func(t *testing.T) {
				sortTerm, sortError := query.NewSort(test.path, test.direction)
				if sortError != nil {
					t.Fatal(sortError)
				}
				read := begin(t, fixture.backend)
				page, listError := read.List(t.Context(), fixture.request(store.Request{
					Collection: fixture.records, Collections: fixture.collections,
					Page: 1, Limit: 10, Sort: []query.Sort{sortTerm},
				}))
				rollback(t, read)
				if listError != nil {
					t.Fatal(listError)
				}
				assertDocumentIDs(t, page.Documents, test.want...)
			})
		}
	})
}

type fixture struct {
	backend     store.Store
	users       schema.Collection
	records     schema.Collection
	windows     schema.Collection
	collections map[schema.StableID]schema.Collection
	locales     []schema.LocaleCode
}

type recordFixture struct {
	id    string
	state string
	rank  float64
}

func openFixture(t *testing.T, factory Factory, manifest schema.Manifest) fixture {
	t.Helper()
	backend := factory(t, manifest)
	if backend == nil {
		t.Fatal("store conformance factory returned nil")
	}
	snapshot := manifest.Snapshot()
	result := fixture{backend: backend, collections: make(map[schema.StableID]schema.Collection)}
	if snapshot.Application.Localization != nil {
		result.locales = snapshot.Application.Localization.LocaleCodes()
	}
	for _, collection := range snapshot.Collections {
		result.collections[collection.ID] = collection
		switch collection.Slug {
		case "users":
			result.users = collection
		case "records":
			result.records = collection
		case "windows":
			result.windows = collection
		}
	}
	if result.users.ID == "" || result.records.ID == "" || result.windows.ID == "" {
		t.Fatal("store conformance manifest is incomplete")
	}
	return result
}

func (fixture fixture) request(request store.Request) store.Request {
	request.Locales = fixture.localeOrder()
	return request
}

func (fixture fixture) createRequest(request store.CreateRequest) store.CreateRequest {
	request.Locales = fixture.localeOrder()
	return request
}

func (fixture fixture) updateRequest(request store.UpdateRequest) store.UpdateRequest {
	request.Request = fixture.request(request.Request)
	return request
}

func (fixture fixture) distinctRequest(request store.DistinctRequest) store.DistinctRequest {
	request.Locales = fixture.localeOrder()
	return request
}

func (fixture fixture) versionRequest(request store.VersionRequest) store.VersionRequest {
	request.Locales = fixture.localeOrder()
	return request
}

func (fixture fixture) localeOrder() []schema.LocaleCode {
	return append([]schema.LocaleCode(nil), fixture.locales...)
}

func conformanceManifest() schema.Manifest {
	users := schema.Collection{
		ID: "conformance-users", Slug: "users",
		Labels:       schema.CollectionLabels{Singular: "User", Plural: "Users"},
		Capabilities: schema.Capabilities{Auth: true, Versions: true},
		Auth: &schema.AuthSettings{
			IdentityField: "email", SessionDurationSeconds: 3600,
			PasswordMinLength: 1, PasswordMaxBytes: 128, PasswordBcryptCost: 4,
		},
		Versions: &schema.VersionSettings{Drafts: true, MaxPerDocument: 10},
		Fields: []schema.Field{
			textField("conformance-users-email", "email", true),
		},
	}
	records := schema.Collection{
		ID: "conformance-records", Slug: "records",
		Labels:       schema.CollectionLabels{Singular: "Record", Plural: "Records"},
		Capabilities: schema.Capabilities{Versions: true, Locking: true},
		Versions:     &schema.VersionSettings{Drafts: true, MaxPerDocument: 10},
		DocumentLock: &schema.DocumentLockSettings{DurationSeconds: 300},
		Fields: append([]schema.Field{
			textField("conformance-records-slug", "slug", true),
			textField("conformance-records-state", "state", false),
			localizedTextField("conformance-records-localized", "localized"),
			numberField("conformance-records-rank", "rank"),
			checkboxField("conformance-records-flag", "flag"),
			textField("conformance-records-optional", "optional", false),
		}, append(repeatedPredicateFields(),
			relationshipField("conformance-records-restricted-user", "restrictedUser", users, schema.ReferenceDeleteRestrict),
			relationshipField("conformance-records-nullable-user", "nullableUser", users, schema.ReferenceDeleteNullify),
		)...),
	}
	windows := schema.Collection{
		ID: "conformance-windows", Slug: "windows",
		Labels: schema.CollectionLabels{Singular: "Window", Plural: "Windows"},
		Fields: []schema.Field{
			textField("conformance-windows-slug", "slug", true),
		},
	}
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{
			Name: "Store conformance",
			Localization: &schema.LocalizationSettings{
				DefaultLocale: "en",
				Locales:       []schema.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}},
			},
		},
		Collections: []schema.Collection{users, records, windows}, Plugins: []schema.Plugin{},
	})
}

func textField(fieldID schema.StableID, name string, unique bool) schema.Field {
	return schema.Field{
		ID: fieldID, Name: name, Path: mustPath(name), Type: schema.FieldTypeText,
		Category: schema.FieldCategoryScalar, Unique: unique, Index: unique, Text: &schema.TextField{},
		Admin: schema.FieldAdmin{Label: name},
	}
}

func localizedTextField(fieldID schema.StableID, name string) schema.Field {
	field := textField(fieldID, name, false)
	field.Localized = true
	return field
}

func numberField(fieldID schema.StableID, name string) schema.Field {
	return schema.Field{
		ID: fieldID, Name: name, Path: mustPath(name), Type: schema.FieldTypeNumber,
		Category: schema.FieldCategoryScalar, Index: true, Number: &schema.NumberField{},
		Admin: schema.FieldAdmin{Label: name},
	}
}

func checkboxField(fieldID schema.StableID, name string) schema.Field {
	return schema.Field{
		ID: fieldID, Name: name, Path: mustPath(name), Type: schema.FieldTypeCheckbox,
		Category: schema.FieldCategoryScalar, Index: true,
		Admin: schema.FieldAdmin{Label: name},
	}
}

func relationshipField(fieldID schema.StableID, name string, target schema.Collection, action schema.ReferenceDeleteAction) schema.Field {
	return schema.Field{
		ID: fieldID, Name: name, Path: mustPath(name), Type: schema.FieldTypeRelationship,
		Category: schema.FieldCategoryRelationship, Admin: schema.FieldAdmin{Label: name},
		Relationship: &schema.RelationshipField{
			CollectionID: target.ID, CollectionSlug: target.Slug, OnDelete: action,
		},
	}
}

func repeatedPredicateFields() []schema.Field {
	return []schema.Field{
		{
			ID: "conformance-records-tags", Name: "tags", Path: mustPath("tags"),
			Type: schema.FieldTypeSelect, Category: schema.FieldCategoryScalar,
			Select: &schema.SelectField{HasMany: true, Options: []schema.SelectOption{
				{Value: "alpha", Label: "Alpha"}, {Value: "beta", Label: "Beta"},
			}},
			Admin: schema.FieldAdmin{Label: "tags"},
		},
		{
			ID: "conformance-records-rows", Name: "rows", Path: mustPath("rows"),
			Type: schema.FieldTypeArray, Category: schema.FieldCategoryNested,
			Nested: &schema.NestedField{Fields: []schema.Field{
				{ID: "conformance-records-rows-kind", Name: "kind", Path: mustPath("rows.kind"), Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}, Admin: schema.FieldAdmin{Label: "kind"}},
				{ID: "conformance-records-rows-label", Name: "label", Path: mustPath("rows.label"), Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}, Admin: schema.FieldAdmin{Label: "label"}},
			}},
			Admin: schema.FieldAdmin{Label: "rows"},
		},
		{
			ID: "conformance-records-layout", Name: "layout", Path: mustPath("layout"),
			Type: schema.FieldTypeBlocks, Category: schema.FieldCategoryNested,
			Blocks: &schema.BlocksField{Types: []schema.BlockType{
				{Slug: "hero", Labels: schema.BlockLabels{Singular: "Hero"}, Fields: []schema.Field{
					{ID: "conformance-records-layout-hero-heading", Name: "heading", Path: mustPath("layout.hero.heading"), Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}, Admin: schema.FieldAdmin{Label: "heading"}},
					{ID: "conformance-records-layout-hero-tone", Name: "tone", Path: mustPath("layout.hero.tone"), Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}, Admin: schema.FieldAdmin{Label: "tone"}},
				}},
				{Slug: "quote", Labels: schema.BlockLabels{Singular: "Quote"}, Fields: []schema.Field{
					{ID: "conformance-records-layout-quote-heading", Name: "heading", Path: mustPath("layout.quote.heading"), Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}, Admin: schema.FieldAdmin{Label: "heading"}},
				}},
			}},
			Admin: schema.FieldAdmin{Label: "layout"},
		},
	}
}

func createRecords(t *testing.T, fixture fixture, records []recordFixture) {
	t.Helper()
	transaction := begin(t, fixture.backend)
	for _, record := range records {
		if _, err := transaction.Create(t.Context(), fixture.createRequest(store.CreateRequest{
			Collection: fixture.records, ID: record.id, Values: recordValues(record.state, record.rank),
		})); err != nil {
			rollback(t, transaction)
			t.Fatal(err)
		}
	}
	commit(t, transaction)
}

func recordValues(state string, rank float64) store.Values {
	return store.Values{"state": store.String(state), "rank": store.Number(rank)}
}

func withOptional(values store.Values, value store.Value) store.Values {
	values["optional"] = value
	return values
}

func withRepeatedValues(values store.Values, rows, layout, tags store.Value) store.Values {
	values["rows"] = rows
	values["layout"] = layout
	values["tags"] = tags
	return values
}

func nodePointer(node query.Node) *query.Node {
	return &node
}

func begin(t *testing.T, backend store.Store) store.Transaction {
	t.Helper()
	transaction, err := backend.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return transaction
}

func commit(t *testing.T, transaction store.Transaction) {
	t.Helper()
	if err := transaction.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func rollback(t *testing.T, transaction store.Transaction) {
	t.Helper()
	if err := transaction.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func requireVersionTransaction(t *testing.T, transaction store.Transaction) store.VersionTransaction {
	t.Helper()
	versions, ok := transaction.(store.VersionTransaction)
	if !ok {
		t.Fatal("transaction does not implement store.VersionTransaction")
	}
	return versions
}

func requireAuthStore(t *testing.T, backend store.Store) store.AuthStore {
	t.Helper()
	auth, ok := backend.(store.AuthStore)
	if !ok {
		t.Fatal("store does not implement store.AuthStore")
	}
	return auth
}

func assertDocumentIDs(t *testing.T, documents []store.Document, want ...string) {
	t.Helper()
	if len(documents) != len(want) {
		t.Fatalf("document IDs = %v, want %v", documentIDs(documents), want)
	}
	for index := range want {
		if documents[index].ID != want[index] {
			t.Fatalf("document IDs = %v, want %v", documentIDs(documents), want)
		}
	}
}

func documentIDs(documents []store.Document) []string {
	result := make([]string, len(documents))
	for index := range documents {
		result[index] = documents[index].ID
	}
	return result
}

func mustPath(value string) query.Path {
	path, err := query.ParsePath(value)
	if err != nil {
		panic(fmt.Sprintf("invalid conformance path %q: %v", value, err))
	}
	return path
}

func id(value int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", value)
}

func conformanceTime() time.Time {
	return time.Date(2026, time.August, 29, 12, 0, 0, 0, time.UTC)
}
