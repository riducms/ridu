<!-- Generated from website/src/content/docs/go-packages/store.md by scripts/sync-agent-docs.ts. -->

# Documents and values

The `store` package contains the Go types Ridu uses for document data. You use them when calling
the local API, reading a document in a hook, or inspecting another field's value.

Its name comes from document storage, but constructing a `store.Value` does not save anything.
For example, `store.String("Home")` creates a text value in memory. You save it by passing it to
`app.Local().Create(...)` or `app.Local().Update(...)`.

## Which type do I need? {#value-types}

Several packages have a type named `Value`. They serve different purposes:

| Type                 | What it means                                                                                            | When you use it                                                                                                 |
| -------------------- | -------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `store.Value`        | One piece of document data: text, a number, a boolean, null, an object, a list, or a populated document. | `store.String("Home")` supplies a field's value.                                                                |
| `store.Values`       | A Go map from field names to `store.Value`.                                                              | `store.Values{"title": store.String("Home")}` supplies fields to a write.                                       |
| `store.Document`     | One document's ID, timestamps, other metadata, and its `Values` map.                                     | A local API create or read returns this.                                                                        |
| `operation.Value[T]` | A callback value of a known Go type, plus whether a value is present.                                    | A text validator receives `operation.Value[string]`. Calling `Get()` returns the string and a presence boolean. |
| `query.Value`        | A value to compare with a field in a filter.                                                             | `query.String("Home")` is the comparison value in an equality filter.                                           |

`store.String("Home")` and `query.String("Home")` are different Go types: use the former for
content and the latter for filters. `operation.Present("Home")` wraps a typed callback value;
it does not construct a document field. The [callback guide](./operation.md) and
[query guide](./query.md) show those types in context.

## Build and read field values {#read-values}

Choose a constructor that matches your field, then read its value or children:

| Construct a value                 | Read it back                                   | Result                                                           |
| --------------------------------- | ---------------------------------------------- | ---------------------------------------------------------------- |
| `store.String("Home")`            | `StringValue()`                                | A `string` and a type-match boolean.                             |
| `store.Number(4.5)`               | `NumberValue()`                                | A `float64` and a type-match boolean.                            |
| `store.Boolean(false)`            | `BooleanValue()`                               | A `bool` and a type-match boolean.                               |
| `store.Object(store.Values{...})` | `Get("title")`, `Lookup("title")`, `Entries()` | One immutable child value, or an iterator over names and values. |
| `store.List(...)`                 | `ListItem(0)`, `Elements()`                    | One immutable item, or an iterator over items in order.          |
| `store.Null()`                    | `Kind() == store.ValueNull`                    | An explicit null has no inner value.                             |
| `store.Populated(document)`       | `CopyDocument()`                               | A detached `store.Document` and a type-match boolean.            |

Object and list reads share immutable child values without copying their containers. `Len()`
returns the number of object members or list items. It returns zero for other kinds, so use
`Kind()` when the distinction matters. `Entries()` has no guaranteed order; `Elements()` follows
list order. Reading the same value later gives you the same snapshot.

Text, email, date, and singular relationship IDs use string values; the field definition determines
which strings are valid. A number constructor accepts a `float64`, but the field can still reject
it through its constraints. Constructors make data; they do not run field validation.

The accessor's boolean matters. `store.Boolean(false).BooleanValue()` returns `false, true`:
false is a valid checkbox value. Reading a string as a number returns `0, false`; it does not
parse the string or panic.

Save this as `values_test.go` in a Go package and run `go test`. The `Output` comment shows the
results and lets Go check them:

```go title="values_test.go" focus={17-23,25-31}
package content

import (
	"fmt"

	"github.com/riducms/ridu/store"
)

func Example_values() {
	values := store.Values{
		"title":    store.String("Welcome"),
		"featured": store.Boolean(false),
		"summary":  store.Null(),
		"tags":     store.List(),
	}

	title, ok := values["title"].StringValue()
	fmt.Println("title:", title, ok)
	// The bool checks the type, not whether the value is nonzero.
	featured, ok := values["featured"].BooleanValue()
	fmt.Println("featured:", featured, ok)
	_, ok = values["title"].NumberValue()
	fmt.Println("title is a number:", ok)

	// Map membership is a separate check from the value's type.
	_, exists := values["missing"]
	fmt.Println("missing key exists:", exists)
	summary := values["summary"]
	fmt.Println("summary is null:", summary.Kind() == store.ValueNull)
	tags := values["tags"]
	fmt.Println("tags:", tags.Len(), tags.Kind() == store.ValueList)

	// Output:
	// title: Welcome true
	// featured: false true
	// title is a number: false
	// missing key exists: false
	// summary is null: true
	// tags: 0 true
}
```

Use `value, exists := values["title"]` when you need to distinguish an absent map entry from a
present value of the wrong type. The accessor's boolean checks the type; the map's boolean
checks membership.

## Missing, null, and empty are different {#empty-values}

When preparing data for the local API, choose the state you mean:

| Input                                       | Meaning                                                                                                        |
| ------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| Leave a field out of `store.Values`         | Do not supply that field. An update keeps its saved value; a create applies defaults and required-field rules. |
| `store.Null()`                              | Explicitly clear a nullable field. Required fields reject null.                                                |
| `store.String("")`                          | Supply an empty string. It is still a string; a required text field rejects it.                                |
| `store.Number(0)` or `store.Boolean(false)` | Supply an actual zero or false value. These are not missing.                                                   |
| `store.List()`                              | Supply an empty list. On an array update, this removes all rows if the field's requirements allow it.          |
| `store.Object(store.Values{})`              | Supply an empty object. Its field definition determines whether that object is valid.                          |

There is no `store.Missing()` constructor: missing means the map has no entry. Do not substitute
`store.Value{}` for null. Its zero value has no valid kind and cannot be JSON-encoded. Indexing a
missing key without checking membership also gives you this zero value.

These are input distinctions, not a promise that every later callback preserves them. In a raw
field hook, an empty `operation.Value[store.Value]` means omitted input, while
`operation.Present(store.Null())` means explicit null. After defaults and retained update values
have been applied, typed string, number, and boolean callbacks use one empty state for missing
and null. See [missing callback input](../fields/callback-values.md#missing-input).

## Create a document through the local API {#local-api}

This collection has a title, an optional summary, a group, and an array of links. Add `Pages` to
your config's `Collections`, then call `CreatePage` from your Go handler or service. Pass the
caller's `Actor` and `ActorCollection` in `options`; empty options mean an anonymous request.

```go title="pages.go" focus={31-42}
package content

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

var Pages = ridu.Collection{
	Slug: "pages",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Textarea("summary"),
		field.Group("seo", field.Fields{
			field.Text("title"),
		}),
		field.Array("links", field.Fields{
			field.Text("label").Required(),
			field.Text("url").Required(),
		}),
	},
}

func CreatePage(
	ctx context.Context,
	app *ridu.App,
	options ridu.MutationOptions,
) (store.Document, error) {
	return app.Local().Create(ctx, "pages", store.Values{
		"title":   store.String("Home"),
		"summary": store.String("Start here"),
		"seo": store.Object(store.Values{
			"title": store.String("Welcome to Acme"),
		}),
		"links": store.List(store.Object(store.Values{
			// Ridu adds this new row's _key when it saves the page.
			"label": store.String("About"),
			"url":   store.String("/about"),
		})),
	}, options)
}
```

The returned document has `page.ID`, `page.CreatedAt`, and `page.UpdatedAt` metadata. Its title is
in `page.Values["title"]`, not in a Go `Title` property or a top-level `"id"` field in that map.
The link receives a `_key` when saved.

The local API runs access checks, validation, hooks, and transactions. Application code should
use it rather than calling an adapter's `store.Store` methods directly; those methods are the
lower-level database contract and do not run the full application operation.

The separate `storage` package handles uploaded file contents, such as image bytes in a bucket.
It is not the package for constructing document values. See [Uploads](../uploads.md) for files.

## Read and edit nested data {#nested-values}

An object contains another field map; a list contains values in order. Read one level at a time:
`page.Values["seo"].Get("title").StringValue()` reads the group's title without copying the
object. Names are literal keys: `Get("seo.title")` does not walk into the group. `Get` returns
null for a missing child or a non-object value. Use `Lookup` to distinguish a missing child
from a present null.

Read each link without making a slice or copying its row:

```go
for link := range page.Values["links"].Elements() {
	label, _ := link.Get("label").StringValue()
	fmt.Println(label)
}
```

An array is a `store.List` of `store.Object` rows. Blocks use the same shape, with `blockType`
identifying each block's configured type. Keep each existing row's `_key` when editing or
reordering it. Ridu uses the key to recognize the same row after it moves; the row's position
is not its identity. Omit the key for a new row and let Ridu assign it. Keys you supply must be
nonempty strings and unique within their list. Changing a block's type requires a new key.

Use `CopyObject()` when you need a mutable field map, `CopyList()` when you need a mutable slice,
and `CopyDocument()` for a populated document. Editing these detached copies leaves the original
value unchanged. To replace one list item, `WithListItem` builds a new list that shares its
unchanged items. This avoids copying the whole list just to edit one row:

```go title="nested.go" focus={14-25}
package content

import (
	"fmt"

	"github.com/riducms/ridu/store"
)

func RenameFirstLink(
	links store.Value,
	label string,
) (store.Value, error) {
	first, ok := links.ListItem(0)
	if !ok {
		return store.Value{}, fmt.Errorf("links must be a nonempty list")
	}
	row, ok := first.CopyObject()
	if !ok {
		return store.Value{}, fmt.Errorf("first link must be an object")
	}

	// Only this row needs a mutable copy. Preserve its _key and other fields.
	row["label"] = store.String(label)
	updated, _ := links.WithListItem(0, store.Object(row))
	return updated, nil
}
```

`RenameFirstLink` returns a list with the first label changed and every existing row key retained.
The original list is unchanged. It returns an error for a missing, null, empty, or malformed list.

The top-level `store.Values` map follows normal Go map rules: assigning it to another variable
shares that map. Use `store.CloneValues(values)` before editing a separate top-level copy, or
`store.CloneDocument(document)` when you also need copied document metadata. Nested values
remain immutable and can be shared by the original and the edited document.

## Save the edited value {#save-changes}

Changing a returned `page.Values` map only changes your in-memory response. To persist the list
from `RenameFirstLink`, send it through the local API:

```go title="update.go" focus={17-25}
package content

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/store"
)

func RenamePageLink(
	ctx context.Context,
	app *ridu.App,
	page store.Document,
	label string,
	options ridu.MutationOptions,
) (store.Document, error) {
	links, err := RenameFirstLink(page.Values["links"], label)
	if err != nil {
		return store.Document{}, err
	}

	// Send the updated list. Omitted top-level fields stay unchanged.
	return app.Local().Update(ctx, "pages", page.ID,
		store.Values{"links": links}, options,
	)
}
```

After `RenamePageLink` succeeds, the saved first link has the requested label. The title, summary,
and SEO group stay unchanged because the update did not include them.

A supplied array is the new complete list, not a patch for one row: rows you leave out are
removed. Start with the full list, preserve the fields you intend to retain, and send the rebuilt
list. Do not rebuild a complete write from a response that intentionally omitted fields through
selection or access rules. [Array fields](https://riducms.com/docs/fields/array/) and
[Blocks](https://riducms.com/docs/fields/blocks/) explain their update and localization rules.

## Understand the document you receive {#returned-documents}

`store.Document` keeps metadata separate from your fields:

| Property                 | What you read                                                                       |
| ------------------------ | ----------------------------------------------------------------------------------- |
| `ID`                     | The document ID used by find, update, and delete calls.                             |
| `CreatedAt`, `UpdatedAt` | Go `time.Time` timestamps.                                                          |
| `Values`                 | Your field names and their values.                                                  |
| `Status`, `Revision`     | Draft/published state and revision information for versioned content.               |
| `DeletedAt`              | The deletion time for a trashed document, or nil.                                   |
| `LocalizationSources`    | Response metadata identifying the locales that supplied returned translated values. |

A local API response is prepared for that caller. Selected or protected fields may be absent;
read hooks can format values; locale fallback can supply translations. Do not treat its `Values`
as an exact copy of every stored field.

A singular relationship to one collection normally reads as an ID string. If you ask Ridu to populate that relationship,
use `CopyDocument()` to obtain a detached related document and its own `Values`. This is why a relationship's
`StringValue()` may return false after population. `store.Populated(...)` describes that response
shape; write the relationship ID when updating the field.

## Use generated Go structs for known collections {#generated-types}

If your service always works with the same collection, generated handles can remove repeated
map lookups and type checks. Run `ridu generate`, bind the generated collection to `app.Local()`,
and use its create/update structs and returned document structs. Field names become Go properties
such as `Title`, and the compiler checks their types.

Generated input wrappers still distinguish omitted fields from explicit nulls and concrete values.
The generated handle uses the same local API, permissions, validation, and hooks; it does not give
you direct database access. Use `store.Values` when writing generic code across collections or
calling local API features that the typed handle does not expose. See
[generated typed handles](../local-api.md#typed-handles) for binding and input examples.

For allocation-sensitive callbacks, [Hook and value performance](https://riducms.com/docs/performance/hooks-and-values/)
shows when immutable iteration avoids a copy and how repeated copies can grow with embedded data.
