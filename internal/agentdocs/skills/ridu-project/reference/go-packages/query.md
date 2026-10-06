<!-- Generated from website/src/content/docs/go-packages/query.md by scripts/sync-agent-docs.ts. -->

# Filters and paths

Use the `query` package to describe which documents you want, such as “products that cost at most
50 and are in stock”. Pass the filter to the [local Go API](../local-api.md) to read matching
documents, or return it from an [access rule](../access-control.md) to limit what a user can see.
For the `where` syntax used by REST and the TypeScript SDK, see [Querying data](../querying.md).

A filter is a `query.Expression`. You build one by naming a field and giving an ordinary Go value
to compare it with:

| Part               | What it is                | Example                 |
| ------------------ | ------------------------- | ----------------------- |
| Field name         | The field to check        | `"price"`               |
| Value              | The value to compare with | `50`                    |
| `query.Expression` | The resulting condition   | `price` is at most `50` |

## Build a filter {#build-filter}

This function builds a filter for in-stock products within a budget:

```go title="content/product_filters.go" focus={6-10}
package content

import "github.com/riducms/ridu/query"

func AffordableProducts(maxPrice float64) query.Expression {
	// Describe both requirements; this does not read the database yet.
	return query.And(
		query.LessThanEqual("price", maxPrice),
		query.Equal("inStock", true),
	)
}
```

Each comparison takes a field name and a value, and `query.And` requires both conditions. Building
the expression does not read the database: it is a value you can store, pass around, and reuse.
[Run a filter](#run-filter) executes it.

## query.Path {#paths}

A `query.Path` names one field. In filters you write in code, pass the field's name as a string,
joining group and field names with a dot: `"title"`, or `"seo.title"` for the `title` field in the
`seo` group. Every comparison helper, and `query.Asc` and `query.Desc`, converts the name for you.

Build a `query.Path` yourself only when a name is not written in your code, or where an option
takes paths:

| To name                            | Write                          |
| ---------------------------------- | ------------------------------ |
| A dotted name you received as text | `query.ParsePath("seo.title")` |
| Field names computed at run time   | `query.NewPath(group, name)`   |
| A field for `ListOptions.Select`   | `query.Field("seo", "title")`  |

A malformed name written in code, such as `"SEO.title"`, panics, like `regexp.MustCompile`. For
names that come from input, use `ParsePath` or `NewPath`, which return an error instead.

A name is checked against your schema only when the operation runs. `query.Equal("unknown", "x")`
succeeds, but a read that uses it fails with `bad_query` if the collection has no such field.
Fields with read access rules cannot be used in caller filters; see
[protected field queries](../access-control.md#protected-field-queries).

### Groups, arrays, and blocks {#nested-paths}

| Structure                                   | Path                  | What it checks                   |
| ------------------------------------------- | --------------------- | -------------------------------- |
| A `seo` group                               | `seo.title`           | The group's title                |
| A `variants` array                          | `variants.sku`        | The SKU in each row of the array |
| A `layout` blocks field with a `hero` block | `layout.hero.heading` | The heading in each Hero block   |

A path never contains a row index. A comparison on `variants.sku` matches a document when **any**
row matches, and `NotEqual` excludes a document when any row equals the value. Two comparisons
joined with `And` may be satisfied by different rows: there is no way yet to require the same row
to match both.

## query.Value {#values}

Pass the value you compare with as an ordinary Go value of the kind the field holds:

| To compare with              | Write                          |
| ---------------------------- | ------------------------------ |
| Text, or a relationship's ID | `"notebook"`, `ctx.Actor.ID`   |
| A number                     | `50`, `price`, `2.5`           |
| A checkbox value             | `true`                         |
| A select option              | `"published"`, or a named type |
| A date and time              | `query.DateTime(time.Now())`   |
| A date only                  | `"2026-09-29"`                 |
| A polymorphic reference      | `query.Reference("posts", id)` |
| Null                         | `query.Null()`                 |

Named types work too, so a `type Status string` constant can be compared with a select field
directly. `"50"` is text and `50` is a number, so choose the one that matches the field.

Dates are stored as text, so a comparison must use the same form the field stores.
`query.DateTime` converts a `time.Time` to that form for date-and-time fields, `createdAt`, and
`updatedAt`:

```go
recent := query.GreaterThan(
	"publishedAt",
	query.DateTime(time.Now().AddDate(0, 0, -7)),
)
```

Compare a date-only field with a `"2026-09-29"` string, such as `t.Format(time.DateOnly)`.

A number is stored as a 64-bit float, which holds integers exactly only up to 2^53. Larger
integers panic instead of silently losing precision; compare them as strings.

A polymorphic relationship can point into several collections, so its candidates name the
collection as well as the ID: `query.In("subjects", query.Reference("posts", postID))`. Lists,
has-many fields and polymorphic relationships take `query.In` only; see
[Filter by membership](../querying/filters.md#membership).

A `query.Value` is the typed form the helpers build internally. Build one with `query.String`,
`query.Number`, `query.Boolean`, `query.Null`, `query.Reference`, or `query.List` when you call
`query.Compare` or need null. The similarly named `store.Number(50)` is a document value for
saving content, not a comparison value.

## Comparisons {#operators}

Each helper takes a field name and a value, and returns a `query.Expression`:

| Matches documents where the field…               | Example                                                   |
| ------------------------------------------------ | --------------------------------------------------------- |
| Equals the value                                 | `query.Equal("status", "published")`                      |
| Does not equal the value                         | `query.NotEqual("status", "draft")`                       |
| Equals one of several values, or holds one item  | `query.In("color", "red", "blue")`                        |
| Is greater than, or at least, a number or string | `query.GreaterThan("price", 10)`, `GreaterThanEqual`      |
| Is less than, or at most, a number or string     | `query.LessThan("price", 50)`, `LessThanEqual`            |
| Contains the text, ignoring case                 | `query.Contains("title", "ridu")`                         |
| Contains every word of the text, ignoring case   | `query.Like("title", "go cms")`                           |
| Has a value, or has none                         | `query.Exists("image", true)`, `query.Exists(..., false)` |

To pass a list you built, spread it: `query.In("color", colors...)`.

The helpers expect a valid name and a matching value, and panic otherwise. That is fine for
filters written in your code. When the operator or value comes from user input, use
[`query.Compare`](https://riducms.com/reference/query/compare/), which returns an error instead. The
[operator table](../querying/filters.md) shows the matching REST and TypeScript names.

## And, Or, and Not {#combine-filters}

`query.And` matches documents that satisfy every condition, `query.Or` matches any, and
`query.Not` inverts one:

```go
visible := query.Or(
	query.Equal("status", "published"),
	query.Equal("author", userID),
)
```

To add optional conditions, extend the filter as you go. `query.And(filter, next)` merges into the
existing `And` instead of nesting it, so building a filter this way stays cheap:

```go
filter := query.Equal("group", groupID)
if kind != "" {
	filter = query.And(filter, query.Equal("kind", kind))
}
```

With a single condition, `And` and `Or` return that condition, so you can pass a list whose
length varies. Like the comparison helpers, they panic when given no conditions or a `nil` one;
check the length of a list built from input before calling them.

## Run a filter {#run-filter}

Pass the expression as `ridu.ListOptions.Where`:

```go title="content/find_products.go" focus={15-20}
package content

import (
	"context"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/store"
)

func FindAffordableProducts(
	ctx context.Context,
	local *ridu.LocalAPI,
	maxPrice float64,
) (store.Page, error) {
	// List executes the filter and also applies collection read access.
	return local.List(ctx, "products", ridu.ListOptions{
		Where: AffordableProducts(maxPrice),
		Page:  1,
		Limit: 20,
	})
}
```

`FindAffordableProducts(ctx, app.Local(), 50)` returns visible, in-stock products that cost at
most 50. Ridu applies your filter and the collection's access rules together, before pagination.

This example reads anonymously because it passes no actor. To read as a signed-in user, set
`Actor` and `ActorCollection` in the options; see [passing the caller](../local-api.md#actors).

### Typed collection handles {#generated-collections}

Generated Go collection handles return your application's document types instead of
`store.Values`. Their `ridu.TypedListOptions` take the same `query.Expression` in `Where`, so
`generated.ProductsCollection.With(app.Local()).List(...)` uses the filters on this page
unchanged.

## Use a filter in an access rule {#access-filters}

An access rule can return [`ridu.Where`](https://riducms.com/reference/ridu/where/) with a filter to allow access to
matching documents only. This collection allows reads of visible products:

```go title="content/products.go" focus={10-11,22}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
)

func visibleProducts(ridu.AccessContext) (ridu.AccessDecision, error) {
	// This filter limits every read, including reads with other filters.
	return ridu.Where(query.Equal("visible", true)), nil
}

var Products = ridu.Collection{
	Slug: "products",
	Fields: field.Fields{
		field.Text("title").Required(),
		field.Number("price").Required().Min(0),
		field.Checkbox("inStock").Default(false),
		field.Checkbox("visible").Default(false),
	},
	Access: ridu.CollectionAccess{Read: visibleProducts},
}
```

The rule applies to reads from the admin, REST, the SDK, and the local API, and it filters the
database query itself, so counts and pagination only include visible products.

Two different things are called “where”:

- `ridu.ListOptions{Where: filter}` asks for matching documents. Access rules still apply.
- `ridu.Where(filter)` is an access rule's decision to allow matching documents.

## Sort results {#result-options}

A filter chooses documents; the list options choose their order and page. Sort with `query.Asc`
and `query.Desc`; later terms break ties in earlier ones:

```go
return local.List(ctx, "products", ridu.ListOptions{
	Where: filter,
	Sort: []query.Sort{
		query.Asc("price"),
		query.Desc("createdAt"),
	},
	Limit: 20,
})
```

When the direction comes from input, use `query.NewSort`, which returns an error for an unknown
direction. Some fields, such as arrays and blocks, cannot be sorted. See [sorting](../querying/sorting-and-pagination.md#sort) and [pagination](../querying/sorting-and-pagination.md#paginate) for
limits, and the [`query` reference](https://riducms.com/reference/query/) for every function.
