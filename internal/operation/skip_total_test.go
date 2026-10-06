package operation

import (
	"errors"
	"testing"

	"github.com/riducms/ridu/internal/teststore"
	operationkind "github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestSkipTotalIsAnUncountedCollectionListRead(t *testing.T) {
	titlePath, _ := query.NewPath("title")
	title := schema.Field{ID: "posts-title", Name: "title", Path: titlePath, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar}
	visible := query.NotEqual(titlePath, "hidden").Node()
	engine, err := New(Config{Store: teststore.New(), Collections: []Collection{{
		Schema: schema.Collection{ID: "collection-posts", Slug: "posts", Fields: []schema.Field{title}},
		Access: map[operationkind.Kind]Access{operationkind.Read: func(Context) (Decision, error) {
			return Decision{Kind: Where, Access: &visible}, nil
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "hidden"} {
		if _, err := engine.Execute(t.Context(), Request{
			Operation: operationkind.Create, Collection: "posts", ImportID: id, Data: store.Values{"title": store.String(id)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	sort := []query.Sort{{Path: titlePath, Direction: query.Ascending}}
	for page, want := range []struct {
		id   string
		next bool
	}{{id: "a", next: true}, {id: "b"}} {
		result, err := engine.Execute(t.Context(), Request{
			Operation: operationkind.Read, Collection: "posts", Page: page + 1, Limit: 1, Sort: sort, SkipTotal: true, IncludeAccess: true,
		})
		if err != nil || result.Page == nil {
			t.Fatalf("uncounted page %d = %#v, %v", page+1, result, err)
		}
		if result.Page.Total != nil || result.Page.HasNextPage != want.next || len(result.Page.Documents) != 1 || result.Page.Documents[0].ID != want.id {
			t.Fatalf("uncounted page %d = %#v", page+1, result.Page)
		}
		if result.PageAccess == nil || len(result.PageAccess.Documents) != 1 {
			t.Fatalf("uncounted page %d access = %#v", page+1, result.PageAccess)
		}
	}
	for _, request := range []Request{
		{Operation: operationkind.Read, Collection: "posts", ID: "a", SkipTotal: true},
		{Operation: operationkind.Create, Collection: "posts", Data: store.Values{"title": store.String("c")}, SkipTotal: true},
	} {
		_, err := engine.Execute(t.Context(), request)
		var operationError *Error
		if !errors.As(err, &operationError) || operationError.Code != "bad_operation" || operationError.Status != 400 {
			t.Fatalf("%s with SkipTotal = %v, want bad_operation", request.Operation, err)
		}
	}
}
