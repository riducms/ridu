package main

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

// This reset-token-protected fixture redeploys the application schema against
// the same store. It never edits persisted content or adds a production route.
func blockSchemaRecoveryFixture(initial http.Handler, original ridu.Config, backend store.Store, options ridu.HandlerOptions) http.Handler {
	token := os.Getenv("RIDU_BROWSER_RESET_TOKEN")
	if token == "" || os.Getenv("RIDU_BROWSER_BOOTSTRAP") == "true" {
		return initial
	}
	var mu sync.RWMutex
	active := initial
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/__ridu-test/blocks-schema" {
			mu.RLock()
			defer mu.RUnlock()
			active.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !resetTokenMatches(r.Header.Get(resetTokenHeader), token) {
			http.Error(w, "fixture reset authentication failed", http.StatusUnauthorized)
			return
		}
		var input struct {
			RetireHero    bool `json:"retireHero"`
			RetireCallout bool `json:"retireCallout"`
			RenameCallout bool `json:"renameCallout"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		config := original
		config.Collections = append([]ridu.Collection(nil), original.Collections...)
		for i, collection := range config.Collections {
			retireHero := collection.Slug == "pages" && input.RetireHero
			changeCallout := collection.Slug == "block-articles" && (input.RetireCallout || input.RenameCallout)
			if !retireHero && !changeCallout {
				continue
			}
			graph, err := collection.Fields.Edit(func(draft *field.ChildrenDraft) error {
				for _, node := range draft.Fields() {
					if retireHero && node.Name() == "layout" {
						blocks, err := field.AsBlocks(node)
						if err != nil {
							return err
						}
						blocks, err = blocks.EditBlocks(func(variants *[]field.Block) error {
							remaining := make([]field.Block, 0, len(*variants))
							for _, variant := range *variants {
								if variant.Slug != "hero" {
									remaining = append(remaining, variant)
								}
							}
							*variants = remaining
							return nil
						})
						if err != nil {
							return err
						}
						return draft.Replace("layout", blocks)
					}
					if changeCallout && node.Name() == "body" {
						body, err := field.AsPlugin(node)
						if err != nil {
							return err
						}
						trees := field.Snapshot(body).EmbeddedTrees()
						var variants []field.Block
						for _, variant := range trees[0].Cases[0].Types {
							if variant.Slug == "callout" {
								if input.RetireCallout {
									continue
								}
								variant.Slug = "renamed-callout"
							}
							variants = append(variants, variant)
						}
						trees[0].Cases[0].Types = variants
						return draft.Replace("body", body.EmbeddedTrees(trees...))
					}
				}
				return nil
			})
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			collection.Fields = graph
			config.Collections[i] = collection
		}
		application, err := ridu.New(config, backend)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		active = application.Handler(options)
		w.WriteHeader(http.StatusNoContent)
	})
}
