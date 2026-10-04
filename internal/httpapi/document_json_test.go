package httpapi

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func decodedDocument(t *testing.T, encoded json.RawMessage) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode document %s: %v", encoded, err)
	}
	return document
}

// The wire encoder replaced a map that encoding/json serialized. Responses
// keep those bytes for valid text: key order, HTML-safe escaping, number
// formatting, UTC timestamps and populated-document metadata.
func TestDocumentJSONMatchesEncodingJSONOfTheDocumentMap(t *testing.T) {
	created := time.Date(2026, 9, 30, 18, 4, 5, 123456789, time.FixedZone("BST", 3600))
	deleted := created.Add(time.Hour)
	author := store.Document{
		ID: "user_1", CreatedAt: created, UpdatedAt: created, DeletedAt: &deleted, Revision: 2,
		LocalizationSources: map[string]schema.LocaleCode{"name": "en", "bio": "fr"},
		Values:              store.Values{"name": store.String("Ada <admin> & co"), "bio": store.Null()},
	}
	documents := map[string]store.Document{
		"metadata only":        {ID: "empty", CreatedAt: created, UpdatedAt: created},
		"pending draft":        {ID: "pending", CreatedAt: created, UpdatedAt: created, Status: store.StatusPublished, Revision: 3, PublishedRevision: 2, HasDraftChanges: true},
		"aligned working head": {ID: "aligned", CreatedAt: created, UpdatedAt: created, Status: store.StatusPublished, Revision: 2, PublishedRevision: 2},
		"every value kind": {
			ID: "post_1", CreatedAt: created, UpdatedAt: created.Add(time.Second), Status: store.StatusDraft, Revision: 7,
			LocalizationSources: map[string]schema.LocaleCode{"title": "fr", "seo.title": "en"},
			Values: store.Values{
				"title":    store.String("<script>alert(\"x\")</script> & \u2028\u2029 \x00 café 🎉"),
				"views":    store.Number(1e21),
				"ratio":    store.Number(0.000001234),
				"negative": store.Number(-0),
				"featured": store.Boolean(true),
				"missing":  store.Null(),
				"zero":     {},
				"tags":     store.List(store.String("b"), store.String("a"), store.Null()),
				"seo":      store.Object(store.Values{"z": store.Number(1), "a": store.Object(store.Values{"Ä": store.Boolean(false), "a": store.String("é")})}),
				"author":   store.Populated(author),
				"related":  store.List(store.Object(store.Values{"relationTo": store.String("users"), "value": store.Populated(author)})),
			},
		},
		"fields replace metadata": {ID: "meta", CreatedAt: created, UpdatedAt: created, Status: store.StatusPublished, Values: store.Values{"id": store.String("field id"), "_status": store.Number(3)}},
	}
	for name, document := range documents {
		t.Run(name, func(t *testing.T) {
			want := encodedResponse(t, protocol.DocumentEnvelope[map[string]any]{Doc: documentMap(document)})
			got := encodedResponse(t, protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(document)})
			if !bytes.Equal(got, want) {
				t.Fatalf("wire document\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

// store escapes invalid UTF-8 as \ufffd where encoding/json now writes the
// replacement character itself. Both decode to the same text.
func TestDocumentJSONReplacesInvalidUTF8(t *testing.T) {
	document := store.Document{ID: "bytes", Values: store.Values{"title": store.String("a\xffb")}}
	encoded := documentJSON(document)
	if !bytes.Contains(encoded, []byte(`"title":"a\ufffdb"`)) || decodedDocument(t, encoded)["title"] != "a\uFFFDb" {
		t.Fatalf("invalid UTF-8 = %s", encoded)
	}
}

func TestDocumentJSONFailsTheResponseForValuesEncodingJSONRejects(t *testing.T) {
	document := store.Document{ID: "nan", Values: store.Values{"score": store.Number(math.NaN())}}
	var old, current bytes.Buffer
	if json.NewEncoder(&old).Encode(protocol.DocumentEnvelope[map[string]any]{Doc: documentMap(document)}) == nil {
		t.Fatal("encoding/json accepted NaN")
	}
	if err := json.NewEncoder(&current).Encode(protocol.DocumentEnvelope[json.RawMessage]{Doc: documentJSON(document)}); err == nil || current.Len() != old.Len() {
		t.Fatalf("response encode error = %v, body = %q, previous body = %q", err, current.Bytes(), old.Bytes())
	}
}

func encodedResponse(t *testing.T, value any) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := json.NewEncoder(&buffer).Encode(value); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// documentMap is the map shape responses previously gave encoding/json.
func documentMap(document store.Document) map[string]any {
	result := map[string]any{
		"id":        document.ID,
		"createdAt": document.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updatedAt": document.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if document.DeletedAt != nil {
		result["deletedAt"] = document.DeletedAt.UTC().Format(time.RFC3339Nano)
	}
	if document.Status != "" {
		result["_status"] = document.Status
	}
	if document.Revision > 0 {
		result["_revision"] = document.Revision
	}
	if document.PublishedRevision > 0 {
		result["_publishedRevision"] = document.PublishedRevision
		result["_hasDraftChanges"] = document.HasDraftChanges
	}
	if len(document.LocalizationSources) > 0 {
		sources := map[string]string{}
		for path, locale := range document.LocalizationSources {
			sources[path] = string(locale)
		}
		result["_localization"] = map[string]any{"sources": sources}
	}
	for name, value := range document.Values {
		result[name] = valueMap(value)
	}
	return result
}

func valueMap(value store.Value) any {
	switch value.Kind() {
	case store.ValueString:
		text, _ := value.StringValue()
		return text
	case store.ValueNumber:
		number, _ := value.NumberValue()
		return number
	case store.ValueBoolean:
		boolean, _ := value.BooleanValue()
		return boolean
	case store.ValueObject:
		result := map[string]any{}
		for name, child := range value.Entries() {
			result[name] = valueMap(child)
		}
		return result
	case store.ValueList:
		result := []any{}
		for child := range value.Elements() {
			result = append(result, valueMap(child))
		}
		return result
	case store.ValueDocument:
		document, _ := value.CopyDocument()
		return documentMap(document)
	default:
		return nil
	}
}
