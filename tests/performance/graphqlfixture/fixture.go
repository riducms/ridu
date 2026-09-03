// Package graphqlfixture provides the representative schema used by the
// optional-GraphQL memory comparison. It deliberately does not import the
// GraphQL plugin so the baseline binary proves Go linker exclusion.
package graphqlfixture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
)

type Memory struct {
	HeapAlloc   uint64 `json:"heapAlloc"`
	HeapObjects uint64 `json:"heapObjects"`
	Sys         uint64 `json:"sys"`
	Requests    int    `json:"requests"`
}

// Run constructs 39 collections and 28 block definitions, optionally binds a
// transport plugin, executes requests, releases unused pages, and prints live
// runtime memory as one JSON object.
func Run(plugin ridu.Plugin, requests int) error {
	config := Config()
	if plugin != nil {
		config.Plugins = []ridu.Plugin{plugin}
	}
	application, err := ridu.New(config, teststore.New())
	if err != nil {
		return err
	}
	handler := application.Handler(ridu.HandlerOptions{})
	for index := 0; index < requests; index++ {
		body, _ := json.Marshal(map[string]interface{}{"query": `{ Items00(limit: 1) { totalDocs } }`})
		request := httptest.NewRequest(http.MethodPost, "/api/graphql", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			return fmt.Errorf("GraphQL request %d returned %d: %s", index, response.Code, response.Body.String())
		}
	}
	runtime.GC()
	debug.FreeOSMemory()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	runtime.KeepAlive(handler)
	return json.NewEncoder(stdout{}).Encode(Memory{HeapAlloc: stats.HeapAlloc, HeapObjects: stats.HeapObjects, Sys: stats.Sys, Requests: requests})
}

// stdout keeps the fixture free from loggers and other process-global state.
type stdout struct{}

func (stdout) Write(value []byte) (int, error) { return fmt.Print(string(value)) }

func Config() ridu.Config {
	collections := make([]ridu.Collection, 39)
	blocks := make([]field.Block, 28)
	for index := range blocks {
		blocks[index] = field.BlockType(fmt.Sprintf("block-%02d", index), fmt.Sprintf("Block %02d", index),
			field.Text("heading"), field.Text("body"), field.Number("weight"),
		)
	}
	for index := range collections {
		slug := fmt.Sprintf("items-%02d", index)
		fields := []field.Definition{field.Text("title", field.Required()), field.Text("summary"), field.Number("rank"), field.Checkbox("featured")}
		if index == 0 {
			fields = append(fields, field.Blocks("layout", field.BlockTypes(blocks...)))
		} else {
			fields = append(fields, field.Relationship("parent", field.To("items-00")))
		}
		collections[index] = ridu.Collection{
			Slug: schema.CollectionSlug(slug), Labels: ridu.CollectionLabels{Singular: fmt.Sprintf("Item%02d", index), Plural: fmt.Sprintf("Items%02d", index)}, Fields: fields,
		}
	}
	return ridu.Config{Name: "GraphQL memory fixture", Collections: collections}
}
