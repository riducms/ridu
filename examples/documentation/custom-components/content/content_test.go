package content

import (
	"context"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestCustomComponentCollections(t *testing.T) {
	app, err := ridu.New(ridu.Config{Name: "Custom component documentation", Collections: []ridu.Collection{Posts, Pages}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	post, err := app.Local().Create(ctx, "posts", store.Values{"title": store.String("An article"), "readingMinutes": store.Number(5)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := post.Values["readingMinutes"].NumberValue(); value != 5 {
		t.Fatalf("reading time = %v", value)
	}
	if _, err := app.Local().Create(ctx, "pages", store.Values{
		"title": store.String("About"),
		"links": store.List(store.Object(store.Values{"label": store.String("About"), "url": store.String("/about")})),
	}, ridu.MutationOptions{}); err != nil {
		t.Fatal(err)
	}
}
