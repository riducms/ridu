<!-- Generated from website/src/content/docs/go-packages/query.md by scripts/sync-agent-docs.ts. -->

# Filters and paths

Use the `query` package to describe which documents you want. For example, “products costing at
most 50 that are in stock” becomes a filter you can pass to the local API. The same package also
builds filters for [access rules](../access-control.md).

Three types appear together:

| Type               | What it means           | Example                 |
| ------------------ | ----------------------- | ----------------------- |
| `query.Path`       | Which field to check    | The product's `price`   |
| `query.Value`      | What to compare it with | The number `50`         |
| `query.Expression` | The resulting condition | `price` is at most `50` |

Building an expression does not fetch any documents. The local API executes it when you make a
read or other operation. For REST and TypeScript filter syntax, sorting, and pagination, see
[Querying data](../querying.md).

## Build a filter in Go {#build-filter}

This helper builds a filter for products in stock and within a budget. Put it in
`content/product_filters.go`:

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

[`NewPath`](https://riducms.com/reference/query/new-path/) names each field. `query.Number(maxPrice)` and
`query.Boolean(true)` give the comparisons their values. `query.And` joins the two conditions:
a document must satisfy both.

The helper returns a [`query.Expression`](https://riducms.com/reference/query/expression/). Keep it in a variable,
pass it to another function, or reuse it in several requests. Expressions do not change after
construction.

## Choose a field with a path {#paths}

A [`query.Path`](https://riducms.com/reference/query/path/) represents one field location. It is a validated Go value,
so functions accepting a path can rely on its spelling being valid. A plain string has not passed
that check.

| To name                            | Write                           | Resulting path |
| ---------------------------------- | ------------------------------- | -------------- |
| A top-level title                  | `query.NewPath("title")`        | `title`        |
| A title inside the SEO group       | `query.NewPath("seo", "title")` | `seo.title`    |
| A dotted path already held as text | `query.ParsePath("seo.title")`  | `seo.title`    |

`NewPath` takes separate segments. **`query.NewPath("seo.title")` returns an error** because the dot
is not part of a field name. Use [`ParsePath`](https://riducms.com/reference/query/parse-path/) for a dotted string,
such as one received from a filter form. Both functions return errors for malformed paths,
including empty segments; handle those errors before building the comparison.

This first check does not know your schema. `query.NewPath("unknown")` succeeds because the name
is well formed, but a request using it fails if the collection has no such field. Ridu checks the
field and its permissions when the operation runs. Fields with read-access rules also restrict
caller-supplied filters; see [protected field queries](../access-control.md#protected-field-queries).

### Reach groups, arrays, and blocks {#nested-paths}

Use authored field names, with a block's configured key where needed:

| Structure                           | Path                  | What it checks                       |
| ----------------------------------- | --------------------- | ------------------------------------ |
| `seo` group                         | `seo.title`           | The group's title                    |
| `variants` array                    | `variants.sku`        | SKU values across the product's rows |
| `layout` blocks with a `hero` block | `layout.hero.heading` | Headings in Hero rows only           |

An array path contains no row index or row key. Equality on `variants.sku` matches a document if
**any** row has the requested SKU. The `hero` segment selects the configured block type; leaving
it out of `layout.hero.heading` produces an invalid path.

Two comparisons joined with `And` are checked independently. A product with a red variant costing
30 and a blue variant costing 5 can match both “SKU equals red” and “variant price is at most 10”.
Those conditions do not require the same row to match. There is currently no same-row grouping
operator or all-rows operator.

For a repeated path, `NotEqual` excludes a document when any row equals the value. A product with
both red and blue variants therefore does not match “SKU is not red”.

## Use values that match the field {#values}

[`query.Value`](https://riducms.com/reference/query/value/) is the value **inside a comparison**. Construct it with the
Go helper for the value you mean:

| Value to compare with                     | Constructor                                                 |
| ----------------------------------------- | ----------------------------------------------------------- |
| Text or a relationship ID                 | `query.String("notebook")`                                  |
| A price or other number                   | `query.Number(50)`                                          |
| A checkbox value                          | `query.Boolean(true)`                                       |
| Null                                      | `query.Null()`                                              |
| Several candidates for a membership check | `query.In(path, query.String("red"), query.String("blue"))` |

`query.String("50")` is text, while `query.Number(50)` is a number. Use the kind that matches the
field. `query.In` accepts candidate values and builds its list; `query.List(...)` is also available
when constructing a comparison through `query.Compare`.

The similar names in `store` serve a different purpose. `store.Number(50)` is a document value
used when creating or reading content. `query.Number(50)` is a comparison value used when finding
content. They are separate Go types and cannot be substituted for each other. See
[Documents and values](./store.md) for working with document data.

## Combine conditions and handle errors {#combine-filters}

Use `query.And` when all conditions must pass, `query.Or` when any condition may pass, and
`query.Not` to negate a condition. `And` and `Or` require at least two expressions. Passing one,
none, or a `nil` child returns an error. If an optional filter is your only condition, use that
expression directly.

For comparisons you control in code, helpers such as `Equal`, `LessThanEqual`, and `Contains`
keep expressions short. They expect a valid path and compatible comparison value; invalid
arguments can panic. In the example, `NewPath` is checked first and `LessThanEqual` receives a
number.

When the operator or value comes from input, use
[`query.Compare`](https://riducms.com/reference/query/compare/), which returns an error for invalid combinations.
For example, `OperatorExists` expects `query.Boolean(...)`, while `OperatorIn` expects a list.
The [operator table](../querying.md#where) explains the available comparisons and text matching.

## Use a filter as a permission rule {#access-filters}

An access rule returns a decision about what the caller may do. Wrap an expression in
[`ridu.Where`](https://riducms.com/reference/ridu/where/) to allow access only to matching documents.

This collection allows reads only when its Visible checkbox is selected:

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

Add `Products` to `Config.Collections`. The read rule applies to reads from the admin, REST, the
SDK, and the local API. Configure create, update, and delete access separately for your application.

## Execute a filtered read {#run-filter}

Pass the expression directly to `ridu.ListOptions.Where`. This helper combines the budget filter
with the collection's visibility rule:

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

Call `FindAffordableProducts(ctx, app.Local(), 50)`. A visible, in-stock product costing 40 is
included. A hidden product, an out-of-stock product, or one costing 70 is excluded. Both the
caller filter and the access filter are applied before pagination.

The two uses of “where” have different jobs:

- `ridu.ListOptions{Where: filter}` asks for matching documents and still obeys access rules.
- `ridu.Where(filter)` is the decision returned by an access rule.

The example makes an anonymous read because it supplies no actor. For a read on behalf of a
signed-in user, also set `Actor` and `ActorCollection` in the list options. See
[passing the caller](../local-api.md#actors).

### Use the same filter with generated collections {#generated-collections}

The dynamic API above returns `store.Page`, whose documents hold `store.Values`. Generated Go
collection handles return your application's document types. For a generated `ProductsCollection` handle,
call `generated.ProductsCollection.With(app.Local()).List(...)` with `ridu.TypedListOptions`; its `Where`
property takes the same `query.Expression`.

Generation gives Go callers typed document models; you still build filters with this `query`
package. The generated TypeScript SDK instead accepts typed `where` objects, as shown in
[Querying data](../querying.md#where).

### Keep ordering and page size in the request {#result-options}

A filter chooses which documents match. `Page`, `Limit`, and `Sort` belong to the list options.
For Go sorting, build a term with `query.NewSort(path, query.Ascending)` or `query.Descending`
and put it in the options' `Sort` list. A field can be filterable without being sortable;
repeated array paths are one example.

See [sorting](../querying.md#sort) and [pagination](../querying.md#pagination) for request limits,
ordering, and result metadata. The [Local Go API](../local-api.md) covers the surrounding read
options, while the [`query` reference](https://riducms.com/reference/query/) lists each constructor and its arguments.
