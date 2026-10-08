// Command editor_server serves the posts that the SvelteKit editor fixture
// (tests/contracts/editor_app) loads, edits and saves through its generated client.
package main

import (
	"log"
	"net"
	"net/http"
	"os"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/plugins/richtext"
)

// config is the application the fixture's generated client describes.
func config() ridu.Config {
	return ridu.Config{
		Name:    "Editor app contract",
		Plugins: []ridu.Plugin{richtext.New()},
		Collections: []ridu.Collection{{
			Slug: "posts",
			Fields: field.Fields{
				field.Text("title").Required(),
				richtext.Field("body", richtext.Config{
					Features: []richtext.Feature{richtext.FeatureLinks, richtext.FeatureLists},
				}),
			},
		}},
	}
}

func main() {
	application, err := ridu.New(config(), teststore.New())
	if err != nil {
		log.Fatal(err)
	}
	address := os.Getenv("RIDU_EDITOR_SERVER_ADDRESS")
	if address == "" {
		address = "127.0.0.1:4182"
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Handler: application.Handler(ridu.HandlerOptions{})}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
