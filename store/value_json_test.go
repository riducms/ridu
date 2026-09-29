package store_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/store"
)

// reference converts a value to the Go data encoding/json handles natively. The
// store codec must encode exactly as encoding/json encodes this data.
func reference(value store.Value) any {
	switch value.Kind() {
	case store.ValueNull:
		return nil
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
		members := map[string]any{}
		for name, member := range value.Entries() {
			members[name] = reference(member)
		}
		return members
	case store.ValueList:
		items := []any{}
		for item := range value.Elements() {
			items = append(items, reference(item))
		}
		return items
	case store.ValueDocument:
		document, _ := value.CopyDocument()
		members := map[string]any{"id": document.ID, "createdAt": document.CreatedAt, "updatedAt": document.UpdatedAt}
		if document.Status != "" {
			members["_status"] = document.Status
		}
		if document.Revision > 0 {
			members["_revision"] = document.Revision
		}
		for name, member := range document.Values {
			members[name] = reference(member)
		}
		return members
	}
	panic("unknown kind")
}

// fromAny builds a value from encoding/json's decoded form, as the previous
// decoder did.
func fromAny(decoded any) store.Value {
	switch typed := decoded.(type) {
	case nil:
		return store.Null()
	case string:
		return store.String(typed)
	case float64:
		return store.Number(typed)
	case bool:
		return store.Boolean(typed)
	case []any:
		items := make([]store.Value, len(typed))
		for index, item := range typed {
			items[index] = fromAny(item)
		}
		return store.List(items...)
	case map[string]any:
		values := store.Values{}
		for name, member := range typed {
			values[name] = fromAny(member)
		}
		return store.Object(values)
	}
	panic(fmt.Sprintf("unexpected %T", decoded))
}

func assertEncodesLikeEncodingJSON(t *testing.T, value store.Value) {
	t.Helper()
	got, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	want, err := json.Marshal(reference(value))
	if err != nil {
		t.Fatalf("reference encode: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("encoded\n%s\nwant\n%s", got, want)
	}
}

// assertDecodesLikeEncodingJSON checks that data is accepted or rejected as
// encoding/json does, and decodes to the same value.
func assertDecodesLikeEncodingJSON(t *testing.T, data []byte) {
	t.Helper()
	var decoded any
	referenceErr := json.Unmarshal(data, &decoded)
	var value store.Value
	// Adapters call the codec directly, without encoding/json's syntax check.
	err := value.UnmarshalJSON(data)
	if (err == nil) != (referenceErr == nil) {
		t.Fatalf("decode %q: error %v, encoding/json error %v", data, err, referenceErr)
	}
	if err != nil {
		return
	}
	got, err := json.Marshal(value)
	if err != nil {
		// Only non-finite numbers fail to encode, and JSON cannot express them.
		t.Fatalf("encode decoded %q: %v", data, err)
	}
	want, _ := json.Marshal(reference(fromAny(decoded)))
	if !bytes.Equal(got, want) {
		t.Fatalf("decode %q:\ngot  %s\nwant %s", data, got, want)
	}
}

func TestValueJSONMatchesEncodingJSONForEdgeCases(t *testing.T) {
	texts := []string{
		"", "plain", `quote " and \ backslash`, "<script>&</script>", "tab\tnew\nline\rcr",
		"\b\f\x00\x01\x1f\x7f", "é ü 日本 🎉", "\u2028\u2029", "bad \xff utf8 \xc3", "\ufffd",
	}
	numbers := []float64{0, math.Copysign(0, -1), 1, -1, 1.5, 0.1, 1e20, 1e21, 1e-6, 1e-7,
		123456789012345, 1234567890123456789, math.MaxFloat64, math.SmallestNonzeroFloat64, -2.5e-10}
	for _, text := range texts {
		assertEncodesLikeEncodingJSON(t, store.String(text))
	}
	for _, number := range numbers {
		assertEncodesLikeEncodingJSON(t, store.Number(number))
	}
	created := time.Date(2026, 9, 29, 14, 5, 0, 123000000, time.UTC)
	assertEncodesLikeEncodingJSON(t, store.Object(store.Values{
		"b": store.Boolean(false), "a": store.Null(), "<key>": store.List(), "empty": store.Object(store.Values{}),
		"nested": store.List(store.Object(store.Values{"z": store.Number(2), "y": store.String("x")}), store.List(store.Boolean(true))),
		"author": store.Populated(store.Document{
			ID: "u1", CreatedAt: created, UpdatedAt: created, Status: store.StatusPublished, Revision: 3,
			Values: store.Values{"name": store.String("Ada"), "id": store.String("overrides the metadata id")},
		}),
	}))
	long := make([]store.Value, 1100)
	for index := range long {
		long[index] = store.Number(float64(index))
	}
	assertEncodesLikeEncodingJSON(t, store.List(long...))

	for _, data := range []string{
		`null`, `true`, `false`, `0`, `-0`, `-0.0`, `1e3`, `1E+3`, `2.50`, `123456789012345`,
		`1234567890123456`, `99999999999999999999`, `1e400`, `-1e400`, `01`, `1.`, `.5`, `+1`, `--1`, `1e`,
		`""`, `"\u00e9\uD83C\uDF89"`, `"\uD800"`, `"\uDC00x"`, `"\uD800\u0041"`, `"\u12"`, `"\x"`, "\"a\x01\"",
		"\"\xff\"", `"\/"`, `[]`, `[1,]`, `[,1]`, `[1 2]`, `{}`, `{"a":1,"a":2}`, `{"a":1,}`, `{a:1}`,
		`{"a" 1}`, ` { "a" : [ 1 , { "b" : null } ] } `, `[1]x`, `nul`, `truex`, `{"a":1}}`, ``, ` `,
	} {
		assertDecodesLikeEncodingJSON(t, []byte(data))
	}
}

func TestValuesJSONDecodesObjectsOnly(t *testing.T) {
	var values store.Values
	if err := json.Unmarshal([]byte(`{"title":"Home","tags":["a"]}`), &values); err != nil {
		t.Fatal(err)
	}
	if title, _ := values["title"].StringValue(); title != "Home" || values["tags"].Len() != 1 {
		t.Fatalf("values = %#v", values)
	}
	// Decoding adds to an existing map and null clears it, as for any Go map.
	if err := json.Unmarshal([]byte(`{"summary":"More"}`), &values); err != nil || len(values) != 3 {
		t.Fatalf("merged values = %#v, error = %v", values, err)
	}
	if err := json.Unmarshal([]byte(`null`), &values); err != nil || values != nil {
		t.Fatalf("null values = %#v, error = %v", values, err)
	}
	for _, data := range []string{`[]`, `"text"`, `1`} {
		var rejected store.Values
		if err := json.Unmarshal([]byte(data), &rejected); err == nil {
			t.Fatalf("decoded %s into values", data)
		}
	}
	if encoded, err := json.Marshal(store.Values(nil)); err != nil || string(encoded) != "null" {
		t.Fatalf("nil values encoded %s, error = %v", encoded, err)
	}
}

func TestValueJSONLimitsNestingLikeEncodingJSON(t *testing.T) {
	for _, depth := range []int{10000, 10001} {
		data := []byte(strings.Repeat("[", depth) + strings.Repeat("]", depth))
		var value store.Value
		err := value.UnmarshalJSON(data)
		var decoded any
		referenceErr := json.Unmarshal(data, &decoded)
		if (err == nil) != (referenceErr == nil) {
			t.Fatalf("depth %d: error %v, encoding/json error %v", depth, err, referenceErr)
		}
	}
}

func FuzzValueJSONMatchesEncodingJSON(f *testing.F) {
	for _, seed := range []string{
		`{"title":"Home","links":[{"_key":"a","label":"<b>"}],"n":1.5e-7}`,
		`[null,true,false,0,-0,"\u2028\ud83c\udf89\ud800"]`, "\"\xff\"", `{"a":{"a":{"a":[]}}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		assertDecodesLikeEncodingJSON(t, data)
		var value store.Value
		if json.Unmarshal(data, &value) == nil {
			assertEncodesLikeEncodingJSON(t, value)
		}
	})
}

func BenchmarkValuesJSON(b *testing.B) {
	var page strings.Builder
	page.WriteString(`{"title":"Home","summary":"Start here","seo":{"title":"Welcome"},"links":[`)
	for index := range 20 {
		if index > 0 {
			page.WriteByte(',')
		}
		fmt.Fprintf(&page, `{"_key":"k%d","label":"Link %d","url":"/l/%d"}`, index, index, index)
	}
	page.WriteString(`],"body":{"root":{"children":[`)
	for index := range 30 {
		if index > 0 {
			page.WriteByte(',')
		}
		fmt.Fprintf(&page, `{"type":"paragraph","children":[{"type":"text","text":"Paragraph %d","format":0},{"type":"link","children":[{"type":"text","text":"link","format":1}]}]}`, index)
	}
	page.WriteString(`]}}}`)
	data := []byte(page.String())
	b.Run("decode", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		b.ReportAllocs()
		for b.Loop() {
			var values store.Values
			if err := json.Unmarshal(data, &values); err != nil {
				b.Fatal(err)
			}
		}
	})
	var values store.Values
	if err := json.Unmarshal(data, &values); err != nil {
		b.Fatal(err)
	}
	b.Run("encode", func(b *testing.B) {
		b.SetBytes(int64(len(data)))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := json.Marshal(values); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkValuesJSONDirect(b *testing.B) {
	data := []byte(`{"title":"Home","summary":"Start here","seo":{"title":"Welcome"},"links":[` + strings.Repeat(`{"_key":"k1","label":"Link 1","url":"/l/1"},`, 19) + `{"_key":"k1","label":"Link 1","url":"/l/1"}],"body":{"root":{"children":[` + strings.Repeat(`{"type":"paragraph","children":[{"type":"text","text":"Paragraph 1","format":0},{"type":"link","children":[{"type":"text","text":"link","format":1}]}]},`, 29) + `{"type":"paragraph","children":[]}]}}}`)
	for _, test := range []struct {
		name   string
		decode func() error
	}{
		{"through encoding/json", func() error { var values store.Values; return json.Unmarshal(data, &values) }},
		{"UnmarshalJSON", func() error { var values store.Values; return values.UnmarshalJSON(data) }},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				if err := test.decode(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
