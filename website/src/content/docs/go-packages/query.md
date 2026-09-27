---
title: 'Filters and paths'
description: 'Build Go filters with query paths, comparison values, and conditions, then use them in reads and access rules.'
product: core
eyebrow: 'Go packages'
order: 40
relatedSymbolIds:
  - 'go:github.com/riducms/ridu/query#Path'
  - 'go:github.com/riducms/ridu/query#Expression'
  - 'go:github.com/riducms/ridu/query#Value'
  - 'go:github.com/riducms/ridu/query#NewPath'
  - 'go:github.com/riducms/ridu/query#ParsePath'
  - 'go:github.com/riducms/ridu/query#And'
  - 'go:github.com/riducms/ridu/query#Equal'
aliases:
  [
    'query package',
    'query.Path',
    'query.Expression',
    'query.Value',
    'query.NewPath'
  ]
navigation:
  section: 'Get started'
  parent: go-packages
  order: 30
  title: 'Filters and paths'
---

Use the `query` package to describe which documents you want, such as “products that cost at most
50 and are in stock”. Pass the filter to the [local Go API](/docs/local-api/) to read matching
documents, or return it from an [access rule](/docs/access-control/) to limit what a user can see.
For the `where` syntax used by REST and the TypeScript SDK, see [Querying data](/docs/querying/).

A filter is built from three types:

| Type               | What it is                | Example                 |
| ------------------ | ------------------------- | ----------------------- |
| `query.Path`       | The field to check        | The product's `price`   |
| `query.Value`      | The value to compare with | The number `50`         |
| `query.Expression` | The resulting condition   | `price` is at most `50` |

## Build a filter {#build-filter}

This function builds a filter for in-stock products within a budget:

```go title="content/product_filters.go" focus={15-19}
package content

import "github.com/riducms/ridu/query"

func AffordableProducts(maxPrice float64) (query.Expression, error) {
	price, err := query.NewPath("price")
	if err != nil {
		return nil, err
	}
	inStock, err := query.NewPath("inStock")
	if err != nil {
		return nil, err
	}

	// Describe both requirements; this does not read the database yet.
	return query.And(
		query.LessThanEqual(price, query.Number(maxPrice)),
		query.Equal(inStock, query.Boolean(true)),
	)
}
```

`query.NewPath` names each field, `query.Number` and `query.Boolean` supply the values, and
`query.And` requires both conditions. Building the expression does not read the database: it is a
value you can store, pass around, and reuse. [Run a filter](#run-filter) executes it.

## query.Path {#paths}

A `query.Path` names one field. Build it from field names, or parse a dotted string:

| To name                            | Write                           | Path        |
| ---------------------------------- | ------------------------------- | ----------- |
| A top-level field                  | `query.NewPath("title")`        | `title`     |
| A field inside the `seo` group     | `query.NewPath("seo", "title")` | `seo.title` |
| A dotted path you received as text | `query.ParsePath("seo.title")`  | `seo.title` |

Both functions return an error for a malformed path, such as an empty name.
`query.NewPath("seo.title")` is also an error, because a dot is not part of a field name; pass
the names separately or use `ParsePath`.

A path is checked against your schema only when the operation runs. `query.NewPath("unknown")`
succeeds, but a read that uses it fails if the collection has no such field. Fields with read
access rules cannot be used in caller filters; see
[protected field queries](/docs/access-control/#protected-field-queries).

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

A `query.Value` is the value inside a comparison. Use the constructor for the kind of value the
field holds:

| To compare with              | Write                      |
| ---------------------------- | -------------------------- |
| Text, or a relationship's ID | `query.String("notebook")` |
| A number                     | `query.Number(50)`         |
| A checkbox value             | `query.Boolean(true)`      |
| Null                         | `query.Null()`             |

`query.String("50")` is text and `query.Number(50)` is a number, so choose the one that matches
the field. The similarly named `store.Number(50)` is a document value for saving content, not a
comparison value; the two types are not interchangeable.

## Comparisons {#operators}

Each helper takes a path and a value, and returns a `query.Expression`:

| Matches documents where the field…               | Example                                                          |
| ------------------------------------------------ | ---------------------------------------------------------------- |
| Equals the value                                 | `query.Equal(status, query.String("published"))`                 |
| Does not equal the value                         | `query.NotEqual(status, query.String("draft"))`                  |
| Equals one of several values                     | `query.In(color, query.String("red"), query.String("blue"))`     |
| Is greater than, or at least, a number or string | `query.GreaterThan(price, query.Number(10))`, `GreaterThanEqual` |
| Is less than, or at most, a number or string     | `query.LessThan(price, query.Number(50))`, `LessThanEqual`       |
| Contains the text, ignoring case                 | `query.Contains(title, "ridu")`                                  |
| Contains every word of the text, ignoring case   | `query.Like(title, "go cms")`                                    |

To match a field that has a value, or one that has none, use `query.Compare` with
`query.OperatorExists`:

```go
hasImage, err := query.Compare(image, query.OperatorExists, query.Boolean(true))
```

The helpers expect a valid path and a matching value, and panic otherwise. That is fine for
filters written in your code. When the operator or value comes from user input, use
[`query.Compare`](/reference/query/compare/), which returns an error instead. The
[operator table](/docs/querying/#where) shows the matching REST and TypeScript names.

## And, Or, and Not {#combine-filters}

`query.And` matches documents that satisfy every condition, `query.Or` matches any, and
`query.Not` inverts one:

```go
visible, err := query.Or(
	query.Equal(status, query.String("published")),
	query.Equal(author, query.String(userID)),
)
```

`And` and `Or` need at least two conditions, and `Not` needs exactly one; otherwise they return an
error. To add an optional condition, use it on its own when it is the only one.

## Run a filter {#run-filter}

Pass the expression as `ridu.ListOptions.Where`:

```go title="content/find_products.go" focus={20-25}
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
	filter, err := AffordableProducts(maxPrice)
	if err != nil {
		return store.Page{}, err
	}

	// List executes the filter and also applies collection read access.
	return local.List(ctx, "products", ridu.ListOptions{
		Where: filter,
		Page:  1,
		Limit: 20,
	})
}
```

`FindAffordableProducts(ctx, app.Local(), 50)` returns visible, in-stock products that cost at
most 50. Ridu applies your filter and the collection's access rules together, before pagination.

This example reads anonymously because it passes no actor. To read as a signed-in user, set
`Actor` and `ActorCollection` in the options; see [passing the caller](/docs/local-api/#actors).

### Typed collection handles {#generated-collections}

Generated Go collection handles return your application's document types instead of
`store.Values`. Their `ridu.TypedListOptions` take the same `query.Expression` in `Where`, so
`generated.ProductsCollection.With(app.Local()).List(...)` uses the filters on this page
unchanged.

## Use a filter in an access rule {#access-filters}

An access rule can return [`ridu.Where`](/reference/ridu/where/) with a filter to allow access to
matching documents only. This collection allows reads of visible products:

```go title="content/products.go" focus={14-17,28}
package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
)

func visibleProducts(ridu.AccessContext) (ridu.AccessDecision, error) {
	visible, err := query.NewPath("visible")
	if err != nil {
		return ridu.Deny(), err
	}
	// This filter limits every read, including reads with other filters.
	return ridu.Where(
		query.Equal(visible, query.Boolean(true)),
	), nil
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

A filter chooses documents; the list options choose their order and page. Build a sort term with
`query.NewSort`:

```go
price, err := query.NewPath("price")
if err != nil {
	return store.Page{}, err
}
cheapest, err := query.NewSort(price, query.Ascending)
if err != nil {
	return store.Page{}, err
}
return local.List(ctx, "products", ridu.ListOptions{
	Where: filter,
	Sort:  []query.Sort{cheapest},
	Limit: 20,
})
```

Use `query.Descending` for the reverse order. Some fields, such as arrays and blocks, cannot be
sorted. See [sorting](/docs/querying/#sort) and [pagination](/docs/querying/#pagination) for
limits, and the [`query` reference](/reference/query/) for every function.
