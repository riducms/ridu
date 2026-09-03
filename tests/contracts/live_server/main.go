package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
)

func main() {
	ctx := context.Background()
	config := ridu.Config{
		Name:  "Live SDK contract",
		Admin: ridu.AdminConfig{User: "users"},
		Endpoints: []ridu.Endpoint{
			{
				Method: http.MethodPost, Path: "/custom/:value", Summary: "Exercise the raw SDK transport",
				Handler: func(ctx ridu.EndpointContext) {
					ctx.Writer.Header().Set("Content-Type", "application/json")
					ctx.Writer.WriteHeader(http.StatusAccepted)
					_ = json.NewEncoder(ctx.Writer).Encode(map[string]string{
						"method": ctx.Request.Method,
						"value":  ctx.RouteParams["value"],
					})
				},
			},
			{
				Method: http.MethodGet, Path: "/whoami", Summary: "Exercise custom endpoint identity",
				Handler: func(ctx ridu.EndpointContext) {
					if ctx.Actor == nil {
						ctx.Writer.WriteHeader(http.StatusUnauthorized)
						return
					}
					ctx.Writer.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(ctx.Writer).Encode(map[string]string{
						"actorId":    ctx.Actor.ID,
						"collection": string(ctx.ActorCollection),
					})
				},
			},
		},
		Collections: []ridu.Collection{
			{Slug: "users", Auth: true, Fields: []field.Definition{field.Text("email", field.Required(), field.Unique())}},
			{Slug: "authors", Fields: []field.Definition{
				field.Text("name", field.Required()),
			}},
			{
				Slug: "posts",
				Fields: []field.Definition{
					field.Text("title", field.Required()),
					field.Select("status", field.Default("draft"), field.Choices(
						field.Choice{Value: "draft", Label: "Draft"},
						field.Choice{Value: "published", Label: "Published"},
					)),
					field.Relationship("author", field.To("authors")),
					field.Group("seo", field.Fields(
						field.Text("description"),
					)),
				},
				Endpoints: []ridu.Endpoint{{
					Method: http.MethodGet, Path: "/custom-summary", Summary: "Read the collection endpoint scope",
					Handler: func(ctx ridu.EndpointContext) {
						ctx.Writer.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(ctx.Writer).Encode(map[string]string{
							"collection": string(ctx.Collection),
						})
					},
				}},
			},
		},
	}
	backend := store.Store(teststore.New())
	closeBackend := func() error { return nil }
	if databasePath := os.Getenv("RIDU_SQLITE_PATH"); databasePath != "" {
		sqliteBackend, err := sqlite.Open(ctx, databasePath)
		if err != nil {
			log.Fatal(err)
		}
		manifest, err := ridu.Resolve(config)
		if err != nil {
			_ = sqliteBackend.Close()
			log.Fatal(err)
		}
		if err := sqliteBackend.Migrate(ctx, manifest); err != nil {
			_ = sqliteBackend.Close()
			log.Fatal(err)
		}
		backend = sqliteBackend
		closeBackend = sqliteBackend.Close
	}
	defer func() { _ = closeBackend() }()
	application, err := ridu.New(config, backend)
	if err != nil {
		log.Fatal(err)
	}
	user, err := application.Local().Create(ctx, "users", store.Values{"email": store.String("live@riducms.test")}, nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := application.SetPassword(ctx, "users", user.ID, "live-password"); err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("http://%s\n", listener.Addr())
	server := &http.Server{Handler: application.Handler(ridu.HandlerOptions{})}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
