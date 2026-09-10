package content

import (
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func TestComparedPostAccess(t *testing.T) {
	for _, ownOnly := range []bool{false, true} {
		name := "signed-in editors"
		if ownOnly {
			name = "post authors"
		}
		t.Run(name, func(t *testing.T) {
			posts := Posts
			if ownOnly {
				posts.Access.Update = ownPosts
				posts.Access.Delete = ownPosts
			}
			app, err := ridu.New(ridu.Config{
				Name:  "Payload comparison",
				Admin: ridu.AdminConfig{User: "users"},
				Collections: []ridu.Collection{
					{Slug: "users", Auth: true, Fields: field.Fields{field.Email("email").Required().Unique()}},
					posts,
				},
			}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			author, err := app.CreateAuthUser(t.Context(), "users", store.Values{"email": store.String("author@example.test")}, "documentation-password", nil)
			if err != nil {
				t.Fatal(err)
			}
			other, err := app.CreateAuthUser(t.Context(), "users", store.Values{"email": store.String("other@example.test")}, "documentation-password", nil)
			if err != nil {
				t.Fatal(err)
			}
			values := store.Values{
				"title":  store.String("Hello, Ridu"),
				"slug":   store.String("hello-ridu"),
				"author": store.String(author.ID),
			}
			if _, err := app.Local().Create(t.Context(), "posts", values, nil); err == nil {
				t.Fatal("anonymous create succeeded")
			}
			created, err := app.Local().Create(t.Context(), "posts", values, &author)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := app.Local().Find(t.Context(), "posts", created.ID, nil); err != nil {
				t.Fatalf("public read: %v", err)
			}
			if _, err := app.Local().Update(t.Context(), "posts", created.ID, store.Values{"summary": store.String("Other user's edit")}, &other); (err != nil) != ownOnly {
				t.Fatalf("other user's update: %v, owner-only=%v", err, ownOnly)
			}
			if _, err := app.Local().Update(t.Context(), "posts", created.ID, store.Values{"summary": store.String("Author's edit")}, &author); err != nil {
				t.Fatalf("author update: %v", err)
			}
			if _, err := app.Local().Delete(t.Context(), "posts", created.ID, &other); (err != nil) != ownOnly {
				t.Fatalf("other user's delete: %v, owner-only=%v", err, ownOnly)
			}
			if ownOnly {
				if _, err := app.Local().Delete(t.Context(), "posts", created.ID, &author); err != nil {
					t.Fatalf("author delete: %v", err)
				}
			}
		})
	}
}
