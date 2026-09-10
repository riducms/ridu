// Package richtextblocks runs the same rich-text block acceptance contract on
// real adapters. The fixture contains no alternative lifecycle implementation.
package richtextblocks

import (
	"errors"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/examples/blocks/content"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/store"
)

// Factory must provide isolated, migrated storage and register its cleanup.
type Factory func(*testing.T, ridu.Config) (store.Store, *ridu.App)

type observations struct {
	rollback    bool
	occurrences []operation.OccurrenceID
	original    map[string]string
}

func configuration(t *testing.T, seen *observations) ridu.Config {
	t.Helper()
	config := content.Config()
	storage, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config.Storage, config.StorageNamespace = storage, "richtext-blocks"
	config.Admin.User = "users"
	config.Collections = append(config.Collections,
		ridu.Collection{Slug: "users", Auth: true, Fields: field.Fields{
			field.Email("email").Required().Unique(),
		}},
		ridu.Collection{Slug: "files", Upload: true, UploadConfig: ridu.UploadConfig{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}}},
		ridu.Collection{Slug: "audit-events", Fields: field.Fields{
			field.Text("message"),
		}},
	)
	cta := field.Block{Slug: "cta", Labels: field.BlockLabels{Singular: "CTA"}, Fields: field.Fields{
		field.Text("label").Required(),
		field.Relationship("destination", "pages"),
	}}
	callout := field.Block{Admin: field.BlockAdmin{RowLabelPath: "title"}, Slug: "callout", Fields: field.Fields{
		field.Text("title").Required().Validate(func(_ operation.ValidationContext, value operation.Value[string]) ([]operation.Issue, error) {
			if text, _ := value.Get(); text == "invalid" {
				return []operation.Issue{{Code: "callout_title", Message: "Use a descriptive title"}}, nil
			}
			return nil, nil
		}).Hooks(field.Hooks[string]{BeforeChange: []field.Transform[string]{func(ctx operation.WriteContext, _ operation.Value[string]) (operation.Change[string], error) {
			seen.occurrences = append(seen.occurrences, ctx.OccurrenceID)
			if seen.original != nil {
				key, _ := ctx.Siblings.String("_key")
				seen.original[key], _ = ctx.Prior.String("title")
			}
			if seen.rollback {
				return operation.Keep[string](), errors.New("deliberate embedded hook rollback")
			}
			return operation.Keep[string](), nil
		}}}),
		field.Text("caption").Default("A helpful note"),
		field.Textarea("translation").Localized(),
		field.Text("secret").Access(field.Access{Read: func(operation.AccessContext) (bool, error) { return false, nil }}),
		field.Group("appearance", field.Fields{
			field.Text("tone").Default("neutral"),
		}),
		field.Array("links", field.Fields{
			field.Text("label").Required(),
			field.Text("href"),
		}),
		field.Relationship("target", "assets"),
		field.Relationship("locked", "assets").OnDelete(field.ReferenceDeleteRestrict),
		field.Upload("asset", "files"),
		field.Upload("lockedAsset", "files").OnDelete(field.ReferenceDeleteRestrict),
		richtext.Field("detail", richtext.Config{Blocks: []field.Block{cta}}),
		richtext.Field("aside", richtext.Config{Blocks: []field.Block{cta}}),
	}}

	for i, collection := range config.Collections {
		if collection.Slug != "articles" {
			continue
		}
		collection.Admin.LivePreview = ridu.LivePreviewConfig{URL: "https://preview.example.test/articles/{id}"}
		collection.VersionConfig.MaxPerDocument = 30
		collection.Fields = field.Fields{
			field.Text("title").Required(),
			richtext.Field("body", richtext.Config{Blocks: []field.Block{callout, cta, content.Media}}),
			richtext.Field("localizedBody", richtext.Config{Blocks: []field.Block{callout, cta}}).Localized(),
		}
		collection.Hooks.BeforeChange = append(collection.Hooks.BeforeChange, func(ctx ridu.HookContext) error {
			if seen.rollback {
				_, err := ctx.Local.Create(ctx.Context, "audit-events", store.Values{"message": store.String("must roll back")}, nil)
				return err
			}
			return nil
		})
		config.Collections[i] = collection
	}
	return config
}

// Document constructs the portable supported JSON envelope; tests use raw
// store values deliberately to challenge API validation independently of types.
func Document(nodes ...store.Value) store.Value {
	return store.Object(store.Values{"version": store.Number(1), "root": store.Object(store.Values{
		"type": store.String("root"), "version": store.Number(1), "format": store.String(""), "indent": store.Number(0), "direction": store.Null(), "children": store.List(nodes...),
	})})
}

func Block(kind, key string, values store.Values) store.Value {
	payload := store.Values{"blockType": store.String(kind)}
	if key != "" {
		payload["_key"] = store.String(key)
	}
	for name, value := range values {
		payload[name] = value
	}
	return store.Object(store.Values{"type": store.String("block"), "version": store.Number(1), "fields": store.Object(payload)})
}

func paragraph(value string) store.Value {
	return store.Object(store.Values{"type": store.String("paragraph"), "version": store.Number(1), "format": store.String(""), "indent": store.Number(0), "direction": store.Null(), "textFormat": store.Number(0), "textStyle": store.String(""), "children": store.List(store.Object(store.Values{
		"type": store.String("text"), "version": store.Number(1), "text": store.String(value), "detail": store.Number(0), "format": store.Number(0), "mode": store.String("normal"), "style": store.String(""),
	}))})
}

func payload(t *testing.T, document store.Value, index int) store.Values {
	t.Helper()
	value, ok := document.CopyObject()
	if !ok {
		t.Fatal("missing document object")
	}
	root, _ := value["root"].CopyObject()
	nodes, _ := root["children"].CopyList()
	if index >= len(nodes) {
		t.Fatalf("missing node %d", index)
	}
	node, _ := nodes[index].CopyObject()
	fields, ok := node["fields"].CopyObject()
	if !ok {
		t.Fatalf("node %d is not a block", index)
	}
	return fields
}

func stringValue(value store.Value) string { text, _ := value.StringValue(); return text }

func issue(t *testing.T, err error, path string) {
	t.Helper()
	var failure *ridu.OperationError
	if !errors.As(err, &failure) {
		t.Fatalf("expected issue at %s: %v", path, err)
	}
	for _, item := range failure.Issues {
		if item.Path == path {
			return
		}
	}
	t.Fatalf("wanted issue %s, got %v: %v", path, failure.Issues, err)
}

func code(t *testing.T, err error, want string) {
	t.Helper()
	var failure *ridu.OperationError
	if !errors.As(err, &failure) || failure.Code != want {
		t.Fatalf("wanted %s: %#v", want, err)
	}
}

func noPrivateContent(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), "never disclose") {
		t.Fatal("error disclosed payload")
	}
}
